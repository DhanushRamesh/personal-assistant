package pattern_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"personal-assistant/internal/app/pattern"
	"personal-assistant/internal/base/world/event"
)

func paired(kind, what string, at time.Time) event.Event {
	return event.Event{Kind: kind, OccurredAt: at,
		Payload: []byte(fmt.Sprintf(`{"value":%q}`, what))}
}

func on(t *testing.T, evs []event.Event, with map[string]any) bool {
	t.Helper()
	now := time.Date(2026, 10, 9, 19, 30, 0, 0, time.UTC)
	m := pattern.Matcher{Events: remembers(evs), Now: func() time.Time { return now }}
	got, err := m.Match(context.Background(), "u", "still", with)
	if err != nil {
		t.Fatalf("working it out: %v", err)
	}
	return got
}

var helmet = map[string]any{
	"on": "bluetooth.connected", "off": "bluetooth.disconnected", "is": "bike helmet",
}

// TestWhicheverHappenedLast : the whole question, and the one the
// grammar cannot ask. "Connected" is true of a helmet unpaired an
// hour ago; "no disconnect lately" is true of one unpaired an hour
// ago too. Only the order settles it.
func TestWhicheverHappenedLast(t *testing.T) {
	now := time.Date(2026, 10, 9, 19, 30, 0, 0, time.UTC)

	stillOn := []event.Event{
		paired("bluetooth.disconnected", "bike helmet", now.Add(-3*time.Hour)),
		paired("bluetooth.connected", "bike helmet", now.Add(-20*time.Minute)),
	}
	if !on(t, stillOn, helmet) {
		t.Error("connected after the last disconnect is still connected")
	}

	takenOff := append(stillOn,
		paired("bluetooth.disconnected", "bike helmet", now.Add(-2*time.Minute)))
	if on(t, takenOff, helmet) {
		t.Error("disconnected since, so it is not still connected")
	}
}

// TestOnlyThatThing : the earbuds coming off must not answer for
// the helmet.
func TestOnlyThatThing(t *testing.T) {
	now := time.Date(2026, 10, 9, 19, 30, 0, 0, time.UTC)
	evs := []event.Event{
		paired("bluetooth.connected", "bike helmet", now.Add(-20*time.Minute)),
		paired("bluetooth.disconnected", "earbuds", now.Add(-time.Minute)),
	}
	if !on(t, evs, helmet) {
		t.Error("the earbuds came off, not the helmet")
	}
}

// TestNeverStartedIsNotStillGoing : an absence proves nothing, and
// a rule that fired because it had never heard of the thing would
// be worse than one that stayed quiet.
func TestNeverStartedIsNotStillGoing(t *testing.T) {
	if on(t, nil, helmet) {
		t.Error("nothing has ever connected")
	}
}

// TestItSaysWhatItNeeds : rather than quietly answering no.
func TestItSaysWhatItNeeds(t *testing.T) {
	m := pattern.Matcher{Events: remembers(nil)}
	for _, with := range []map[string]any{
		{"off": "bluetooth.disconnected"},
		{"on": "bluetooth.connected"},
		{"on": "a.b", "off": "a.b"},
	} {
		if _, err := m.Match(context.Background(), "u", "still", with); err == nil {
			t.Errorf("%v should have been refused", with)
		}
	}
}
