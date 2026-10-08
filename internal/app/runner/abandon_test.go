package runner

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/base/records/chat"
)

func quiet() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// sayings : An aside player that records what it was actually asked to say.
type sayings struct {
	mu   sync.Mutex
	said []string
	// takes : How long each sentence occupies the player, so a queue
	// forms behind it the way a real one does.
	takes time.Duration
}

func (s *sayings) Say(_ context.Context, text string) error {
	s.mu.Lock()
	s.said = append(s.said, text)
	s.mu.Unlock()
	return nil
}

func (s *sayings) Stop(context.Context) error { return nil }

func (s *sayings) Settled(context.Context) error {
	time.Sleep(s.takes)
	return nil
}

func (s *sayings) spoken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.said...)
}

// A failed turn must not go on announcing the work it failed to do. This is
// the shape of a real failure: several rounds each said what they were
// about to do, and the turn gave up while most of them were still queued.
func TestAFailedTurnStopsNarrating(t *testing.T) {
	player := &sayings{takes: 150 * time.Millisecond}
	r := &Runner{aside: player, logger: quiet()}
	voice := &chat.Chat{Channel: chat.ChannelVoice}
	aloud := &speech{}

	r.sayAside(context.Background(), voice, aloud, "creating a new task list")
	r.sayAside(context.Background(), voice, aloud, "checking how to create a list")
	r.sayAside(context.Background(), voice, aloud, "creating that list")

	// Long enough for the first to be playing and the rest to be queued
	// behind the gap, which is where a turn usually fails.
	time.Sleep(50 * time.Millisecond)
	aloud.stop()

	time.Sleep(AsideGap + 500*time.Millisecond)

	if got := player.spoken(); len(got) > 1 {
		t.Fatalf("kept narrating after the turn failed: %q", got)
	}
}

// Nothing may be said at all once a turn is abandoned before it starts.
func TestNothingIsSaidAfterStop(t *testing.T) {
	player := &sayings{}
	r := &Runner{aside: player, logger: quiet()}
	aloud := &speech{}
	aloud.stop()

	r.sayAside(context.Background(), &chat.Chat{Channel: chat.ChannelVoice}, aloud, "creating that list")
	time.Sleep(100 * time.Millisecond)

	if got := player.spoken(); len(got) != 0 {
		t.Fatalf("spoke after being stopped: %q", got)
	}
}

// A turn slow enough to need covering must still say what it was
// doing.
func TestASlowTurnStillNarrates(t *testing.T) {
	player := &sayings{}
	r := &Runner{aside: player, logger: quiet()}
	aloud := &speech{}

	r.sayAside(context.Background(), &chat.Chat{Channel: chat.ChannelVoice}, aloud, "looking at your inbox")
	time.Sleep(AsideAfter + 300*time.Millisecond)

	if got := player.spoken(); len(got) != 1 {
		t.Fatalf("a slow turn did not narrate: %q", got)
	}
}

// And a turn that answers quickly says nothing beside itself.
//
// The aside is there to cover a wait. Measured on 3 October 2026 it
// was being said when there was none: the median turn spent four and
// a half seconds after its tool had already returned, narrating work
// that was finished, with the answer held behind it.
func TestAQuickTurnSaysNothingBesideItself(t *testing.T) {
	player := &sayings{}
	r := &Runner{aside: player, logger: quiet()}
	aloud := &speech{}

	r.sayAside(context.Background(), &chat.Chat{Channel: chat.ChannelVoice}, aloud, "looking at your inbox")
	// The answer arrives before the hesitation is out, which is what
	// the end of every turn now does.
	time.Sleep(AsideAfter / 4)
	aloud.stop()
	time.Sleep(AsideAfter + 300*time.Millisecond)

	if got := player.spoken(); len(got) != 0 {
		t.Fatalf("narrated a turn that was already answered: %q", got)
	}
}
