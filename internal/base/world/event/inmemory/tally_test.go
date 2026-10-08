package inmemory_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/event/inmemory"
)

var when = time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)

func back(d time.Duration) time.Time { return when.Add(-d) }

// TestCountingIsSearchingAndGroupingToo : The three questions a
// count answers are one form and not three, because the loop is the
// part nobody should have to write.
func TestCountingIsSearchingAndGroupingToo(t *testing.T) {
	s := inmemory.New()
	n := 0
	add := func(kind, value string, ago time.Duration) {
		n++
		e := &event.Event{
			Kind: kind, Source: "tasker", OccurredAt: back(ago),
			DedupeKey: fmt.Sprintf("k%d", n),
			Payload:   []byte(fmt.Sprintf(`{"value":%q}`, value)),
		}
		if _, _, err := s.Record(context.Background(), "u", []*event.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	// Six from one caller, one from another, one outside the window.
	for _, ago := range []time.Duration{10, 20, 30, 40, 50, 60} {
		add("call.missed", "unknown", ago*time.Minute)
	}
	add("call.missed", "Megatron", 70*time.Minute)
	add("call.missed", "Amma", 5*time.Hour)
	add("battery.level", "41", 10*time.Minute)

	count := func(t2 event.Tally) int {
		t.Helper()
		n, err := s.Tally(context.Background(), "u", t2)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	two := event.Tally{Kind: "call.missed", Since: back(2 * time.Hour)}

	if got := count(two); got != 7 {
		t.Errorf("counted %d in the window, want 7", got)
	}
	// Searching: only that caller.
	narrowed := two
	narrowed.Match = "unknown"
	if got := count(narrowed); got != 6 {
		t.Errorf("counted %d from unknown, want 6", got)
	}
	// Case is not the question.
	narrowed.Match = "MEGATRON"
	if got := count(narrowed); got != 1 {
		t.Errorf("counted %d from Megatron, want 1 -- case should not matter", got)
	}
	// Grouping: the largest, without having to name anybody.
	grouped := two
	grouped.Group = true
	if got := count(grouped); got != 6 {
		t.Errorf("largest group was %d, want 6", got)
	}
	// The window is real.
	narrowed = event.Tally{Kind: "call.missed", Since: back(time.Hour), Match: "unknown"}
	if got := count(narrowed); got != 6 {
		t.Errorf("counted %d in the last hour, want 6", got)
	}
	// A thing that did not happen counts as none, not as an error.
	if got := count(event.Tally{Kind: "door.opened", Since: back(time.Hour)}); got != 0 {
		t.Errorf("counted %d of a kind nobody has ever sent, want 0", got)
	}
	// Another person's events are not theirs.
	if n, err := s.Tally(context.Background(), "someone_else", two); err != nil || n != 0 {
		t.Errorf("counted %d of another person's events (%v)", n, err)
	}
}

// TestAReadingWithNoValueIsInNoGroup : There is nothing to group it
// by, so it cannot be the thing that makes a group the largest.
func TestAReadingWithNoValueIsInNoGroup(t *testing.T) {
	s := inmemory.New()
	for i := range 5 {
		e := &event.Event{Kind: "thing.happened", Source: "x",
			OccurredAt: back(time.Duration(i+1) * time.Minute),
			DedupeKey:  fmt.Sprintf("n%d", i),
			Payload:    []byte(`{"level":"9"}`)}
		if _, _, err := s.Record(context.Background(), "u", []*event.Event{e}); err != nil {
			t.Fatal(err)
		}
	}
	w := event.Tally{Kind: "thing.happened", Since: back(time.Hour)}
	if n, _ := s.Tally(context.Background(), "u", w); n != 5 {
		t.Errorf("plain count was %d, want 5", n)
	}
	w.Group = true
	if n, _ := s.Tally(context.Background(), "u", w); n != 0 {
		t.Errorf("grouped count was %d, want 0 -- nothing had a value to group by", n)
	}
}
