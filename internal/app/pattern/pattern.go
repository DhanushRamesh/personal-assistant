// Package pattern works out the named shapes a rule can stand on.
//
// A rule says `{"pattern": "reaching", "with": {"tries": 2}}` and
// something has to turn that into yes or no. This is that, and it
// is the half the engine deliberately does not have: the shapes
// need events and people and the application holds both.
//
// Owner, on sizing the engine: *"I'll make the AI create rules
// based on the patterns, so it should not have limitations"*, and
// on how: *predefined templates*. So a shape is Go -- written
// once, tested, named -- and what a rule chooses is which one and
// what to put in its blanks. The determinism stays here where it
// can be tested; the choosing moves to the engine.
//
// Why a shape rather than more grammar: the engine already counts,
// groups, compares and nests, and the one thing it cannot express
// is a thing that another thing undoes. Somebody ringing twice
// matters until they get through, and no arrangement of counting
// and windows says "until". That is not a missing operator, it is
// a different question, and the answer is a shape.
package pattern

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/job"
)

// Behind : How far back a shape reads.
//
// The same three hours the situations it replaces used, and for
// the same reason: long enough that a morning's worth of trying is
// still visible at lunchtime, short enough that yesterday is not.
const Behind = 3 * time.Hour

// Events : Where the shapes read from.
type Events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Shape : One named pattern.
//
// Given the blanks as the rule wrote them and everything that has
// happened lately, it answers yes or no. Nothing else: a shape
// that returned a sentence would be a third way of saying things.
type Shape func(evs []event.Event, now time.Time, with Blanks) (bool, error)

// Of : The shapes, by the name a rule calls them.
//
// A map rather than a switch so that adding one is adding one
// thing in one place, and so that the names can be listed for
// whatever is going to write rules out of them.
var Of = map[string]Shape{
	"reaching": reaching,
}

// Names : Every shape a rule may ask for, in order.
func Names() []string {
	out := make([]string, 0, len(Of))
	for name := range Of {
		out = append(out, name)
	}
	// Sorted so a prompt listing them does not change between runs
	// and quietly invalidate whatever was measured against it.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Matcher : Works out shapes for the engine.
type Matcher struct {
	// Events : Where to read from. Required.
	Events Events
	// Now : The clock. Nil is time.Now.
	Now func() time.Time
}

// Match : Whether the named shape holds. Satisfies [job.Patterns].
//
// An unknown name is an error and never a no. A rule standing on a
// shape that does not exist is broken rather than unsatisfied, and
// a rule that quietly never holds is the failure this codebase has
// least patience with.
func (m Matcher) Match(ctx context.Context, userID, name string, with map[string]any) (bool, error) {
	shape, found := Of[strings.ToLower(strings.TrimSpace(name))]
	if !found {
		return false, fmt.Errorf("%w: there is no pattern called %q; there is %s",
			job.ErrUnreadableGate, name, strings.Join(Names(), ", "))
	}

	now := time.Now()
	if m.Now != nil {
		now = m.Now()
	}
	blanks := Blanks(with)
	since := Behind
	if d, said, err := blanks.Span("within"); err != nil {
		return false, err
	} else if said {
		since = d
	}

	evs, err := m.Events.Recent(ctx, userID, event.Query{Since: now.Add(-since)})
	if err != nil {
		return false, err
	}
	return shape(evs, now, blanks)
}

// Blanks : What a rule put in a shape's blanks.
//
// Everything arrives as text, because a gate source is one string
// and that is what it was flattened into. Read back here rather
// than in each shape, so a shape says what it wants and not how to
// parse it.
type Blanks map[string]any

// Number : A blank as a whole number, and whether it was given.
func (b Blanks) Number(key string) (int, bool, error) {
	raw, found := b[key]
	if !found {
		return 0, false, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(raw)))
	if err != nil {
		return 0, false, fmt.Errorf("%w: %s should be a number, not %q",
			job.ErrUnreadableGate, key, fmt.Sprint(raw))
	}
	return n, true, nil
}

// Span : A blank as a length of time, and whether it was given.
func (b Blanks) Span(key string) (time.Duration, bool, error) {
	raw, found := b[key]
	if !found {
		return 0, false, nil
	}
	d, err := job.Span(strings.TrimSpace(fmt.Sprint(raw)))
	if err != nil {
		return 0, false, fmt.Errorf("%w: %s should be a length of time, not %q",
			job.ErrUnreadableGate, key, fmt.Sprint(raw))
	}
	return d, true, nil
}

// Word : A blank as text, and whether it was given.
func (b Blanks) Word(key string) (string, bool) {
	raw, found := b[key]
	if !found {
		return "", false
	}
	out := strings.TrimSpace(fmt.Sprint(raw))
	return out, out != ""
}
