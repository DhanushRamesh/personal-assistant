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
	"github.com/DhanushRamesh/personal-assistant/internal/presence"
)

// greeted : Counts hellos said in the background.
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

// settled : Waits briefly for a greeting said in a goroutine.
func (g *greeted) settled() int {
	for i := 0; i < 200 && g.count() == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	return g.count()
}

// holding : An event store that hands back whatever it was given.
type holding struct{ events []event.Event }

func (h holding) Record(context.Context, string, []*event.Event) ([]string, []string, error) {
	return nil, nil, nil
}
func (h holding) Recent(context.Context, string, event.Query) ([]event.Event, error) {
	return h.events, nil
}
func (h holding) Kinds(context.Context, string) ([]event.Kind, error) { return nil, nil }

func reading(lat, lon float64, when time.Time, key string) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": event.Coordinates(lat, lon)})
	return event.Event{Kind: event.Fixed, Payload: payload, OccurredAt: when, DedupeKey: key}
}

func desk(lat, lon float64, when time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": event.Coordinates(lat, lon)})
	return event.Event{Kind: event.Standing, Payload: payload, OccurredAt: when}
}

func handler(g Greeter, history []event.Event, now time.Time) *Handler {
	h := &Handler{store: holding{events: history}, now: func() time.Time { return now }}
	h.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return h.Welcomes(g)
}

// Coming back is their phone near this machine now and not at the
// reading before.
func TestComingBackSaysHello(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	history := []event.Event{
		desk(12.91090, 80.06235, now.Add(-9*time.Hour)),
		reading(12.94690, 80.06235, now.Add(-6*time.Minute), "k0"), // four km away
		reading(12.91100, 80.06242, now.Add(-time.Minute), "k1"),   // at the desk
	}
	g := &greeted{}
	arrival := reading(12.91100, 80.06242, now.Add(-time.Minute), "k1")

	handler(g, history, now).welcome(context.Background(), "u1", []*event.Event{&arrival}, []string{"k1"})

	if n := g.settled(); n != 1 {
		t.Errorf("greeted %d times, want 1", n)
	}
}

// Sitting still is not coming back.
func TestSittingStillSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	history := []event.Event{
		desk(12.91090, 80.06235, now.Add(-9*time.Hour)),
		reading(12.91100, 80.06242, now.Add(-6*time.Minute), "k0"),
		reading(12.91105, 80.06240, now.Add(-time.Minute), "k1"),
	}
	g := &greeted{}
	still := reading(12.91105, 80.06240, now.Add(-time.Minute), "k1")

	handler(g, history, now).welcome(context.Background(), "u1", []*event.Event{&still}, []string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for sitting still", n)
	}
}

// A resend is one moment arriving twice.
func TestAResentReadingSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	again := reading(12.91100, 80.06242, now.Add(-time.Minute), "k1")

	handler(g, nil, now).welcome(context.Background(), "u1", []*event.Event{&again}, nil)

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for a resend", n)
	}
}

// A spool flushed after a day underground delivers this morning's
// walk home at midnight.
func TestAnOldReadingInABacklogSaysNothing(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	g := &greeted{}
	old := reading(12.91100, 80.06242, now.Add(-6*time.Hour), "k1")

	handler(g, nil, now).welcome(context.Background(), "u1", []*event.Event{&old}, []string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times for a reading six hours old", n)
	}
}

// An assistant that has never been spoken to out loud does not know
// where it is, so nobody can have arrived at it.
func TestWithNowhereKnownNothingIsSaid(t *testing.T) {
	now := time.Date(2026, 10, 2, 18, 0, 0, 0, time.UTC)
	history := []event.Event{
		reading(12.94690, 80.06235, now.Add(-6*time.Minute), "k0"),
		reading(12.91100, 80.06242, now.Add(-time.Minute), "k1"),
	}
	g := &greeted{}
	arrival := reading(12.91100, 80.06242, now.Add(-time.Minute), "k1")

	handler(g, history, now).welcome(context.Background(), "u1", []*event.Event{&arrival}, []string{"k1"})

	if n := g.count(); n != 0 {
		t.Errorf("greeted %d times with nowhere known", n)
	}
}

// Ensure the store stand-in satisfies what the handler needs.
var _ Store = holding{}
var _ presence.Reader = holding{}
