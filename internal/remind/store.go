package remind

import (
	"context"
	"time"
)

// Store : Where reminders are kept.
type Store interface {
	// Create : Stores a new reminder.
	Create(ctx context.Context, r *Reminder) error

	// Get : Returns one reminder, or ErrNotFound.
	Get(ctx context.Context, userID, id string) (*Reminder, error)

	// List : A person's reminders in the given states, soonest first.
	// No states means every state.
	List(ctx context.Context, userID string, states ...Status) ([]Reminder, error)

	// Cancel : Calls one off. Cancelling one already cancelled, or already
	// fired, is not an error: the caller wanted it not to happen and it
	// will not happen.
	Cancel(ctx context.Context, userID, id string) error

	// Due : Pending reminders whose time has come, soonest first.
	//
	// This is what the firing loop asks, several times a minute, for ever.
	Due(ctx context.Context, at time.Time, limit int) ([]Reminder, error)

	// Fired : Records that a reminder was said.
	//
	// A zero next finishes it. Otherwise it is due again then, which is how
	// a repeating one comes back.
	//
	// Pending or held, and nothing else. Pending is the firing loop saying
	// it; held is a delivery to somebody who has just walked in. Both are
	// one-way claims, so the guard against two passes saying the same
	// reminder twice still holds.
	Fired(ctx context.Context, id string, at time.Time, next time.Time) error

	// Missed : Records that a reminder's time passed with nothing listening,
	// too long ago to say now.
	Missed(ctx context.Context, id string, at time.Time) error

	// Unmentioned : Missed reminders the person has not been told about,
	// oldest first.
	Unmentioned(ctx context.Context, userID string) ([]Reminder, error)

	// Mentioned : Records that a miss has been brought up, so it is brought
	// up once and not on every turn afterwards.
	Mentioned(ctx context.Context, ids []string, at time.Time) error

	// Hold : Keeps a reminder back because nobody was there to hear it.
	//
	// Its due time is left alone: it says when the reminder was for, and
	// that is what the person is told when they come back. Only a pending
	// one can be held, so a firing loop and this cannot both claim it.
	Hold(ctx context.Context, id string, at time.Time) error

	// Waiting : Reminders held back for somebody, oldest first.
	//
	// What to say when they walk in. Oldest first because that is the
	// order they were for.
	Waiting(ctx context.Context, userID string) ([]Reminder, error)

	// Snooze : Puts a reminder off until a later time.
	//
	// It goes back to pending, whether it was waiting or has just been
	// said, so that one that already rang can ring again. Fires is left
	// alone: how many times it has been put off is worth knowing.
	//
	// Refused for anything Snoozable refuses, and with that error.
	Snooze(ctx context.Context, userID, id string, until time.Time) error

	// LastSpoken : What the person was told since the given moment,
	// most recently first.
	//
	// This is what "that" means in "snooze that". Several, not one,
	// because two reminders can come due together and the difference
	// between one answer and two is the difference between answering and
	// asking which.
	LastSpoken(ctx context.Context, userID string, since time.Time) ([]Reminder, error)

	// Reschedule : Moves a reminder to its next time without saying it.
	//
	// For a repeating one whose turn was missed. Marking it missed would
	// end it for good, and recording a firing would claim something was
	// said that nobody heard.
	Reschedule(ctx context.Context, id string, next time.Time) error
}

// DefaultDueLimit : The most reminders one pass of the firing loop takes.
//
// A bound rather than a guess at a maximum. If a hundred are somehow due at
// once, they are said over several passes rather than all in one breath.
const DefaultDueLimit = 20

// DefaultSpokenLimit : The most LastSpoken returns.
//
// A bound, not a guess. What it is for is the last thing or two said, and
// anything past a handful is not what "that" could mean anyway.
const DefaultSpokenLimit = 5
