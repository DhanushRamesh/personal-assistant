package situation_test

import (
	"context"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/situation"
)

// held : A reader holding one window, whatever is asked of it.
type held struct {
	evs  []event.Event
	read int
}

func (h *held) Recent(context.Context, string, event.Query) ([]event.Event, error) {
	h.read++
	return h.evs, nil
}

func found(key, says string) situation.Check {
	return func([]event.Event, time.Time) (situation.Found, bool) {
		return situation.Found{Key: key, Says: says}, true
	}
}

func nothing() situation.Check {
	return func([]event.Event, time.Time) (situation.Found, bool) {
		return situation.Found{}, false
	}
}

func watching(store *held, here bool, checks ...situation.Check) (*situation.Watcher, *[]string) {
	var spoke []string
	w := &situation.Watcher{
		Events: store,
		Checks: checks,
		Say: func(_ context.Context, _, text string) error {
			spoke = append(spoke, text)
			return nil
		},
		Here: func(string) (bool, bool) { return here, true },
		Now:  func() time.Time { return now },
	}
	return w, &spoke
}

// TestOneThingAtADesk : Two situations at once is two things to
// say, and a person told two things at a desk remembers neither.
// The second is still true in two minutes.
func TestOneThingAtADesk(t *testing.T) {
	store := &held{}
	w, spoke := watching(store, true,
		found("a", "The first thing."), found("b", "The second thing."))

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(*spoke) != 1 || (*spoke)[0] != "The first thing." {
		t.Errorf("said %v", *spoke)
	}
}

// TestTheRoomComesFirst : Nothing here is urgent enough to be worth
// reading a window for somebody who cannot hear the answer.
func TestTheRoomComesFirst(t *testing.T) {
	store := &held{}
	w, spoke := watching(store, false, found("a", "Something."))

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(*spoke) != 0 {
		t.Errorf("spoke to an empty room: %v", *spoke)
	}
	if store.read != 0 {
		t.Errorf("read the window %d times for nobody", store.read)
	}
}

// TestACableIsNotMentionedEveryTwoMinutes : These are not events
// that recur. A cable that is not in stays not in, and saying so on
// every tick is how an assistant gets switched off.
func TestACableIsNotMentionedEveryTwoMinutes(t *testing.T) {
	store := &held{}
	w, spoke := watching(store, true, found("a", "The cable is not in."))

	for i := 0; i < 5; i++ {
		if _, err := w.Look(context.Background(), "usr_1"); err != nil {
			t.Fatalf("look %d: %v", i, err)
		}
	}
	if len(*spoke) != 1 {
		t.Errorf("said it %d times: %v", len(*spoke), *spoke)
	}
}

// TestNothingFoundIsNothingSaid : Most runs. No tick should produce
// a sentence merely because it happened.
func TestNothingFoundIsNothingSaid(t *testing.T) {
	store := &held{}
	w, spoke := watching(store, true, nothing(), nothing())

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(*spoke) != 0 {
		t.Errorf("found something in nothing: %v", *spoke)
	}
}

// TestNoChecksFindNothing : An empty list is the safe failure, not
// a reason to invent one.
func TestNoChecksFindNothing(t *testing.T) {
	store := &held{}
	w, spoke := watching(store, true)

	if _, err := w.Look(context.Background(), "usr_1"); err != nil {
		t.Fatalf("look: %v", err)
	}
	if len(*spoke) != 0 || store.read != 0 {
		t.Errorf("did something with no checks: %v", *spoke)
	}
}
