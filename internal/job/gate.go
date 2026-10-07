package job

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/routine"
)

// Presence : The gate source that asks whether the person is in the
// room, rather than reading an event.
const Presence = "presence"

// Here, Away : What a presence gate may be compared against.
const (
	Here = "here"
	Away = "away"
)

// How a gate may compare. Two-word forms are matched before
// one-word ones, so "is not" is never read as "is".
const (
	// Is, IsNot : Equality against the value, ignoring case.
	Is    = "is"
	IsNot = "is not"
	// Less, More, AtMost, AtLeast : Numeric comparison. Both sides
	// must be numbers or the gate does not pass.
	Less    = "<"
	More    = ">"
	AtMost  = "<="
	AtLeast = ">="
	// Older, Within : How long ago the reading arrived, against a
	// duration such as "2h".
	Older  = "older than"
	Within = "within"
)

// ErrUnreadableGate : The condition is not one of the forms.
var ErrUnreadableGate = errors.New("job: that condition could not be read")

// Gate : A condition checked before the rest of a sequence runs.
type Gate struct {
	// Source : The event kind to read, or [Presence].
	Source string
	// Op : How to compare.
	Op string
	// Value : What to compare against -- a number, a word, or a
	// duration for [Older] and [Within].
	Value string
}

// ParseGate : Reads a condition as it is written on a step.
//
//	battery.level < 20
//	battery.level is not 100
//	battery.level older than 2h
//	presence is away
//
// The first word names the source, the next one or two the
// comparison, and the rest is the value, which may contain spaces.
func ParseGate(s string) (Gate, error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 3 {
		return Gate{}, fmt.Errorf("%w: %q", ErrUnreadableGate, s)
	}

	gate := Gate{Source: strings.ToLower(fields[0])}
	rest := fields[1:]
	if two := strings.ToLower(rest[0] + " " + rest[1]); two == IsNot || two == Older {
		gate.Op, rest = two, rest[2:]
	} else {
		switch one := strings.ToLower(rest[0]); one {
		case Is, Less, More, AtMost, AtLeast, Within:
			gate.Op, rest = one, rest[1:]
		}
	}
	if gate.Op == "" || len(rest) == 0 {
		return Gate{}, fmt.Errorf("%w: %q", ErrUnreadableGate, s)
	}
	gate.Value = strings.Join(rest, " ")

	if err := gate.sound(); err != nil {
		return Gate{}, err
	}
	return gate, nil
}

// sound : Whether the parts make a condition that could ever hold.
//
// Checked when the gate is written rather than when it runs, so a
// rule that cannot work fails in front of whoever wrote it.
func (g Gate) sound() error {
	switch g.Op {
	case Older, Within:
		if _, err := time.ParseDuration(g.Value); err != nil {
			return fmt.Errorf("%w: %q is not a length of time", ErrUnreadableGate, g.Value)
		}
	case Less, More, AtMost, AtLeast:
		if _, err := strconv.ParseFloat(g.Value, 64); err != nil {
			return fmt.Errorf("%w: %q is not a number", ErrUnreadableGate, g.Value)
		}
	}
	if g.Source == Presence {
		if g.Op != Is && g.Op != IsNot {
			return fmt.Errorf("%w: presence is only ever %q or %q", ErrUnreadableGate, Here, Away)
		}
		if v := strings.ToLower(g.Value); v != Here && v != Away {
			return fmt.Errorf("%w: presence is %q or %q, not %q", ErrUnreadableGate, Here, Away, g.Value)
		}
	}
	return nil
}

// Needs : The event kind this gate reads, empty when it reads
// presence instead, and empty for a routine: neither arrives.
func (g Gate) Needs() string {
	if g.Source == Presence || routine.Mine(g.Source) {
		return ""
	}
	return g.Source
}

// Reading : The latest the server has of one event kind.
type Reading struct {
	// Value : What the sender gave, as text.
	Value string
	// At : When it arrived.
	At time.Time
	// Known : Whether there is a reading at all.
	Known bool
}

// Passes : Whether the condition holds.
//
// An unknown reading never passes. Nothing is known, so nothing is
// proved, and a sequence that does nothing is the cheaper mistake.
// The same answer [notice] gives when it cannot tell which way a
// value was moving.
//
// away is whatever the server's existing presence says, including
// its convention for not being able to tell.
func (g Gate) Passes(r Reading, away bool, now time.Time) bool {
	if g.Source == Presence {
		want := strings.EqualFold(g.Value, Here)
		if g.Op == IsNot {
			want = !want
		}
		return want != away
	}
	if !r.Known {
		return false
	}

	value := strings.TrimSpace(r.Value)
	switch g.Op {
	case Older, Within:
		d, err := time.ParseDuration(g.Value)
		if err != nil {
			return false
		}
		since := now.Sub(r.At)
		// A moment that has not come yet satisfies neither. Nothing
		// is older than a thing that has not happened, and "within
		// ten minutes" of it would otherwise be true from midnight
		// onwards -- a reading is always in the past, but what
		// somebody usually does at nine in the evening is not.
		if since < 0 {
			return false
		}
		if g.Op == Older {
			return since > d
		}
		return since <= d
	case Is:
		return strings.EqualFold(value, g.Value)
	case IsNot:
		return !strings.EqualFold(value, g.Value)
	}

	got, err := strconv.ParseFloat(value, 64)
	if err != nil {
		// A word where a number was expected. Not a crossing, and
		// not an error either: the sender simply reports something
		// this gate cannot order.
		return false
	}
	want, err := strconv.ParseFloat(g.Value, 64)
	if err != nil {
		return false
	}
	switch g.Op {
	case Less:
		return got < want
	case More:
		return got > want
	case AtMost:
		return got <= want
	case AtLeast:
		return got >= want
	}
	return false
}
