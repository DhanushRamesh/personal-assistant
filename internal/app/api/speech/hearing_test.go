package speech_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	livehub "personal-assistant/internal/app/live"
)

// The words heard so far go straight to that person's clients, as a
// notice: not remembered, so a client that connects later is not sent them.
func TestHearingIsPassedOnAndNotKept(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	events, stop := hub.Subscribe()
	defer stop()

	rec := e.Do(t, http.MethodPost, "/v1/speech/hearing", `{"text":"what is on my calendar","final":false}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	select {
	case got := <-events:
		if got.Kind != livehub.Hearing || got.Text != "what is on my calendar" || got.Final || got.UserID != e.User.ID {
			t.Errorf("event = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no hearing notice")
	}
	if _, kept := hub.Latest(livehub.Hearing); kept {
		t.Error("a hearing notice was kept as a state")
	}
}

// How long the turn will still wait for a word goes with the words, held
// to what a client can count down; a notice without one says none.
func TestHearingCarriesTheWait(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	events, stop := hub.Subscribe()
	defer stop()

	for _, c := range []struct {
		body string
		want *float64
	}{
		{`{"text":"","final":false,"wait":2}`, ptr(2)},
		{`{"text":"what is","final":false,"wait":0.74}`, ptr(0.74)},
		{`{"text":"what is","final":false,"wait":-1}`, ptr(0)},
		{`{"text":"what is","final":false,"wait":900}`, ptr(30)},
		{`{"text":"what is the time","final":true}`, nil},
	} {
		if rec := e.Do(t, http.MethodPost, "/v1/speech/hearing", c.body); rec.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d", c.body, rec.Code)
		}
		got := <-events
		if (got.Wait == nil) != (c.want == nil) || (got.Wait != nil && *got.Wait != *c.want) {
			t.Errorf("%s: wait = %v, want %v", c.body, got.Wait, c.want)
		}
	}
}

func ptr(v float64) *float64 { return &v }

// What is heard is cut from the front past its limit, keeping what is
// being said now; a body that is not JSON is refused.
func TestHearingKeepsTheLatestWords(t *testing.T) {
	hub := livehub.NewHub()
	e := apitest.NewWith(t, apitest.Options{Live: hub})
	events, stop := hub.Subscribe()
	defer stop()

	long := strings.Repeat("a", 700) + "END"
	e.Do(t, http.MethodPost, "/v1/speech/hearing", `{"text":"`+long+`","final":true}`)
	got := <-events
	if len([]rune(got.Text)) != 600 || !strings.HasSuffix(got.Text, "END") || !got.Final {
		t.Errorf("kept %d characters, final %v", len(got.Text), got.Final)
	}
	if rec := e.Do(t, http.MethodPost, "/v1/speech/hearing", `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// written : A Recorder that keeps what it was given.
type written struct{ user, heard, said string }

func (w *written) Elsewhere(_ context.Context, userID, heard, said string) {
	w.user, w.heard, w.said = userID, heard, said
}

// A turn Home Assistant answered itself is handed on to be written down,
// for the person whose credential reported it.
func TestATurnAnsweredElsewhereIsRecorded(t *testing.T) {
	rec := &written{}
	e := apitest.NewWith(t, apitest.Options{Answered: rec})

	got := e.Do(t, http.MethodPost, "/v1/speech/answered", `{"heard":"hello","said":"At your service, sir."}`)
	if got.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", got.Code, got.Body.String())
	}
	if rec.user != e.User.ID || rec.heard != "hello" || rec.said != "At your service, sir." {
		t.Errorf("recorded %+v", rec)
	}
}
