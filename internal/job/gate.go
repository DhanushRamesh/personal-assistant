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

// Links : What every gate source about a connection to somewhere
// else is called.
//
//	check link.google is broken
//
// A permission can die without anybody doing anything: Google
// expires a refresh token after seven days while an app is in
// testing, and the server finds out only when the next call
// fails. That is not a rule finding nothing -- it is a capability
// gone, and every rule standing on it will quietly do nothing
// until somebody notices.
//
// A namespace rather than one source per service, so the next
// connection needs a resolver and not a word in the grammar. The
// value is working or broken, which is all a rule can usefully
// ask: why it broke is the connection's own business and is
// already recorded beside it.
//
// Its time is when it broke, and a working one is always nought
// seconds old. So "link.google older than 1h" asks whether it has
// been down for more than an hour, in one condition -- which is
// the question worth asking, since a permission that fails once
// and recovers is not worth a sentence.
const Links = "link."

// Working, Broken : What a connection gate may be compared
// against.
const (
	Working = "working"
	Broken  = "broken"
)

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
	// Any : Nothing about the value at all -- a reading of this
	// kind arrived.
	//
	//	when power.plugged arrives
	//
	// Every other gate asks about a value, and for a measurement
	// that is the whole question: what matters about a battery is
	// what it says. For a thing that happened the value is often
	// beside the point and sometimes absent -- a phone cannot
	// always tell what it has been plugged into, and reports
	// "unknown" -- so a rule that wanted every plug-in had to say
	// `is not` against a string that could never occur. It worked
	// and it read like a trick.
	//
	// Said rather than left out. "when nonsense" would be a rule
	// that never fired and never said why, which is the failure
	// this codebase has least patience with; "when nonsense
	// arrives" is at least somebody meaning it.
	//
	// Meaningful as a trigger above all. As a condition it asks
	// whether the kind has ever been reported, which is nearly
	// always true; how recently is what [Within] is for.
	Any = "arrives"
	// Older, Within : How long ago the reading arrived, against a
	// duration such as "2h".
	Older  = "older than"
	Within = "within"
)

// ErrUnreadableGate : The condition is not one of the forms.
var ErrUnreadableGate = errors.New("job: that condition could not be read")

// Ago : What marks a value as a moment in the past rather than a
// number.
//
//	battery.level <= 15m ago
//
// Owner, 7 October 2026, on the engine's limitations: *"it can't
// compare two things."* It could only ever compare a reading with
// a number somebody wrote down, so *lower than it was an hour ago*
// had nowhere to go -- and that is the whole of knowing whether a
// phone on charge is actually charging.
//
// A value and not an operator, deliberately. Every comparison
// already there works on it unchanged: "<= 15m ago" is has not
// risen in a quarter of an hour, "> 1h ago" is higher than it was,
// "is 10m ago" is has not moved at all. Three new operators would
// have given less and needed rules of their own.
const Ago = " ago"

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
func (c Conditions) Passes(got map[string][]Reading, away bool, now time.Time) bool {
	for _, g := range c {
		if g.Holds(got[g.Needs()], away, now) {
			return true
		}
	}
	return false
}

// Deep : How far back the readings have to go for these to be
// judged, or zero when the latest of each will do.
//
// Asked of the conditions and not of each gate, because the check
// fetches once per kind: a condition satisfied by the battery now
// or by the battery an hour ago needs the hour either way.
func (c Conditions) Deep() time.Duration {
	var most time.Duration
	for _, g := range c {
		if back, against := g.Against(); against && back > most {
			most = back
		}
	}
	return most
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
	// The kind and nothing else to compare: a reading of it
	// arrived, whatever it said.
	if len(fields) == 2 && strings.EqualFold(fields[1], Any) {
		gate := Gate{Source: strings.ToLower(fields[0]), Op: Any}
		if err := gate.sound(); err != nil {
			return Gate{}, err
		}
		return gate, nil
	}
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
	// A comparison against the past, which every operator takes and
	// none of the sources answered from the moment can.
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(g.Value)), Ago) {
		if _, against := g.Against(); !against {
			return fmt.Errorf("%w: %q is not a length of time to look back",
				ErrUnreadableGate, g.Value)
		}
		switch g.Op {
		case Older, Within:
			return fmt.Errorf("%w: %q asks how old a reading is, which is not a thing to compare with the past",
				ErrUnreadableGate, g.Op)
		}
		switch g.Source {
		case Presence, Hour, Day:
			return fmt.Errorf("%w: %q has no past to compare with",
				ErrUnreadableGate, g.Source)
		}
		if routine.Mine(g.Source) {
			return fmt.Errorf("%w: %q is already a time of day",
				ErrUnreadableGate, g.Source)
		}
		return nil
	}

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
	if Linked(g.Source) {
		switch g.Op {
		case Older, Within:
			if _, err := time.ParseDuration(g.Value); err != nil {
				return fmt.Errorf("%w: %q is not a length of time", ErrUnreadableGate, g.Value)
			}
			return nil
		case Is, IsNot:
			if v := strings.ToLower(g.Value); v != Working && v != Broken {
				return fmt.Errorf("%w: a connection is %q or %q, not %q",
					ErrUnreadableGate, Working, Broken, g.Value)
			}
			return nil
		}
		return fmt.Errorf("%w: a connection is %q or %q something, or older than a length of time",
			ErrUnreadableGate, Is, IsNot)
	}

	if g.Op == Any {
		switch g.Source {
		case Presence, Hour, Day:
			return fmt.Errorf("%w: %q is not something that arrives",
				ErrUnreadableGate, g.Source)
		}
		if routine.Mine(g.Source) {
			return fmt.Errorf("%w: %q is worked out rather than reported, so nothing of it arrives",
				ErrUnreadableGate, g.Source)
		}
		return nil
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

// Against : How far back the value asks us to look, and whether it
// asks at all.
func (g Gate) Against() (time.Duration, bool) {
	value := strings.TrimSpace(g.Value)
	if !strings.HasSuffix(strings.ToLower(value), Ago) {
		return 0, false
	}
	back, err := time.ParseDuration(strings.TrimSpace(value[:len(value)-len(Ago)]))
	if err != nil || back <= 0 {
		return 0, false
	}
	return back, true
}

// Past : Whether this gate compares a reading with its own earlier
// self, and so needs more than the latest one.
func (g Gate) Past() bool {
	_, yes := g.Against()
	return yes
}

// Holds : Whether the condition holds, given what is known of the
// kind it reads.
//
// Newest first, as the store gives them. A gate comparing against
// a number wants only the first; one comparing against the past
// wants the reading from back then as well, which is why this
// takes what [Gate.Passes] could not.
func (g Gate) Holds(got []Reading, away bool, now time.Time) bool {
	back, against := g.Against()
	if !against {
		var latest Reading
		if len(got) > 0 {
			latest = got[0]
		}
		return g.Passes(latest, away, now)
	}
	if len(got) == 0 || !got[0].Known {
		return false
	}

	// The newest reading at least that old. Not the oldest held,
	// and nothing interpolated: what the value actually was then,
	// as near as anything was reported.
	want := now.Add(-back)
	var before Reading
	for _, r := range got {
		if r.Known && !r.At.After(want) {
			before = r
			break
		}
	}
	if !before.Known {
		// Nothing goes back that far, so the comparison cannot be
		// made. An unknown reading never passes, which is the same
		// answer a gate gives about a kind nobody has reported.
		return false
	}

	then, err := strconv.ParseFloat(strings.TrimSpace(before.Value), 64)
	if err != nil {
		return false
	}
	// Against the value it had then, written as the number an
	// ordinary comparison expects, so one piece of arithmetic
	// serves both.
	return Gate{Source: g.Source, Op: g.Op,
		Value: strconv.FormatFloat(then, 'f', -1, 64)}.
		Passes(got[0], away, now)
}

// Measures : Whether this gate compares a measurement rather than
// naming a thing that happened.
//
// What tells the two sorts of reading apart, which a trigger has
// to do and had been getting wrong.
//
// A measurement has a value at every moment -- a battery is always
// at some percent -- so two readings the same mean nothing
// happened, and a rule should speak when it crosses a line and not
// again while it stays over it. A thing that happened has no value
// in between: leaving home twice in a row is leaving twice, and
// the second one was being thrown away for looking like the first.
//
// Decided by the value rather than the operator, which is the
// distinction that matters and the one that is easy to get wrong.
// "battery.level is 100" reads like naming a thing and is a
// measurement; treated as an event it would speak every time the
// phone reported a full battery while it sat on the charger.
//
// No list of event kinds anywhere in it, which is the point: a
// device that starts reporting something nobody has seen is
// sorted by how its rule is written, and nothing here needs
// editing.
func (g Gate) Measures() bool {
	if g.Op == Any {
		// It asks nothing about the value, so there is no line to
		// cross and every arrival is its own occasion.
		return false
	}
	_, err := strconv.ParseFloat(strings.TrimSpace(g.Value), 64)
	return err == nil
}

// Arrives : Whether this gate reads something that lands, and so
// could set a rule off.
//
// Everything a device reports does. The clock does not, presence
// does not, a routine does not -- a time of day is worked out and
// kept -- and neither does a connection, which is read when
// somebody asks and reports how it was the last time it was
// used.
func (g Gate) Arrives() bool {
	return g.Needs() != "" && !routine.Mine(g.Source) && !Linked(g.Source)
}

// Linked : Whether a gate source asks about a connection to
// somewhere else rather than about the person.
func Linked(source string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), Links)
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
	// Nothing about the value. A reading arrived, which is the
	// whole of the question.
	if g.Op == Any {
		return true
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
