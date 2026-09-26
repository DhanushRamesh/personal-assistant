package presence_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/presence"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// satellite : An announcer that writes down what it was asked to say.
type satellite struct {
	mu   sync.Mutex
	said []string
	fail error
}

func (s *satellite) Say(_ context.Context, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.said = append(s.said, message)
	return nil
}

func (s *satellite) Available() bool { return true }

func (s *satellite) spoken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.said...)
}

// arrive : Calls the endpoint and decodes what it answered.
func arrive(t *testing.T, e *apitest.Env) presence.ArrivedResponse {
	t.Helper()
	rec := e.Do(t, http.MethodPost, "/v1/presence/arrived", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got presence.ArrivedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return got
}

// Walking in is greeted, out loud.
func TestArrivingIsGreeted(t *testing.T) {
	sat := &satellite{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat})

	got := arrive(t, e)

	if !got.Spoke {
		t.Fatalf("nothing was spoken: %+v", got)
	}
	if len(sat.spoken()) != 1 {
		t.Fatalf("said %v, want one greeting", sat.spoken())
	}
	if !strings.Contains(got.Said, "sir") {
		t.Errorf("greeting = %q", got.Said)
	}
}

// Every call greets. Whether somebody has really been away is the
// caller's to judge, and it judges it on thirty unbroken seconds of a
// faint signal, which is better evidence than a clock here.
//
// This once refused a second greeting within ten minutes. Of three real
// arrivals in a quarter of an hour it refused two, which is the same
// silence as the fault it was there to prevent.
func TestEveryArrivalIsGreeted(t *testing.T) {
	sat := &satellite{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat})

	first := arrive(t, e)
	second := arrive(t, e)

	if !first.Spoke || !second.Spoke {
		t.Errorf("an arrival went ungreeted: %+v, %+v", first, second)
	}
	if len(sat.spoken()) != 2 {
		t.Errorf("said %v, want both", sat.spoken())
	}
}

// What is waiting is said, because it is looked up as it is said and so
// cannot be wrong.
func TestWhatIsWaitingIsMentioned(t *testing.T) {
	sat := &satellite{}
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: store})

	for _, title := range []string{"Tablets", "Bins"} {
		r, err := remind.New(e.User.ID, "", remind.ScopeUser, title, "time to "+title,
			time.Now().UTC().Add(time.Hour), remind.Once)
		if err != nil {
			t.Fatalf("remind.New: %v", err)
		}
		if err := store.Create(t.Context(), r); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got := arrive(t, e)

	if !strings.Contains(got.Said, "2 reminders are waiting") {
		t.Errorf("greeting = %q, want what is waiting", got.Said)
	}
}

// Nothing waiting is not mentioned, rather than announced as none.
func TestNothingWaitingIsNotMentioned(t *testing.T) {
	sat := &satellite{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: inmemory.New()})

	if got := arrive(t, e); strings.Contains(got.Said, "waiting") {
		t.Errorf("greeting = %q, want nothing about reminders", got.Said)
	}
}

// A greeting nobody heard is reported as not spoken, rather than as
// success, so a satellite that was busy is visible instead of silent.
func TestAGreetingNobodyHeardSaysSo(t *testing.T) {
	sat := &satellite{fail: errNoSpeaker{}}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat})

	if got := arrive(t, e); got.Spoke {
		t.Fatal("it reported speaking when the satellite refused")
	}

	sat.mu.Lock()
	sat.fail = nil
	sat.mu.Unlock()

	if got := arrive(t, e); !got.Spoke {
		t.Error("the failed attempt used up the greeting")
	}
}

type errNoSpeaker struct{}

func (errNoSpeaker) Error() string { return "satellite unreachable" }
