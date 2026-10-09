package pattern_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"personal-assistant/internal/app/pattern"
	"personal-assistant/internal/base/world/event"
)

type remembers []event.Event

func (r remembers) Recent(_ context.Context, _ string, _ event.Query) ([]event.Event, error) {
	return r, nil
}

func tried(who string, at time.Time, seconds int) event.Event {
	p := fmt.Sprintf(`{"value":%q,"number":"+919000000001"}`, who)
	if seconds > 0 {
		p = fmt.Sprintf(`{"value":%q,"number":"+919000000001","seconds":%d}`, who, seconds)
	}
	return event.Event{Kind: "call.missed", OccurredAt: at, Payload: []byte(p)}
}

func held(t *testing.T, evs []event.Event, with map[string]any) bool {
	t.Helper()
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	m := pattern.Matcher{Events: remembers(evs), Now: func() time.Time { return now }}
	got, err := m.Match(context.Background(), "u", "reaching", with)
	if err != nil {
		t.Fatalf("working the shape out: %v", err)
	}
	return got
}

// TestOnceIsOrdinaryAndTwiceIsNot : the constant the shape was
// written around, now a blank.
func TestOnceIsOrdinaryAndTwiceIsNot(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	one := []event.Event{tried("Amma", now.Add(-20*time.Minute), 0)}
	two := append(one, tried("Amma", now.Add(-5*time.Minute), 0))

	if held(t, one, nil) {
		t.Error("one unanswered call is ordinary")
	}
	if !held(t, two, nil) {
		t.Error("the same person trying again is the whole point of the shape")
	}
	// And the blank moves the line.
	if held(t, two, map[string]any{"tries": 3}) {
		t.Error("asked for three, given two")
	}
}

// TestGettingThroughCancelsIt : the thing no amount of counting
// says, and the reason this is a pattern and not a gate.
//
// `counted call.missed by value within 2h >= 2` is true of both of
// these. Only one of them is somebody who needs you.
func TestGettingThroughCancelsIt(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	evs := []event.Event{
		tried("Amma", now.Add(-40*time.Minute), 0),
		tried("Amma", now.Add(-30*time.Minute), 0),
	}
	if !held(t, evs, nil) {
		t.Fatal("two unanswered calls should hold")
	}

	answered := append(evs, tried("Amma", now.Add(-10*time.Minute), 21))
	if held(t, answered, nil) {
		t.Error("they got through, so there is nothing left to say")
	}
}

// TestOnlyThisPerson : so a rule can be about one of them.
func TestOnlyThisPerson(t *testing.T) {
	now := time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)
	evs := []event.Event{
		tried("Amma", now.Add(-40*time.Minute), 0),
		tried("Amma", now.Add(-30*time.Minute), 0),
	}
	if !held(t, evs, map[string]any{"among": "amma"}) {
		t.Error("named, and matched whatever the case")
	}
	if held(t, evs, map[string]any{"among": "the office"}) {
		t.Error("somebody else entirely")
	}
}

// TestAnUnknownShapeIsBrokenNotUnsatisfied : the two must never
// read alike, or a rule standing on a typo waits for ever and says
// nothing about why.
func TestAnUnknownShapeIsBrokenNotUnsatisfied(t *testing.T) {
	m := pattern.Matcher{Events: remembers(nil)}
	_, err := m.Match(context.Background(), "u", "reachign", nil)
	if err == nil {
		t.Fatal("a pattern that does not exist should be an error")
	}
	if got := err.Error(); !contains(got, "reaching") {
		t.Errorf("the error should say what there is instead: %q", got)
	}
}

// TestABlankThatIsNotANumber : said plainly rather than ignored.
func TestABlankThatIsNotANumber(t *testing.T) {
	m := pattern.Matcher{Events: remembers(nil)}
	if _, err := m.Match(context.Background(), "u", "reaching",
		map[string]any{"tries": "lots"}); err == nil {
		t.Error(`"lots" is not a number of tries`)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
