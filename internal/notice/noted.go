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
