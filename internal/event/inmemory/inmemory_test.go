package inmemory_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
)

func at(t *testing.T, userID, kind string, when time.Time) *event.Event {
	t.Helper()
	e, err := event.New(userID, "tasker", "pixel-7", kind, when, when,
		json.RawMessage(`{"value":"12.911,80.062"}`),
		userID+kind+when.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatalf("building an event: %v", err)
	}
	return e
}

// TestForgetDropsOldReadingsAndNothingElse : The prune takes the kind it
// is given, older than the moment it is given, for everybody -- and
// leaves the history alone.
func TestForgetDropsOldReadingsAndNothingElse(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)

	s := inmemory.New()
	for _, who := range []string{"usr_1", "usr_2"} {
		_, _, err := s.Record(ctx, who, []*event.Event{
			at(t, who, event.Fixed, old),    // goes
			at(t, who, event.Fixed, recent), // too new
			at(t, who, event.Stayed, old),   // history, kept for ever
			at(t, who, event.Entered, old),  // likewise
		})
		if err != nil {
			t.Fatalf("storing: %v", err)
		}
	}

	gone, err := s.Forget(ctx, event.Fixed, now.Add(-event.Keep))
	if err != nil {
		t.Fatalf("forgetting: %v", err)
	}
	if gone != 2 {
		t.Errorf("expected one old reading per person, forgot %d", gone)
	}

	for _, who := range []string{"usr_1", "usr_2"} {
		left, err := s.Recent(ctx, who, event.Query{})
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		if len(left) != 3 {
			t.Fatalf("%s: expected three left, got %d", who, len(left))
		}
		for _, e := range left {
			if e.Kind == event.Fixed && e.OccurredAt.Equal(old) {
				t.Errorf("%s: the old reading survived", who)
			}
		}
	}
}

// TestOmitLeavesAKindOut : What the events tool leans on.
func TestOmitLeavesAKindOut(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	s := inmemory.New()
	if _, _, err := s.Record(ctx, "usr_1", []*event.Event{
		at(t, "usr_1", event.Fixed, now),
		at(t, "usr_1", event.Stayed, now),
	}); err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := s.Recent(ctx, "usr_1", event.Query{Omit: []string{event.Fixed}})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if len(got) != 1 || got[0].Kind != event.Stayed {
		t.Fatalf("expected the stay alone, got %v", got)
	}
}
