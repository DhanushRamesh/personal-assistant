package inmemory_test

import (
	"context"
	"encoding/json"
	"strings"
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

// TestAmendRewritesWhatIsAlreadyThere : A stay keeps the key its start
// gives it and grows, so it comes back as a duplicate every time a
// reading extends it. Without this it would be frozen at the length it
// had when it was first noticed.
func TestAmendRewritesWhatIsAlreadyThere(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s := inmemory.New()

	first, err := event.New("usr_1", "server", "", event.Stayed, now, now,
		json.RawMessage(`{"value":"the desk","minutes":20,"still":true}`), "place.stayed:1")
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if _, _, err := s.Record(ctx, "usr_1", []*event.Event{first}); err != nil {
		t.Fatalf("storing: %v", err)
	}

	// The same stay, an hour longer. Recording is refused as a duplicate.
	grown, _ := event.New("usr_1", "server", "", event.Stayed, now, now.Add(time.Hour),
		json.RawMessage(`{"value":"the desk","minutes":80,"still":true}`), "place.stayed:1")
	stored, seen, err := s.Record(ctx, "usr_1", []*event.Event{grown})
	if err != nil {
		t.Fatalf("re-storing: %v", err)
	}
	if len(stored) != 0 || len(seen) != 1 {
		t.Fatalf("expected it to be seen already, stored=%v seen=%v", stored, seen)
	}

	changed, err := s.Amend(ctx, "usr_1", []*event.Event{grown})
	if err != nil {
		t.Fatalf("amending: %v", err)
	}
	if changed != 1 {
		t.Fatalf("expected one row amended, got %d", changed)
	}

	back, err := s.Recent(ctx, "usr_1", event.Query{Kind: event.Stayed})
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("expected one stay, got %d", len(back))
	}
	if !strings.Contains(string(back[0].Payload), `"minutes":80`) {
		t.Errorf("the stay was not brought up to date: %s", back[0].Payload)
	}
}
