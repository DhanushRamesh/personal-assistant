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
	"sync"
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

// Habits : How far back the counted half reads.
//
// Four weeks against the week of conversation, because the two
// questions need different amounts of time. What somebody is talking
// about now is this week's business and a month of it would describe
// a person who has moved on. What somebody does every Tuesday cannot
// be seen in seven days at all: one Tuesday is an anecdote, and the
// difference between a routine and a coincidence is how many times it
// came round.
const Habits = 28 * 24 * time.Hour

// Doings : The most events and the most tool calls read for one
// rebuild. A cap rather than a promise, as Most is.
const Doings = 4000

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

// Asked : Where what the assistant ran for them is read from.
type Asked interface {
	CalledSince(ctx context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error)
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
	// Asked : Where the tools run for them come from. Optional, like
	// Did.
	Asked Asked
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

	// mu, seen : The newest thing each person had said when they were
	// last described, so an hourly rebuild with nothing new to read
	// does not happen. Guarded because one builder serves everybody.
	mu   sync.Mutex
	seen map[string]time.Time
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

	// What is already written, and when. Both come from the same read:
	// the description is given back to be corrected, and its age is
	// what says whether there is anything new to correct it with.
	if !b.somethingNew(userID, said) {
		// Nothing has been said since the last rebuild. Doing it again
		// would ask a model to write the same description from the
		// same evidence, hourly, for ever.
		return nil
	}
	was, _ := b.previously(ctx, userID)

	answer, err := b.Ask(ctx, Prompt(said, b.rhythm(ctx, userID), b.asking(ctx, userID), was))
	if err != nil {
		return fmt.Errorf("profile: asking for the description: %w", err)
	}
	body := strings.TrimSpace(answer)
	if body == "" {
		return fmt.Errorf("profile: the model described nobody")
	}

	return b.keep(ctx, userID, body)
}

// previously : The description as it stands, and when it was written.
//
// Empty and zero when there is none, which is the first run and any run
// after the memory was deleted by hand.
func (b *Builder) previously(ctx context.Context, userID string) (string, time.Time) {
	held, err := b.Memories.All(ctx, userID, memory.TierAlways)
	if err != nil {
		b.warn(ctx, "cannot read the description that exists", err)
		return "", time.Time{}
	}
	for i := range held {
		if held[i].Subject == Subject {
			return held[i].Body, held[i].UpdatedAt
		}
	}
	return "", time.Time{}
}

// somethingNew : Whether anything has been said since this last
// rebuilt, and notes the newest it has now seen.
//
// The reason the rebuild runs hourly rather than daily is that a
// description goes out of date between rebuilds; the reason most hours
// it will not run is that most hours nothing happens. Without this, an
// idle night is eight calls to a model asking it to write the same
// paragraph from the same week -- and that prompt is a week of
// messages and four weeks of events, so it is not a cheap thing to
// repeat.
//
// Kept here rather than read from the stored description's timestamp,
// which was the first attempt. That timestamp comes from the memory
// store's clock and the messages come from this one's, and comparing
// two clocks that are only the same by accident is how a rebuild
// either never runs or always does. A restart forgets this and costs
// one extra rebuild, which is the right way round.
//
// Messages only, and deliberately. Events arrive on their own all day:
// a laptop rejoining a network would keep this rebuilding through a
// night nobody was awake for, and no description turns on one more
// event. Somebody saying something is the signal that there is
// anything new to say about them.
func (b *Builder) somethingNew(userID string, said []conversation.Message) bool {
	var newest time.Time
	for _, m := range said {
		if m.At.After(newest) {
			newest = m.At
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seen == nil {
		b.seen = map[string]time.Time{}
	}
	if was, built := b.seen[userID]; built && !newest.After(was) {
		return false
	}
	b.seen[userID] = newest
	return true
}

// warn : A failure worth a line and nothing more.
func (b *Builder) warn(ctx context.Context, msg string, err error) {
	if b.Logger != nil {
		b.Logger.WarnContext(ctx, msg, slog.Any("error", err))
	}
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
	did, err := b.Did.Recent(ctx, userID, event.Query{
		Since: b.clock().Add(-Habits),
		Limit: Doings,
		// Not the raw position readings. A phone reports one every five
		// minutes, so over four weeks there are eight thousand of them
		// against a hundred of everything else: they would fill Doings
		// on their own and the description would be written from
		// coordinates and nothing else. They also sit between every
		// pair of real events, which is what pairing has to see past.
		// What they are turned into -- place.stayed -- is here.
		Omit: []string{event.Fixed},
	})
	if err != nil {
		if b.Logger != nil {
			b.Logger.ErrorContext(ctx, "cannot read what their devices reported",
				slog.String("user_id", userID), slog.Any("error", err))
		}
		return ""
	}
	return Rhythm(did, b.Where)
}

// asking : What the assistant ran for them over the same four weeks.
//
// Costs the description its half about what they keep wanting and not
// the description itself, for the same reason rhythm does.
func (b *Builder) asking(ctx context.Context, userID string) string {
	if b.Asked == nil {
		return ""
	}
	called, err := b.Asked.CalledSince(ctx, userID, b.clock().Add(-Habits), Doings)
	if err != nil {
		if b.Logger != nil {
			b.Logger.ErrorContext(ctx, "cannot read what was run for them",
				slog.String("user_id", userID), slog.Any("error", err))
		}
		return ""
	}
	return Asking(called, b.Where)
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
