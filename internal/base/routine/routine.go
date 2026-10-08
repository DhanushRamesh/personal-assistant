// Package routine keeps what somebody usually does, as conditions.
//
// Owner, 7 October 2026: *"I want the routine to be in a conditional
// way and able to visualise. The reason I want it conditional is I
// will use that to write the rules, and further I'll ask the AI to
// write the rules by itself with examples of the existing rules, and
// it should use the routine too."*
//
// The standing description already says it -- *"Returns home around
// nine in the evening"* -- and says it in prose, which a model can
// read and a gate cannot. A rule that fires every two minutes needs a
// number, and it needs one it can have without recomputing four weeks
// of events each time.
//
// # Why this is a table and the description is not
//
// The counted half of the description is recomputed from the events
// on every rebuild, which is why it needs no store: nothing is
// remembered, so nothing can go stale or disagree. That argument
// holds for a paragraph written once an hour and fails completely for
// a condition read every two minutes.
//
// So this is the small, specific part worth keeping: which place,
// whether it is arriving or leaving, the hour, and how many days
// agreed. Not a confidence score -- "4 of 7 days" is a thing that can
// be argued with and 0.83 is not -- and not a lifecycle, because
// there is no history to put one on yet.
package routine

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Prefix : What every routine source is called.
//
// Chosen so a gate reads as the sentence somebody would say:
// usually.home.leaves older than 2h. It also keeps these out of the
// namespace a device reports into, which is how the reading side
// tells them apart without a list.
const Prefix = "usually."

// What : Which end of a visit a fact is about.
type What string

const (
	// Arrives : When they get there.
	Arrives What = "arrives"
	// Leaves : When they go.
	Leaves What = "leaves"
)

// Fact : One thing somebody usually does, and when.
type Fact struct {
	// UserID : Whose it is.
	UserID string
	// Subject : What it is about, by the name they gave it
	// themselves -- home, office. Never a name the server chose.
	Subject string
	// What : Arriving or leaving.
	What What
	// Hour : The hour of the day, 0 to 23, in their own zone.
	//
	// An hour and not a minute. The hour is the middle of the band
	// the observations agreed on, which is three hours wide, so a
	// minute would be precision the evidence does not have.
	Hour int
	// Days : How many days agreed on it.
	Days int
	// Over : Out of how many days it was seen at all.
	//
	// Kept beside Days because the two together are the whole of
	// what makes this arguable: 4 of 7 says something 4 does not.
	Over int
	// At : When this was worked out.
	At time.Time
}

// Name : What a gate calls this fact.
func (f Fact) Name() string {
	return Prefix + strings.ToLower(f.Subject) + "." + string(f.What)
}

// Mine : Whether a gate source is one of these rather than
// something a device reports.
func Mine(source string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), Prefix)
}

// Today : The moment this fact describes, on the given day.
//
// What makes the existing operators work on it unchanged. A fact
// timestamped at today's usual hour means "older than 2h" reads as
// two hours past when they normally go, and "within 30m" as the half
// hour after it -- neither of which needed a new word in the
// grammar.
func (f Fact) Today(now time.Time, where *time.Location) time.Time {
	if where == nil {
		where = time.UTC
	}
	here := now.In(where)
	return time.Date(here.Year(), here.Month(), here.Day(), f.Hour, 0, 0, 0, where)
}

// Said : The hour as somebody would say it.
func (f Fact) Said() string {
	switch {
	case f.Hour == 0:
		return "midnight"
	case f.Hour == 12:
		return "midday"
	case f.Hour < 12:
		return fmt.Sprintf("%dam", f.Hour)
	default:
		return fmt.Sprintf("%dpm", f.Hour-12)
	}
}

// Store : Where the facts are kept.
type Store interface {
	// Keep : Replaces everything known about this person.
	//
	// Replaced rather than merged, because the whole set is worked
	// out together from one window of events. Merging would leave a
	// place they stopped going answering a condition for ever.
	Keep(ctx context.Context, userID string, facts []Fact) error
	// All : Everything known about this person.
	All(ctx context.Context, userID string) ([]Fact, error)
}
