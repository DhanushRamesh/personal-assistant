// Package vocabulary keeps the proper nouns somebody says, so that
// speech recognition can be told to expect them.
//
// A name the decoder has not been primed with does not merely go
// unhelped. It is replaced by whatever common words sound like it:
// "Alekhya" came back as "a leg near" until it was put in the list by
// hand. Every such name had to be noticed by the owner and typed in,
// which means the list only ever held the mistakes somebody had
// already been annoyed by.
//
// This reads the same week of messages the description is built from
// and asks for the names in them. Proper nouns only, because they are
// the words a general model has no reason to know and the ones it
// gets wrong: people, pets, places, brands, the things in one house.
// Ordinary words are already in the decoder's vocabulary and priming
// them costs budget a name needs.
//
// The list is replaced wholesale each night rather than added to. A
// list that only grows fills with names from one conversation in
// March and crowds out the people somebody actually talks about, and
// the budget is small: Deepgram takes a hundred terms in total, and
// Whisper's prompt is worth keeping short.
//
// Nothing here writes to the voice stack. This stores what was found
// and serves it; the machine with the microphones fetches it and
// decides what to do with it, because the engines live there and the
// conversations live here.
package vocabulary

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Window : How far back a rebuild reads.
//
// The same week the description reads, and for the same reason: a day
// is one conversation, and a name said once in it is not yet worth
// spending budget on.
const Window = 7 * 24 * time.Hour

// Most : The most messages read for one rebuild.
const Most = 400

// Least : Fewer messages than this and the list is left alone.
//
// Unlike a description, a thin week does not produce nonsense here --
// it produces a very short list. That is still worse than what is
// already stored, which was built from a fuller one.
const Least = 20

// Cap : The most terms kept for one person.
//
// Deepgram refuses more than a hundred keyterms in total and the
// hand-kept list already uses half of that, so one person's share is
// smaller again. Whisper has no hard limit, but a long prompt is a
// worse prompt.
const Cap = 40

// Longest : A term longer than this is not a name.
const Longest = 40

// Words : The most words in a term. "Air conditioner" is two;
// anything past four is a phrase mistaken for a name.
const Words = 4

// Store : Where the lists are kept.
type Store interface {
	// Put : Replaces one person's list.
	Put(ctx context.Context, userID string, terms []string, at time.Time) error
	// Get : One person's list, empty when they have none.
	Get(ctx context.Context, userID string) ([]string, time.Time, error)
	// Everyone : Every list at once, which is what the microphone
	// needs: it cannot tell who is speaking, so it expects all of them.
	Everyone(ctx context.Context) ([]string, time.Time, error)
}

// Said : Where what the person has said is read from.
type Said interface {
	SaidSince(ctx context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error)
}

// Ask : How the model is asked. Returns what it answered.
type Ask func(ctx context.Context, prompt string) (string, error)

// Builder : Finds the names and stores them.
type Builder struct {
	// Said : Where the person's own messages come from. Required.
	Said Said
	// Store : Where the list is kept. Required.
	Store Store
	// Ask : How the model is asked to find the names. Required.
	Ask Ask
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Build : Rewrites one person's list from what they have said.
func (b *Builder) Build(ctx context.Context, userID string) error {
	if b == nil || b.Said == nil || b.Store == nil || b.Ask == nil {
		return fmt.Errorf("vocabulary: not configured")
	}
	if userID == "" {
		return fmt.Errorf("vocabulary: nobody to listen to")
	}

	said, err := b.Said.SaidSince(ctx, userID, b.clock().Add(-Window), Most)
	if err != nil {
		return fmt.Errorf("vocabulary: reading what they said: %w", err)
	}
	if len(said) < Least {
		// Not a failure. Whatever is stored was built from more than
		// this and is left where it is.
		return nil
	}

	answer, err := b.Ask(ctx, Prompt(said))
	if err != nil {
		return fmt.Errorf("vocabulary: asking for the names: %w", err)
	}

	terms := Clean(answer)
	if len(terms) == 0 && b.Logger != nil {
		// Believed rather than treated as a failure: a week of "what
		// is the time" genuinely has no names in it, and storing the
		// empty list is the honest answer.
		b.Logger.Info("no names in what they said", slog.String("user_id", userID))
	}

	return b.Store.Put(ctx, userID, terms, b.clock().UTC())
}

// Clean : The terms in an answer, with everything that is not one
// dropped.
//
// The model is asked for one name per line and mostly gives that, but
// an answer is not a contract. What arrives carries bullets,
// numbering, a leading sentence and the occasional explanation in
// brackets, and all of it would otherwise be handed to a speech
// engine as though somebody had said it.
func Clean(answer string) []string {
	seen := map[string]bool{}
	var terms []string

	for _, line := range strings.Split(answer, "\n") {
		term := strings.TrimSpace(line)
		term = strings.TrimLeft(term, "-*0123456789.) \t")
		term = strings.Trim(strings.TrimSpace(term), `"'`)
		term = strings.TrimSpace(term)

		if !keepable(term) {
			continue
		}
		if folded := strings.ToLower(term); !seen[folded] {
			seen[folded] = true
			terms = append(terms, term)
		}
		if len(terms) == Cap {
			break
		}
	}
	return terms
}

// keepable : Whether one line is a name rather than the model talking.
func keepable(term string) bool {
	if term == "" || len(term) > Longest {
		return false
	}
	if len(strings.Fields(term)) > Words {
		return false
	}
	// A name starts with a letter. This drops a stray bullet that
	// survived trimming, and anything prefixed with punctuation.
	if !unicode.IsLetter([]rune(term)[0]) {
		return false
	}
	// A sentence, however short. No name holds one of these; an
	// explanation the model could not resist adding does.
	return !strings.ContainsAny(term, ".:;,?!()[]")
}

// clock : Now.
func (b *Builder) clock() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}
