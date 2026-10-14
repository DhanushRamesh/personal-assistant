package assist_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/speech"
)

// at : When each satellite state was published, relative to the start.
type at struct {
	state string
	when  time.Duration
}

// timed : The satellite states published while fn runs, with when.
//
// fn returns how much longer to watch for, because the answer decides
// it: idle is held for as long as the words take to say, and the words
// are not known until the turn has answered.
func timed(t *testing.T, hub *livehub.Hub, fn func() time.Duration) []at {
	t.Helper()
	events, stop := hub.Subscribe()
	defer stop()
	began := time.Now()
	done := make(chan []at, 1)
	go func() {
		var seen []at
		for e := range events {
			if e.Kind == livehub.SatelliteState {
				seen = append(seen, at{e.State, time.Since(began)})
			}
		}
		done <- seen
	}()
	time.Sleep(fn())
	stop()
	select {
	case seen := <-done:
		return seen
	case <-time.After(2 * time.Second):
		t.Fatal("the subscription never closed")
		return nil
	}
}

// TestSpeakingIsHeldForAsLongAsItTakesToSay : the ball's whole problem.
//
// Measured on a real turn before this: processing ran 7.3 seconds and
// responding lasted 61 milliseconds. The server finishes the moment the
// words are handed over; the assistant then speaks for seconds that
// nothing reports. So the screen went dark while Jarvis was talking.
//
// The words are in hand before any of them are played, so how long they
// will take is knowable. Idle waits for it.
func TestSpeakingIsHeldForAsLongAsItTakesToSay(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	var answered string
	got := timed(t, hub, func() time.Duration {
		rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
			`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)
		answered = spokenWords(t, rec.Body.String())
		return speech.Saying(answered) + time.Second
	})

	if len(got) != 3 {
		t.Fatalf("states = %v, want processing, responding, idle", got)
	}
	if got[0].state != livehub.Processing || got[1].state != livehub.Responding || got[2].state != livehub.Idle {
		t.Fatalf("states = %v", got)
	}

	held := got[2].when - got[1].when
	want := speech.Saying(answered)
	t.Logf("answered %d characters, which takes %s to say; responding was held %s",
		len([]rune(answered)), want.Round(time.Millisecond), held.Round(time.Millisecond))

	if want <= 0 {
		t.Fatal("the test answered nothing, so there is no speaking to hold")
	}
	// Generous: the point is that it is held for about as long as the
	// speech, not that a timer is precise.
	if held < want-300*time.Millisecond || held > want+700*time.Millisecond {
		t.Errorf("responding held %s, want about %s", held, want)
	}
	// And the thing this replaces: it used to go out in milliseconds.
	if held < 500*time.Millisecond {
		t.Errorf("responding lasted %s; the screen would go dark while it talks", held)
	}
}

// A held idle must never land on a turn that started after it. There is
// no later state to correct it -- idle published after the next turn's
// processing leaves a screen dark for the whole of that answer.
func TestAHeldIdleNeverLandsOnTheNextTurn(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	satellite := e.LoginOn(t, "the phone", string(chat.ChannelVoice))

	ask := func() {
		e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
			`{"model":"assistant","messages":[{"role":"user","content":"what is the time"}]}`)
	}
	var answered string
	got := timed(t, hub, func() time.Duration {
		ask()
		// Straight back in, while the first turn's idle is still held.
		rec := e.AsBody(t, satellite.Token, http.MethodPost, "/api/chat",
			`{"model":"assistant","messages":[{"role":"user","content":"and the date"}]}`)
		answered = spokenWords(t, rec.Body.String())
		return speech.Saying(answered) + time.Second
	})
	_ = answered

	t.Logf("states: %v", got)
	// Whatever else happened, idle must not be followed by processing
	// of a turn that had already started -- and the last word must be
	// idle, once everything has settled.
	if len(got) == 0 || got[len(got)-1].state != livehub.Idle {
		t.Fatalf("states = %v, want them to settle on idle", got)
	}
	for i := 0; i+1 < len(got); i++ {
		if got[i].state == livehub.Idle && got[i+1].state == livehub.Responding {
			t.Errorf("idle landed before a responding that was already under way: %v", got)
		}
	}
}

// spokenWords : Everything the stream said, joined, which is what was
// handed over to be spoken.
func spokenWords(t *testing.T, body string) string {
	t.Helper()
	var said string
	for _, line := range splitLines(body) {
		var c struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if decodeInto(line, &c) == nil {
			said += c.Message.Content
		}
	}
	return said
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func decodeInto(line string, v any) error {
	return json.Unmarshal([]byte(line), v)
}
