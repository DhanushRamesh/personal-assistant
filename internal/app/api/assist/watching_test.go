package assist_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/records/chat"
)

// settling : How long a test will wait for the states it expects.
//
// Generous because idle is held for as long as the answer takes to say,
// which the test harness's answer makes several seconds. Waiting for the
// events rather than sleeping a fixed time keeps a quick case quick.
const settling = 20 * time.Second

// states : The satellite states published while fn runs, in order,
// waiting for want of them before giving up.
func states(t *testing.T, hub *livehub.Hub, want int, fn func()) []string {
	t.Helper()
	events, stop := hub.Subscribe()
	defer stop()
	var (
		mu   sync.Mutex
		seen []string
	)
	enough := make(chan struct{})
	go func() {
		for e := range events {
			if e.Kind != livehub.SatelliteState {
				continue
			}
			mu.Lock()
			seen = append(seen, e.State)
			done := len(seen) >= want
			mu.Unlock()
			if done {
				select {
				case enough <- struct{}{}:
				default:
				}
			}
		}
	}()
	fn()
	if want > 0 {
		select {
		case <-enough:
		case <-time.After(settling):
		}
	} else {
		// Nothing is expected, so there is nothing to wait for beyond
		// giving anything wrong a moment to arrive.
		time.Sleep(200 * time.Millisecond)
	}
	stop()
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), seen...)
}

// TestAPhoneTurnMovesTheSatelliteState : the whole point of this.
//
// The laptop's satellite has an entity in Home Assistant and a watcher
// follows it, so a screen sees it work. HAwake on the phone registers no
// entity at all, so a turn spoken to the phone moved nothing and the
// client's ball sat still while Jarvis was plainly thinking.
//
// Both devices reach this endpoint, so this is where it is said. The order
// matters as much as the states: processing while it works, responding
// when the first words go out -- not at the end, by which time it has been
// speaking for seconds -- and idle once the answer stops.
func TestAPhoneTurnMovesTheSatelliteState(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	got := states(t, hub, 3, func() {
		e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
			`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)
	})

	want := []string{livehub.Processing, livehub.Responding, livehub.Idle}
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states = %v, want %v", got, want)
		}
	}

	// And it is remembered, not merely announced: a client connecting
	// after the turn, and the dashboard's own reading, both ask for the
	// latest rather than watching for the next.
	last, ok := hub.Latest(livehub.SatelliteState)
	if !ok || last.State != livehub.Idle {
		t.Errorf("latest = %+v, kept %v; want idle", last, ok)
	}
}

// A typed turn moves no microphone. The web client shares this endpoint,
// and reporting its chats as the satellite listening would put a reading
// on screen that nobody took.
func TestATypedTurnSaysNothingAboutTheSatellite(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})

	got := states(t, hub, 0, func() {
		e.Do(t, http.MethodPost, "/api/chat",
			`{"model":"assistant","messages":[{"role":"user","content":"typed, not spoken"}]}`)
	})

	if len(got) != 0 {
		t.Errorf("states = %v, want none: a typed turn is not the satellite", got)
	}
	if _, ok := hub.Latest(livehub.SatelliteState); ok {
		t.Error("a typed turn recorded a satellite state")
	}
}

// A server built without a live hub publishes nowhere and must still
// answer. The hub is optional everywhere else it is used and this is the
// one place that would panic on a nil instead of staying quiet.
func TestWithoutAHubTheTurnStillAnswers(t *testing.T) {
	e := apitest.New(t)
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"spoken aloud"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
