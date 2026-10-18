// Package nearest chooses which tools to describe for one question.
//
// A request carries every tool it offers in full, and that block was
// 117,923 bytes of a 196,382-byte request. Describing all of them is
// what deferral was built to stop: twenty-five are named in a
// hand-written list and described always, the rest are names the model
// can ask about.
//
// Measured on 18 October 2026, the hand-written list is the weakest
// part of it. Over the 106 questions in the tool suite, the right tool
// was reached first try 85 times with those twenty-five and 99 times
// with the twenty nearest the question by meaning -- and describing all
// eighty-eight reached 102, so choosing by meaning recovers most of the
// way on forty per cent of the bytes. Neither run ever asked what a
// tool was: when the right one arrives as a name, the model does not
// call tool_describe, it reaches for something else and is wrong.
//
// The block also costs time in itself. The same question took 2.36s to
// its first word with no tools, 2.94s with twenty and 3.66s with fifty,
// so twenty rather than fifty is about 0.71s off every call in a turn.
//
// What a tool is ranked by is not what the model reads. web_search is
// 3,999 characters against an embedder that reads about 512 tokens, so
// its worked examples -- which are the questions it is for -- never
// reach the embedder at all. Ranked on its purpose and those examples
// the right tool is in the nearest twenty on 98% of cases, against 96%
// for the whole description and 79% for the fixed list.
package nearest

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/thinking/embed"
)

// Batch : How many texts the embedding server takes at once.
//
// Thirty-two, which is what it refuses more than: a request for all
// eighty-eight comes back 413 Payload Too Large.
const Batch = 32

// Warming : How long one pass of embedding may take.
//
// Generous because nobody waits for it. The tools are embedded away
// from any turn, and a turn that arrives before they are ready is
// offered everything, which is what happened before this existed.
const Warming = 2 * time.Minute

// Choosing : How long a turn will wait to be told which tools to
// describe.
//
// Short, and a timeout is not a failure: it falls back to the
// hand-written list, which is what the server sent until today. An
// embedding server that has gone away must cost a fraction of a second
// and not a turn.
const Choosing = 900 * time.Millisecond

// Text : What a tool is ranked by -- its name, what it does, and the
// questions it is for.
//
// Not its whole description. The seven longest run past what the
// embedder reads, and what is cut off is the examples, which are the
// most useful part for matching a question to a tool.
func Text(t tool.Tool) string {
	asks := make([]string, 0, len(t.Examples))
	for _, e := range t.Examples {
		asks = append(asks, e.Ask)
	}
	return strings.TrimSpace(t.Name + ". " + strings.TrimSpace(t.Purpose) + " " + strings.Join(asks, ". "))
}

// Picker : Ranks tools against a question, over vectors it keeps.
//
// Safe to use before it knows anything. A tool it has no vector for is
// never ranked out: it is returned alongside the nearest, so the worst
// a cold or broken Picker can do is offer what the server offered
// before.
type Picker struct {
	// Embed : Where vectors come from. Nil leaves every turn to the
	// hand-written list.
	Embed embed.Embedder
	// Logger : Where a failure is recorded. Nil discards it.
	Logger *slog.Logger

	mu   sync.RWMutex
	seen map[string]embed.Vector
}

// Warm : Embeds any of these tools it does not already hold.
//
// Called away from a turn -- at startup for the server's own, and after
// a chat for whatever a device offered that was new. Returns how many
// it added.
func (p *Picker) Warm(ctx context.Context, tools []tool.Tool) (int, error) {
	if p == nil || p.Embed == nil {
		return 0, nil
	}
	var want []string
	p.mu.RLock()
	for _, t := range tools {
		if text := Text(t); p.seen[text] == nil {
			want = append(want, text)
		}
	}
	p.mu.RUnlock()
	if len(want) == 0 {
		return 0, nil
	}
	// Each text once, however many tools share it.
	want = unique(want)

	ctx, cancel := context.WithTimeout(ctx, Warming)
	defer cancel()
	added := 0
	for i := 0; i < len(want); i += Batch {
		end := min(i+Batch, len(want))
		got, err := p.Embed.Documents(ctx, want[i:end])
		if err != nil {
			return added, fmt.Errorf("nearest: embedding tools: %w", err)
		}
		p.mu.Lock()
		if p.seen == nil {
			p.seen = make(map[string]embed.Vector, len(want))
		}
		for k, v := range got {
			p.seen[want[i+k]] = v
			added++
		}
		p.mu.Unlock()
	}
	return added, nil
}

// Keep : The names to describe in full for this question: the n nearest
// of those it can rank, and everything it cannot.
//
// The second half is the safety. A tool with no vector -- one a device
// has just offered, or one embedded while the server was away -- is
// kept rather than hidden, so a question is never answered without a
// tool for a reason nobody can see.
//
// Empty and false where it cannot rank at all, which the caller reads
// as "offer what you would have offered".
func (p *Picker) Keep(ctx context.Context, say string, among []tool.Tool, n int) (map[string]bool, bool) {
	if p == nil || p.Embed == nil || n <= 0 || strings.TrimSpace(say) == "" {
		return nil, false
	}

	p.mu.RLock()
	held := len(p.seen)
	p.mu.RUnlock()
	if held == 0 {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, Choosing)
	defer cancel()
	q, err := p.Embed.Query(ctx, say)
	if err != nil {
		if p.Logger != nil {
			p.Logger.WarnContext(ctx, "cannot rank the tools, so all the usual ones are offered",
				slog.Any("error", err))
		}
		return nil, false
	}

	type scored struct {
		name string
		sim  float64
	}
	keep := make(map[string]bool, n)
	ranked := make([]scored, 0, len(among))
	p.mu.RLock()
	for _, t := range among {
		v := p.seen[Text(t)]
		if v == nil {
			// Not ranked, and therefore not ranked out.
			keep[t.Name] = true
			continue
		}
		ranked = append(ranked, scored{t.Name, embed.Similarity(q, v)})
	}
	p.mu.RUnlock()

	sort.Slice(ranked, func(i, j int) bool { return ranked[i].sim > ranked[j].sim })
	for i := 0; i < n && i < len(ranked); i++ {
		keep[ranked[i].name] = true
	}
	return keep, true
}

// unique : The distinct strings, in the order first seen.
func unique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
