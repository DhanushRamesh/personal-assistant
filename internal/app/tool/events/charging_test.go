package events_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool/events"
	"personal-assistant/internal/base/world/event"
)

// battery : A reader holding battery readings and one power event.
type battery struct {
	levels []event.Event
	power  []event.Event
}

func (b *battery) Kinds(context.Context, string) ([]event.Kind, error) { return nil, nil }

func (b *battery) Recent(_ context.Context, _ string, q event.Query) ([]event.Event, error) {
	from := b.levels
	if len(q.Kinds) > 0 {
		from = b.power
	}
	out := append([]event.Event(nil), from...)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// at : A reading, newest first is how a reader hands them back.
func reading(when time.Time, kind string, value any) event.Event {
	raw, _ := json.Marshal(map[string]any{"value": value})
	return event.Event{Kind: kind, OccurredAt: when, Payload: raw}
}

// TestTheRateIsTakenOnlySincePluggingIn : The fault itself. Asked how
// long to a full charge on 18 October 2026, the model asked for two
// readings -- 23% at 22:35 and 24% at 22:47 -- and the phone had been
// plugged in at 22:46. Eleven of those twelve minutes were it
// draining, so the rate it worked out was the discharge rate and the
// answer was twelve hours against a true forty-five minutes.
func TestTheRateIsTakenOnlySincePluggingIn(t *testing.T) {
	now := time.Date(2026, 10, 18, 22, 50, 0, 0, time.UTC)
	b := &battery{
		// Newest first, as a reader gives them.
		levels: []event.Event{
			reading(now.Add(-1*time.Minute), events.Battery, 26),
			reading(now.Add(-2*time.Minute), events.Battery, 25),
			reading(now.Add(-3*time.Minute), events.Battery, 24),
			reading(now.Add(-15*time.Minute), events.Battery, 23),
			reading(now.Add(-28*time.Minute), events.Battery, 24),
		},
		power: []event.Event{reading(now.Add(-5*time.Minute), events.Plugged, "unknown")},
	}

	got := events.Moving(context.Background(), b, "usr", now)
	if got == "" {
		t.Fatal("nothing was said about a battery plainly charging")
	}
	if !strings.Contains(got, "charging") {
		t.Errorf("not said to be charging: %q", got)
	}
	// Two percent in two minutes since the plug settled, so a minute
	// a percent and about an hour and a quarter to full -- not the
	// twelve hours the straddled window gave.
	if !strings.Contains(got, "1 minute per percent") {
		t.Errorf("the rate came from the wrong window: %q", got)
	}
	// Two percent in two minutes, 74 to go: about an hour and a
	// quarter, not the twelve hours the straddled window gave.
	if !strings.Contains(got, "74 minutes") {
		t.Errorf("the time to full is wrong: %q", got)
	}
}

// TestTheJumpOnPluggingInIsNotARate : A phone gains a percent or two
// the moment it is connected. That is the jump, not a rate.
func TestTheJumpOnPluggingInIsNotARate(t *testing.T) {
	now := time.Date(2026, 10, 18, 22, 50, 0, 0, time.UTC)
	b := &battery{
		levels: []event.Event{
			reading(now.Add(-10*time.Second), events.Battery, 40),
			reading(now.Add(-50*time.Second), events.Battery, 38),
		},
		power: []event.Event{reading(now.Add(-55*time.Second), events.Plugged, "unknown")},
	}
	if got := events.Moving(context.Background(), b, "usr", now); got != "" {
		t.Errorf("a rate was given from inside the first minute: %q", got)
	}
}

// TestNothingIsSaidWithoutTwoReadings : Saying nothing is right. The
// model still has the readings and can describe what it sees.
func TestNothingIsSaidWithoutTwoReadings(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		b    *battery
	}{
		{"one reading", &battery{levels: []event.Event{reading(now, events.Battery, 50)}}},
		{"none at all", &battery{}},
		{"not moving", &battery{levels: []event.Event{
			reading(now.Add(-time.Minute), events.Battery, 50),
			reading(now.Add(-20*time.Minute), events.Battery, 50),
		}}},
	} {
		if got := events.Moving(context.Background(), tc.b, "usr", now); got != "" {
			t.Errorf("%s: said %q", tc.name, got)
		}
	}
}

// TestADrainingBatteryCountsDownToNothing : The same arithmetic the
// other way, and the one that matters when he is out.
func TestADrainingBatteryCountsDownToNothing(t *testing.T) {
	now := time.Date(2026, 10, 18, 14, 0, 0, 0, time.UTC)
	b := &battery{
		levels: []event.Event{
			reading(now.Add(-10*time.Minute), events.Battery, 40),
			reading(now.Add(-70*time.Minute), events.Battery, 50),
		},
		power: []event.Event{reading(now.Add(-3*time.Hour), events.Unplugged, "unknown")},
	}
	got := events.Moving(context.Background(), b, "usr", now)
	if !strings.Contains(got, "falling") {
		t.Errorf("a draining battery was not said to be falling: %q", got)
	}
	// Ten percent in an hour, forty left: four hours to nothing.
	if !strings.Contains(got, "4 hours") {
		t.Errorf("the time to empty is wrong: %q", got)
	}
}

// TestASpanIsSaidNotCounted : The whole reason this exists. Handed two
// figures and a subtraction a model does it in its head and gets it
// wrong: 76% at twelve minutes a percent is 912 minutes, and it said
// twelve hours.
func TestASpanIsSaidNotCounted(t *testing.T) {
	for d, want := range map[time.Duration]string{
		35 * time.Second:  "35 seconds",
		45 * time.Minute:  "45 minutes",
		2 * time.Hour:     "2 hours",
		912 * time.Minute: "15.2 hours",
		50 * time.Hour:    "2.1 days",
	} {
		if got := events.Span(d); got != want {
			t.Errorf("Span(%v) = %q, want %q", d, got, want)
		}
	}
}
