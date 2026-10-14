package assist_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/base/records/chat"
)

// written : One state written to Home Assistant.
type written struct {
	entity string
	state  string
}

// recorder : Stands in for Home Assistant, and records what it was told.
type recorder struct {
	mu   sync.Mutex
	got  []written
	fail error
	slow time.Duration
}

func (r *recorder) Report(ctx context.Context, entity, state string, _ map[string]any) error {
	if r.slow > 0 {
		select {
		case <-time.After(r.slow):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, written{entity, state})
	return r.fail
}

// states : What was written, in order.
func (r *recorder) states() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.got))
	for _, w := range r.got {
		out = append(out, w.state)
	}
	return out
}

// settle : Waits for want writes, or gives up.
func (r *recorder) settle(t *testing.T, want int) {
	t.Helper()
	// Generous: idle is held for as long as the answer takes to say.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := len(r.got)
		r.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestAVoiceTurnIsWrittenIntoHomeAssistant : the dashboard's half.
//
// The laptop's satellite has an entity Home Assistant keeps; HAwake on the
// phone registers none, so the card on the dashboard sat at Idle through
// every phone turn. This is the entity that is true for both.
func TestAVoiceTurnIsWrittenIntoHomeAssistant(t *testing.T) {
	ha := &recorder{}
	e := apitest.NewWith(t, apitest.Options{ShowsVoice: ha, VoiceState: "sensor.jarvis_voice"})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)

	ha.settle(t, 3)
	want := []string{"processing", "responding", "idle"}
	got := ha.states()
	if len(got) != len(want) {
		t.Fatalf("wrote %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wrote %v, want %v", got, want)
		}
	}
	ha.mu.Lock()
	defer ha.mu.Unlock()
	if ha.got[0].entity != "sensor.jarvis_voice" {
		t.Errorf("entity = %q", ha.got[0].entity)
	}
}

// TestHomeAssistantNeverHoldsUpATurn : the write is a light on a
// dashboard, and a turn must not wait for it.
//
// Written because the obvious shape -- report, then answer -- puts a
// network call on the path of every spoken word, and Home Assistant being
// slow would be heard as Jarvis being slow.
func TestHomeAssistantNeverHoldsUpATurn(t *testing.T) {
	ha := &recorder{slow: 1500 * time.Millisecond}
	e := apitest.NewWith(t, apitest.Options{ShowsVoice: ha, VoiceState: "sensor.jarvis_voice"})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	began := time.Now()
	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"spoken aloud"}]}`)
	took := time.Since(began)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if took > time.Second {
		t.Errorf("the turn took %s with Home Assistant answering in 1.5s; it waited for it", took)
	}
}

// A Home Assistant that refuses must not break the turn, and a turn with
// nowhere to write must still answer.
func TestAWriteThatFailsIsNotATurnThatFails(t *testing.T) {
	ha := &recorder{fail: context.DeadlineExceeded}
	e := apitest.NewWith(t, apitest.Options{ShowsVoice: ha, VoiceState: "sensor.jarvis_voice"})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"spoken aloud"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// And with no entity named, nothing is written at all.
	quiet := &recorder{}
	e2 := apitest.NewWith(t, apitest.Options{ShowsVoice: quiet})
	s2 := e2.LoginOn(t, "the phone", string(chat.ChannelVoice))
	e2.AsBody(t, s2.Token, http.MethodPost, "/api/chat",
		`{"model":"assistant","messages":[{"role":"user","content":"spoken aloud"}]}`)
	time.Sleep(200 * time.Millisecond)
	if got := quiet.states(); len(got) != 0 {
		t.Errorf("wrote %v with no entity named", got)
	}
}
