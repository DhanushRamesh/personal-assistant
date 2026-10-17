// Package knowledge : Everything known of the person, kept on disk in
// chunks and embedded, so what bears on a moment is found by meaning
// rather than everything pushed into every prompt (owner, 17 October 2026:
// *"I should pass it everything, but I can't just push everything .. so
// everything should be recorded on disk somewhere and the embedding model
// should bring my personality"*).
//
// Kept by source -- their description, a memory, a space's notes, Claude
// Code's conversations, a file -- and replaced whole when a source's text
// changes. Found by the nearest chunks to a question, across all of it.
package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/embed"
)

// Source : Where a piece of what is known came from.
type Source string

const (
	// Profile : Their standing description, by its sections.
	Profile Source = "profile"
	// Memory : Something they asked to be remembered.
	Memory Source = "memory"
	// Notes : A space's notes, written on the laptop.
	Notes Source = "notes"
	// Claude : Their conversations with Claude Code: what they asked and
	// what came of it.
	Claude Source = "claude"
	// File : A file on their laptop they chose to be known.
	File Source = "file"
)

// Sources : Every source, in the order they are named.
var Sources = []Source{Profile, Memory, Notes, Claude, File}

// Valid : Whether a source is one there is.
func (s Source) Valid() bool {
	for _, x := range Sources {
		if s == x {
			return true
		}
	}
	return false
}

// ChunkSize : About how long a chunk is, in characters: a paragraph or a
// few, short enough that its meaning is one thing.
const ChunkSize = 700

// MaxText : The most of one source's text kept.
const MaxText = 400 << 10

// Piece : One chunk of what is known.
type Piece struct {
	Source Source
	Ref    string
	Chunk  int
	Text   string
	At     time.Time
}

// Found : A piece and how near it was.
type Found struct {
	Piece
	Score float64
}

// Store : Where it is kept.
type Store interface {
	// Hash : The hash a source's text was last kept with; empty when it
	// was never kept with this model.
	Hash(ctx context.Context, userID string, source Source, ref, model string) (string, error)
	// Put : Replaces a source's chunks with these, with their vectors.
	Put(ctx context.Context, userID string, source Source, ref, model, hash string, at time.Time, chunks []string, vectors []embed.Vector) error
	// Remove : Forgets a source.
	Remove(ctx context.Context, userID string, source Source, ref string) error
	// Refs : The refs kept for a source.
	Refs(ctx context.Context, userID string, source Source) ([]string, error)
	// NearestTo : The chunks nearest a vector, nearest first.
	NearestTo(ctx context.Context, userID string, q embed.Vector, model string, limit int) ([]Found, error)
}

// Known : Keeps what is known and finds what bears on a question.
type Known struct {
	Store Store
	Embed embed.Embedder
	Now   func() time.Time
}

// ErrUnavailable : Nothing to embed with.
var ErrUnavailable = errors.New("knowledge: nothing to embed with")

func (k *Known) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

// Keep : Keeps a source's text, in chunks, embedded; unchanged text is
// left as it is, and empty text forgets the source. Reports whether
// anything was embedded.
func (k *Known) Keep(ctx context.Context, userID string, source Source, ref, text string) (bool, error) {
	if !source.Valid() || strings.TrimSpace(ref) == "" || userID == "" {
		return false, fmt.Errorf("knowledge: keeping needs a person, a source and a ref")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, k.Store.Remove(ctx, userID, source, ref)
	}
	if k.Embed == nil || !k.Embed.Available() {
		return false, ErrUnavailable
	}
	if len(text) > MaxText {
		text = text[:MaxText]
	}
	sum := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(sum[:])
	was, err := k.Store.Hash(ctx, userID, source, ref, k.Embed.Model())
	if err != nil {
		return false, err
	}
	if was == hash {
		return false, nil
	}
	chunks := Chunks(text, ChunkSize)
	vectors := make([]embed.Vector, 0, len(chunks))
	for i := 0; i < len(chunks); i += 16 {
		batch := chunks[i:min(i+16, len(chunks))]
		vs, err := k.Embed.Documents(ctx, batch)
		if err != nil {
			return false, fmt.Errorf("knowledge: embedding %s %s: %w", source, ref, err)
		}
		vectors = append(vectors, vs...)
	}
	return true, k.Store.Put(ctx, userID, source, ref, k.Embed.Model(), hash, k.now(), chunks, vectors)
}

// Find : The pieces nearest a question, at most limit of them, as alike as
// at least floor.
func (k *Known) Find(ctx context.Context, userID, question string, limit int, floor float64) ([]Found, error) {
	if k == nil || k.Embed == nil || !k.Embed.Available() {
		return nil, ErrUnavailable
	}
	question = strings.TrimSpace(question)
	if question == "" || limit <= 0 {
		return nil, nil
	}
	q, err := k.Embed.Query(ctx, question)
	if err != nil {
		return nil, err
	}
	found, err := k.Store.NearestTo(ctx, userID, q, k.Embed.Model(), limit)
	if err != nil {
		return nil, err
	}
	out := found[:0]
	for _, f := range found {
		if f.Score >= floor {
			out = append(out, f)
		}
	}
	return out, nil
}

// Chunks : Text in pieces of about size characters, cut where paragraphs
// end, then lines, then sentences; never a piece of nothing.
func Chunks(text string, size int) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+len(para) > size {
			flush()
		}
		for len(para) > size {
			cut := strings.LastIndexAny(para[:size], "\n.!?")
			if cut < size/3 {
				cut = strings.LastIndex(para[:size], " ")
			}
			if cut <= 0 {
				cut = size
			}
			cur.WriteString(para[:cut+1])
			flush()
			para = strings.TrimSpace(para[cut+1:])
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return out
}

// Told : Found pieces as the model is given them: each with where it came
// from, nearest first.
func Told(found []Found) string {
	if len(found) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range found {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("[" + string(f.Source) + ": " + f.Ref + "]\n" + f.Text)
	}
	return b.String()
}
