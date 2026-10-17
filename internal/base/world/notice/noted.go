// Package notice keeps the record of what the assistant decided to
// say without being asked, and what came of it.
//
// The deciding used to live here too: a watcher on a thirty second
// tick, a table of thresholds read from config.ini, and a gate
// asking whether anybody was in the room. All of it is gone. A
// standing rule says the same thing in a row -- "when battery.level
// < 20" -- and the engine in internal/job runs it, so the last
// watcher of this kind stopped being a special case written in Go.
//
// What is left is the half that was never about batteries. The
// ordinary output of anything speaking unasked is silence, and
// silence has several causes: nothing arrived, the condition did
// not hold, the model found nothing worth saying, or the thing
// that decides has stopped running. From outside all four look
// identical. Owner: *"I should see in UI when it triggered, why it
// didn't speak, if spoken what it spoke."*
//
// So every run of every rule is written here, and /v1/remarks
// reads it.
package notice

import (
	"context"
	"time"
)

// Outcome : What came of a thing that might have been worth saying.
type Outcome string

const (
	// Said : It was spoken.
	Said Outcome = "said"
	// Held : The moment was refused -- nobody there, something said
	// too recently, or this thing said already.
	Held Outcome = "held"
	// Declined : The model read it and decided it was not worth
	// saying. The ordinary outcome, and not a failure.
	Declined Outcome = "declined"
	// Failed : Something went wrong asking or speaking.
	Failed Outcome = "failed"
)

// Reason : Why something was worth saying.
//
// Kept as a type on purpose: it is written down with every row, and
// the words the column has held are how the next person reading the
// page knows which kinds there have been. Two: a rule firing, and a
// terminal wanting attention.
type Reason string

// Immediate : The rule that fired said this was to be said now.
const Immediate Reason = "immediate"

// Terminal : A terminal on their laptop wanted attention -- a long
// command finished, a program asked for them, or a shell ended.
//
// The second value the column has held, from 17 October 2026. Not a
// rule: the desktop app reports it and the assistant decides at once
// whether it is worth a sentence, so this row is the only place a
// decision to stay quiet about it can be seen.
const Terminal Reason = "terminal"

// Noted : One thing the assistant considered saying.
type Noted struct {
	// UserID : Whose it was.
	UserID string
	// At : When the thing it was about happened, not when this was
	// written. A remark held back and the event that prompted it are
	// the same moment to somebody reading it back.
	At time.Time
	// Label : The thing, as its own sender described it.
	Label string
	// Reason : What made it worth looking at.
	Reason Reason
	// Outcome : What came of it.
	Outcome Outcome
	// Said : What was spoken, when anything was.
	Said string
	// Because : Why it came to nothing, where the outcome alone does
	// not say enough.
	Because string
}

// Notes : Where what it considered saying is kept.
//
// Only the considered ones. A tick that finds nothing writes nothing,
// or this would be two and a half thousand rows a day recording that
// nothing happened.
type Notes interface {
	Note(ctx context.Context, n Noted) error
}
