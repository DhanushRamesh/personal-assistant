package events

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// greeted : Counts hellos, and waits for one that is said in a
// goroutine.
type greeted struct {
	mu sync.Mutex
	n  int
}

func (g *greeted) Greet(context.Context, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
}

func (g *greeted) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.n
}

// settled : Waits briefly for a greeting said in the background.
func (g *greeted) settled() int {
	for i := 0; i < 100; i++ {
		if g.count() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return g.count()
}

func crossing(kind, place string, at time.Time, key string) *event.Event {
	payload, _ := json.Marshal(map[string]string{"value": place})
	return &event.Event{Kind: kind, Payload: payload, OccurredAt: at, DedupeKey: key}
}

func handler(g Greeter, here string, now time.Time) *Handler {
	h := &Handler{now: func() time.Time { return now }}
	h.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return h.Welcomes(g, here)
}

// Coming home is what greets somebody now, from their own phone.
func TestArrivingHomeSaysHello(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	h := handler(g, "home", now)

	h.welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Entered, "home", now.Add(-time.Minute), "k1")},
		[]string{"k1"})

	if n := g.settled(); n != 1 {
		t.Errorf("greeted %d times, want 1", n)
	}
}

// Arriving somewhere else is not coming home.
func TestArrivingElsewhereSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	handler(g, "home", now).welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Entered, "office", now.Add(-time.Minute), "k1")},
		[]string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for arriving at the office", n)
	}
}

// A resend is the same crossing arriving twice, and nobody walked in
// twice.
func TestAResentArrivalSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	// Stored is empty: the store recognised it and kept the first one.
	handler(g, "home", now).welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Entered, "home", now.Add(-time.Minute), "k1")},
		nil)

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for a resend", n)
	}
}

// The phone spools what it sees. A flush after a day underground
// delivers this morning's arrival at midnight, and welcoming somebody
// home hours after they got there is worse than silence.
func TestAnOldArrivalInABacklogSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	handler(g, "home", now).welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Entered, "home", now.Add(-6*time.Hour), "k1")},
		[]string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for an arrival six hours ago", n)
	}
}

// Leaving is not arriving.
func TestLeavingSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	handler(g, "home", now).welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Exited, "home", now.Add(-time.Minute), "k1")},
		[]string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for leaving", n)
	}
}

// With nowhere configured as where the assistant is, nothing is said.
func TestWithNowhereConfiguredNothingIsSaid(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	handler(g, "", now).welcome(context.Background(), "u1",
		[]*event.Event{crossing(event.Entered, "home", now.Add(-time.Minute), "k1")},
		[]string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times with nowhere configured", n)
	}
}
