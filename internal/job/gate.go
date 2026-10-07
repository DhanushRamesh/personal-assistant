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

// Hour, Day : The gate sources that ask what time it is, rather
// than reading anything.
//
// Owner, 7 October 2026: *"time is a trigger, event is a trigger. I
// should be able to add like and and or operator -- like if time is
// 10am and battery is less 25 or location is office."*
//
// The first half of that was already possible and the second was
// not. A clock could start a rule -- "at 10:00" -- and could never
// qualify one, so "when the battery drops, but only after ten" had
// nowhere to be written. These are the other side of it.
//
//	hour >= 10        after ten in the morning
//	hour < 18         before six in the evening
//	day is saturday   on a Saturday
//	day is weekday    Monday to Friday
//
// In the person's own zone, which is the only hour that means
// anything: ten o'clock is ten where they are standing.
const (
	Hour = "hour"
	Day  = "day"
)

// Weekdays, Weekend : What a day gate may be compared against
// besides the name of a day.
//
// Because "only on a working day" is the commonest thing anybody
// wants of a clock and spelling it as five names is five chances
// to leave one out.
const (
	Weekdays = "weekday"
	Weekend  = "weekend"
)

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

// Or : What joins the alternatives of one condition.
//
// Lower case and surrounded by spaces, because it is read back to
// whoever dictated it and "or" is the word they said.
const Or = " or "

// Conditions : One condition, which may be satisfied by any of
// several things.
//
// Owner, 7 October 2026: *"I should be able to add like and and or
// operator -- like if time is 10am and battery is less 25 or
// location is office."*
//
// And was already there and is the sequence itself: two check
// steps in a row are two things that must both hold, and a step
// may stop the ones after it. Or had nowhere to go, so this is
// where it goes -- inside a single check, never between steps.
//
// Which gives all of it without nesting, parentheses or an
// expression language: steps are an and, a step is an or, and
// anything of the shape *this and that and (either of these)* can
// be written down. The line the engine holds -- linear, no
// branching, no else -- is unmoved, and it has to be: Tasker
// exists, Home Assistant exists, and a third automation language
// in this house would be a mistake.
type Conditions []Gate

// ParseConditions : A condition as it is written on a step, with
// its alternatives.
//
//	battery.level < 20
//	hour >= 10 or day is weekend
//
// Split on the word before anything else is read, so each part is
// parsed by the same function one on its own would be. An "or"
// cannot appear inside a value here, which is the one thing this
// costs: no source or operator contains the word, and the values
// that might -- a place called "Bar or Grill" -- are compared with
// is, where somebody can name it differently.
func ParseConditions(s string) (Conditions, error) {
	parts := strings.Split(strings.TrimSpace(s), Or)
	out := make(Conditions, 0, len(parts))
	for _, part := range parts {
		gate, err := ParseGate(part)
		if err != nil {
			return nil, err
		}
		out = append(out, gate)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnreadableGate, s)
	}
	return out, nil
}

// Needs : Every event kind these conditions read, without
// repeating one.
//
// Several, where a single gate needs at most one. A check that can
// be satisfied by the battery or by where somebody is has to read
// both before it knows.
func (c Conditions) Needs() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(c))
	for _, g := range c {
		kind := g.Needs()
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out
}

// Passes : Whether any of them holds.
//
// Any, because they are alternatives. A reading is looked up by
// the kind each gate asks for; one that was never fetched is
// unknown, and an unknown reading never passes, which is the same
// answer a single gate gives.
func (c Conditions) Passes(got map[string]Reading, away bool, now time.Time) bool {
	for _, g := range c {
		if g.Passes(got[g.Needs()], away, now) {
			return true
		}
	}
	return false
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
	switch g.Source {
	case Hour:
		if g.Op == Older || g.Op == Within {
			return fmt.Errorf("%w: the hour is a number, not a moment that arrived", ErrUnreadableGate)
		}
		n, err := strconv.Atoi(g.Value)
		if err != nil || n < 0 || n > 23 {
			return fmt.Errorf("%w: %q is not an hour of the day", ErrUnreadableGate, g.Value)
		}
	case Day:
		if g.Op != Is && g.Op != IsNot {
			return fmt.Errorf("%w: a day is only ever %q or %q something", ErrUnreadableGate, Is, IsNot)
		}
		if !namedDay(g.Value) {
			return fmt.Errorf("%w: %q is not a day, %q or %q",
				ErrUnreadableGate, g.Value, Weekdays, Weekend)
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
	// Answered from the moment itself rather than from anything
	// stored, so there is nothing to look up. A routine is not one
	// of these: it is a number somebody worked out and kept, and
	// it has to be fetched like any other.
	switch g.Source {
	case Presence, Hour, Day:
		return ""
	}
	return g.Source
}

// Arrives : Whether this gate reads something that lands, and so
// could set a rule off.
//
// Everything a device reports does. The clock does not, presence
// does not, and a routine does not -- a time of day is worked out
// and kept, and nothing about it ever arrives to be noticed.
func (g Gate) Arrives() bool {
	return g.Needs() != "" && !routine.Mine(g.Source)
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
	switch g.Source {
	case Hour:
		return compare(float64(now.Hour()), g.Op, g.Value)
	case Day:
		is := isDay(now.Weekday(), g.Value)
		if g.Op == IsNot {
			return !is
		}
		return is
	}
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
	return compare(got, g.Op, g.Value)
}

// compare : One number against the value written on the gate.
//
// Shared with the hour, which is a number that nothing reported.
func compare(got float64, op, value string) bool {
	want, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	switch op {
	case Less:
		return got < want
	case More:
		return got > want
	case AtMost:
		return got <= want
	case AtLeast:
		return got >= want
	case Is:
		return got == want
	case IsNot:
		return got != want
	}
	return false
}

// namedDay : Whether a value is something a day gate can mean.
func namedDay(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case Weekdays, Weekend:
		return true
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if strings.EqualFold(d.String(), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

// isDay : Whether today is the day a gate names.
//
// The day names are the ones Go prints, which are English, and that
// is the same bargain a cron expression makes: this is a condition
// somebody writes, not a word heard through a microphone, so the
// rule against matching English on what a person said does not
// reach it. "weekday" and "weekend" are here because they are what
// anybody actually wants of a clock and spelling them as five
// names is five chances to leave one out.
func isDay(today time.Weekday, value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case Weekdays:
		return today != time.Saturday && today != time.Sunday
	case Weekend:
		return today == time.Saturday || today == time.Sunday
	}
	return strings.EqualFold(today.String(), strings.TrimSpace(value))
}
