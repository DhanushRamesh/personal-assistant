package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"personal-assistant/internal/base/world/announce/hass"
	"personal-assistant/internal/base/world/event"
)

// TestWhatHomeAssistantSaysBecomesAReading : The one place a rule's
// view of an entity is built. Everything about it that is not the
// plain answer -- unknown, unavailable, an entity that was renamed
// -- has to come back as nothing known rather than as a fault,
// because an unknown reading never passes and a fault would be in
// the log every two minutes.
func TestWhatHomeAssistantSaysBecomesAReading(t *testing.T) {
	at := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	says := func(state string, err error) readings {
		return readings{hass: func(context.Context, string) (string, time.Time, error) {
			return state, at, err
		}}
	}

	got, err := says("charging", nil).Latest(context.Background(), "u", "ha.sensor.pixel_7_battery_state", 2)
	if err != nil {
		t.Fatalf("reading an entity failed: %v", err)
	}
	if len(got) != 1 || got[0].Value != "charging" || !got[0].Known {
		t.Fatalf("got %+v, want one known reading of charging", got)
	}
	if !got[0].At.Equal(at) {
		t.Errorf("kept the wrong moment: %v", got[0].At)
	}

	// One reading and never two. There is no history to compare
	// against, which is why these may not be triggers.
	if len(got) > 1 {
		t.Error("offered a Home Assistant entity a previous reading to cross")
	}

	// Home Assistant's own words for not knowing, and an entity
	// that has been renamed out from under a rule.
	for _, c := range []struct {
		why   string
		state string
		err   error
	}{
		{"unknown", "unknown", nil},
		{"unavailable", "unavailable", nil},
		{"empty", "", nil},
		{"renamed", "", hass.ErrNoEntity},
		{"no Home Assistant", "", hass.ErrNotConfigured},
	} {
		got, err := says(c.state, c.err).Latest(context.Background(), "u", "ha.binary_sensor.x", 2)
		if err != nil {
			t.Errorf("%s came back as a fault: %v", c.why, err)
		}
		if len(got) != 0 {
			t.Errorf("%s came back as a reading: %+v", c.why, got)
		}
	}

	// A real failure is still a failure. A rule should not act as
	// though it asked and was told nothing when it never got to
	// ask at all.
	broken := errors.New("connection refused")
	if _, err := says("", broken).Latest(context.Background(), "u", "ha.sensor.x", 2); err == nil {
		t.Error("swallowed a Home Assistant that could not be reached")
	}

	// No Home Assistant configured at all.
	none := readings{}
	if got, err := none.Latest(context.Background(), "u", "ha.sensor.x", 2); err != nil || len(got) != 0 {
		t.Errorf("a server with no Home Assistant gave %+v, %v", got, err)
	}
}

// TestACountBecomesAReading : A number where every other source
// gives a value, which is what lets every comparison already there
// work on it unchanged.
func TestACountBecomesAReading(t *testing.T) {
	at := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	var asked event.Tally
	r := readings{
		now: func() time.Time { return at },
		counts: func(_ context.Context, _ string, t event.Tally) (int, error) {
			asked = t
			return 6, nil
		},
	}

	got, err := r.Latest(context.Background(), "u",
		"counted call.missed by value within 2h", 2)
	if err != nil {
		t.Fatalf("counting failed: %v", err)
	}
	if len(got) != 1 || got[0].Value != "6" || !got[0].Known {
		t.Fatalf("got %+v, want one known reading of 6", got)
	}
	if asked.Kind != "call.missed" || !asked.Group || asked.Match != "" {
		t.Errorf("asked the store for %+v", asked)
	}
	if want := at.Add(-2 * time.Hour); !asked.Since.Equal(want) {
		t.Errorf("looked back to %v, want %v", asked.Since, want)
	}

	// Nought is known. Everywhere else nothing known is a condition
	// that does not hold; here the answer was looked for and found
	// to be none, which is the only way to write a rule about a
	// thing NOT happening.
	none := readings{now: func() time.Time { return at },
		counts: func(context.Context, string, event.Tally) (int, error) { return 0, nil }}
	got, err = none.Latest(context.Background(), "u", "counted call.missed within 2h", 2)
	if err != nil || len(got) != 1 || got[0].Value != "0" || !got[0].Known {
		t.Errorf("a count of none came back as %+v, %v -- it should be known", got, err)
	}

	// A source that cannot be read is a fault, not a quiet nothing:
	// it was written wrongly and somebody has to be told.
	if _, err := r.Latest(context.Background(), "u", "counted nonsense", 2); err == nil {
		t.Error("accepted a count that could not be read")
	}

	// No counter at all leaves it unknown, like every other source.
	if got, err := (readings{}).Latest(context.Background(), "u",
		"counted call.missed within 2h", 2); err != nil || len(got) != 0 {
		t.Errorf("a server with no counter gave %+v, %v", got, err)
	}
}
