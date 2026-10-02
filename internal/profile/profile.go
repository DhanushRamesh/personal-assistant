// Package profile keeps a standing description of the person, written
// from what they have actually said.
//
// The memories the assistant holds are facts it was told once: a
// birthday, a pet's name, that the coffee is filter coffee. They are
// true and they are scattered, and none of them says what somebody is
// like to talk to -- what they keep coming back to, who is in their
// life, how they ask for things. A personal assistant who had been
// listening for a month would know that without being told, and this
// is the attempt to have one.
//
// Written from the raw messages every time, never from the previous
// profile. A description built from its own last version drifts: each
// day's wording becomes the next day's evidence, and after a week it
// describes a person who was invented on the Tuesday. Reading the
// messages again is more work and is the only thing that keeps it
// anchored to what was really said.
//
// Two things are kept out of it however well the evidence seems to
// support them: where somebody is from or lives, and their health. The
// first rebuild wrote "almost certainly Indian, likely based in or
// around Hyderabad" from a girlfriend's address and the films that had
// come up, and listed four symptoms mentioned in passing. Neither had
// been said, both were plausible, and a hedge is not a defence -- a
// guess in a description that is read before every answer is acted on
// exactly as a fact is.
//
// The owner chose prose over counted observations, knowing that prose
// cannot be checked. What that buys is a description that reads like
// somebody who knows them; what it costs is that a wrong line stays
// wrong until they notice it and say so. It is kept as an ordinary
// memory for exactly that reason -- memory_list shows it, and they can
// have it changed or dropped like any other.
package profile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// Subject : How the profile memory is recognised among the others.
//
// Matched on to decide whether to write a new memory or replace the one
// already there, so it is a fixed string rather than anything the model
// produces.
const Subject = "What I have noticed about them"

// Window : How far back a rebuild reads.
//
// A week rather than a day. A day is one mood and a handful of
// subjects; asked to describe somebody from it the model reaches for
// whatever happened to come up, and the description swings about. A
// week is long enough that what recurs looks different from what
// happened once.
const Window = 7 * 24 * time.Hour

// Most : The most messages read for one rebuild.
//
// A cap rather than a promise: the whole week is read when it fits, and
// the most recent Most when it does not.
const Most = 400

// Least : Fewer messages than this and no profile is written.
//
// Describing somebody from a handful of sentences produces confident
// nonsense, and a confident wrong description is worse than none: it
// goes into every prompt afterwards and the assistant acts on it.
const Least = 20

// Said : Where what the person has said is read from.
type Said interface {
	SaidSince(ctx context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error)
}

// Did : Where what their devices reported is read from.
type Did interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Ask : How the model is asked. Returns what it answered.
type Ask func(ctx context.Context, prompt string) (string, error)

// Builder : Writes and rewrites the profile.
type Builder struct {
	// Said : Where the person's own messages come from. Required.
	Said Said
	// Did : Where what their devices reported comes from. Optional: with
	// none, they are described from their words alone, which is how
	// this worked before anything was watching.
	Did Did
	// Where : The timezone their days are counted in. Nil is UTC, which
	// puts their evenings on the wrong date.
	Where *time.Location
	// Memories : Where the profile is kept. Required.
	Memories memory.Store
	// Ask : How the model is asked to write it. Required.
	Ask Ask
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Build : Rewrites the profile for one person from what they have said.
//
// Every failure is logged and returned rather than retried. Nothing
// waits on this, and a profile that is a day out of date is not worth
// a second call to a model.
func (b *Builder) Build(ctx context.Context, userID string) error {
	if b == nil || b.Said == nil || b.Memories == nil || b.Ask == nil {
		return fmt.Errorf("profile: not configured")
	}
	if userID == "" {
		return fmt.Errorf("profile: no person to describe")
	}

	said, err := b.Said.SaidSince(ctx, userID, b.clock().Add(-Window), Most)
	if err != nil {
		return fmt.Errorf("profile: reading what they said: %w", err)
	}
	if len(said) < Least {
		// Not a failure. There is nothing to describe yet, and the
		// profile that exists -- if any -- is left alone rather than
		// replaced with something worse.
		return nil
	}

	answer, err := b.Ask(ctx, Prompt(said, b.rhythm(ctx, userID)))
	if err != nil {
		return fmt.Errorf("profile: asking for the description: %w", err)
	}
	body := strings.TrimSpace(answer)
	if body == "" {
		return fmt.Errorf("profile: the model described nobody")
	}

	return b.keep(ctx, userID, body)
}

// rhythm : What their devices reported over the same week, counted.
//
// A failure here costs the description its half about their days and
// not the description itself. What somebody says is the half that has
// always been there, and losing a week of prose because a query failed
// would be the wrong trade.
func (b *Builder) rhythm(ctx context.Context, userID string) string {
	if b.Did == nil {
		return ""
	}
	did, err := b.Did.Recent(ctx, userID, event.Query{Since: b.clock().Add(-Window)})
	if err != nil {
		if b.Logger != nil {
			b.Logger.ErrorContext(ctx, "cannot read what their devices reported",
				slog.String("user_id", userID), slog.Any("error", err))
		}
		return ""
	}
	return Rhythm(did, b.Where)
}

// keep : Stores the description, replacing the one already there.
//
// One profile, rewritten. Appending would leave a pile of descriptions
// of the same person, each contradicting the last, and all of them in
// every prompt.
func (b *Builder) keep(ctx context.Context, userID, body string) error {
	held, err := b.Memories.All(ctx, userID, memory.TierAlways)
	if err != nil {
		return fmt.Errorf("profile: reading the memories: %w", err)
	}
	for i := range held {
		if held[i].Subject != Subject {
			continue
		}
		held[i].Body = body
		held[i].Tier = memory.TierAlways
		if err := b.Memories.Update(ctx, &held[i]); err != nil {
			return fmt.Errorf("profile: rewriting it: %w", err)
		}
		return nil
	}

	m, err := memory.New(userID, memory.TierAlways, Subject, body)
	if err != nil {
		return fmt.Errorf("profile: describing them: %w", err)
	}
	if err := b.Memories.Create(ctx, m); err != nil {
		return fmt.Errorf("profile: writing it: %w", err)
	}
	return nil
}

// clock : Now.
func (b *Builder) clock() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}
