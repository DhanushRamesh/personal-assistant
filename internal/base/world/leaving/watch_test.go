package leaving_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/leaving"
)

// watcher : One wired up around a fixed clock, counting what it did.
type watcher struct {
	*leaving.Watcher
	spoke   []string
	routes  int
	journey time.Duration
	fails   bool
}

func wired(now time.Time, coming []leaving.Appointment, here bool) *watcher {
	w := &watcher{journey: 20 * time.Minute}
	w.Watcher = &leaving.Watcher{
		Diary: func(context.Context, string, time.Time, time.Time) ([]leaving.Appointment, error) {
			return coming, nil
		},
		Journey: func(context.Context, string, string) (time.Duration, error) {
			w.routes++
			if w.fails {
				return 0, errors.New("the map is down")
			}
			return w.journey, nil
		},
		Say: func(_ context.Context, _, text string) error {
			w.spoke = append(w.spoke, text)
			return nil
		},
		Here: func(string) (bool, bool) { return here, true },
		Now:  func() time.Time { return now },
	}
	return w
}

// TestItSaysItOnceAndNotAgain : The diary is read every few minutes
// and the moment lasts longer than that, so without this one film
// produces an announcement every tick until it starts.
func TestItSaysItOnceAndNotAgain(t *testing.T) {
	w := wired(at(17, 45), []leaving.Appointment{film}, true)
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(w.spoke) != 1 {
		t.Fatalf("said %d things, want 1: %v", len(w.spoke), w.spoke)
	}
	// Found again on the next tick, and the next.
	w.Now = func() time.Time { return at(17, 50) }
	_, _ = w.Look(context.Background(), "usr_1")
	w.Now = func() time.Time { return at(17, 55) }
	_, _ = w.Look(context.Background(), "usr_1")
	if len(w.spoke) != 1 {
		t.Errorf("said it %d times: %v", len(w.spoke), w.spoke)
	}
}

// TestTheRoomComesBeforeTheRoute : Somebody out of the room cannot
// hear it, and the journey would be from where they are not. Asking
// anyway spends a call on somebody else's service for nothing.
func TestTheRoomComesBeforeTheRoute(t *testing.T) {
	w := wired(at(17, 45), []leaving.Appointment{film}, false)
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(w.spoke) != 0 {
		t.Errorf("spoke to an empty room: %v", w.spoke)
	}
	if w.routes != 0 {
		t.Errorf("worked out %d journeys for nobody", w.routes)
	}
}

// TestNothingWorthARouteIsNotRouted : The fetch is the expensive
// half, so entries with nowhere to go, or hours off, never reach it.
func TestNothingWorthARouteIsNotRouted(t *testing.T) {
	nowhere := film
	nowhere.Where = ""
	far := film
	far.When = at(23, 0)

	w := wired(at(17, 0), []leaving.Appointment{nowhere, far}, true)
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if w.routes != 0 {
		t.Errorf("worked out %d journeys that could not matter", w.routes)
	}
}

// TestAMapThatIsDownSaysNothing : A guess at the moment is worse
// than no moment, and the next tick is five minutes away.
func TestAMapThatIsDownSaysNothing(t *testing.T) {
	w := wired(at(17, 45), []leaving.Appointment{film}, true)
	w.fails = true
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(w.spoke) != 0 {
		t.Errorf("guessed at a journey it could not work out: %v", w.spoke)
	}
}

// TestOneThingAtATime : Two places to be in the same window is two
// announcements, and the second is still true in five minutes.
func TestOneThingAtATime(t *testing.T) {
	other := leaving.Appointment{
		What: "Meet Vishnu", When: at(18, 30), Where: "Kilambakkam",
	}
	w := wired(at(17, 45), []leaving.Appointment{film, other}, true)
	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(w.spoke) != 1 {
		t.Errorf("said %d things at once: %v", len(w.spoke), w.spoke)
	}
	if !strings.Contains(w.spoke[0], "Meesaya Murukku 2") {
		t.Errorf("said the later one first: %q", w.spoke[0])
	}
}
