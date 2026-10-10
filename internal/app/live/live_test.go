package live

import (
	"testing"
	"time"
)

func event(state string) Event {
	return Event{Kind: SatelliteState, State: state, At: time.Unix(0, 0)}
}

func TestSubscriberStartsWithTheLatestState(t *testing.T) {
	h := NewHub()
	h.Publish(event("idle"))
	h.Publish(event("listening"))
	events, stop := h.Subscribe()
	defer stop()
	if got := (<-events).State; got != "listening" {
		t.Fatalf("first event = %q, want the latest, listening", got)
	}
}

func TestPublishReachesEverySubscriber(t *testing.T) {
	h := NewHub()
	a, stopA := h.Subscribe()
	defer stopA()
	b, stopB := h.Subscribe()
	defer stopB()
	h.Publish(event("responding"))
	for _, ch := range []<-chan Event{a, b} {
		if got := (<-ch).State; got != "responding" {
			t.Errorf("got %q, want responding", got)
		}
	}
}

func TestAnUnchangedStateIsNotSentAgain(t *testing.T) {
	h := NewHub()
	events, stop := h.Subscribe()
	defer stop()
	h.Publish(event("idle"))
	h.Publish(event("idle"))
	h.Publish(event("listening"))
	if got := []string{(<-events).State, (<-events).State}; got[0] != "idle" || got[1] != "listening" {
		t.Fatalf("got %v, want [idle listening]", got)
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected extra event %q", e.State)
	default:
	}
}

func TestASubscriberThatFallsBehindIsDropped(t *testing.T) {
	h := NewHub()
	events, stop := h.Subscribe()
	defer stop()
	states := []string{"idle", "listening"}
	for i := 0; i < buffer+1; i++ {
		h.Publish(event(states[i%2]))
	}
	if h.Subscribers() != 0 {
		t.Fatal("a subscriber that never read is still subscribed")
	}
	n := 0
	for range events {
		n++
	}
	if n != buffer {
		t.Fatalf("drained %d events, want the %d buffered before the drop", n, buffer)
	}
}

func TestStoppingClosesTheChannel(t *testing.T) {
	h := NewHub()
	events, stop := h.Subscribe()
	stop()
	stop()
	if _, ok := <-events; ok {
		t.Fatal("channel still open after stop")
	}
	if h.Subscribers() != 0 {
		t.Fatal("still subscribed after stop")
	}
}
