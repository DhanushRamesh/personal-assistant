package presence_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/presence"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

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

// What is still to come is deliberately not said. A reminder waiting for
// four o'clock is not news at half past one, and counting them at the
// door turns a greeting into a status report.
func TestWhatIsStillToComeIsNotMentioned(t *testing.T) {
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

	if strings.Contains(got.Said, "waiting") {
		t.Errorf("greeting = %q, want nothing about what is still to come", got.Said)
	}
}

// A miss is said at the door, in words rather than as a count, and
// marked as told so the next conversation does not raise it again.
func TestAMissIsToldAtTheDoorAndOnlyOnce(t *testing.T) {
	sat := &satellite{}
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: store})

	r, err := remind.New(e.User.ID, "", remind.ScopeUser, "Bins", "Put the bins out.",
		time.Now().UTC().Add(-2*time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(t.Context(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Missed(t.Context(), r.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Missed: %v", err)
	}

	got := arrive(t, e)
	if !strings.Contains(got.Said, "Put the bins out.") {
		t.Errorf("greeting = %q, want the words of the miss, not a count", got.Said)
	}
	if got.Missed != 1 {
		t.Errorf("Missed = %d, want 1", got.Missed)
	}

	// And not again. Saying it at the door was the telling.
	if again := arrive(t, e); strings.Contains(again.Said, "Put the bins out.") {
		t.Errorf("the same miss was raised twice: %q", again.Said)
	}
	left, err := store.Unmentioned(t.Context(), e.User.ID)
	if err != nil {
		t.Fatalf("Unmentioned: %v", err)
	}
	if len(left) != 0 {
		t.Error("it was told at the door but not recorded as told")
	}
}

// The greeting itself is the hour and nothing more.
func TestTheGreetingIsJustTheHour(t *testing.T) {
	sat := &satellite{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: inmemory.New()})

	got := arrive(t, e)
	if !strings.HasSuffix(strings.TrimSpace(got.Said), "sir.") {
		t.Errorf("greeting = %q", got.Said)
	}
	if n := len(strings.Fields(got.Said)); n > 4 {
		t.Errorf("greeting = %q, %d words: it should be the hour and nothing else", got.Said, n)
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

// One fixed line per part of the day is the same sentence every morning
// for ever, and a greeting somebody can recite along with is not one.
func TestTheGreetingVaries(t *testing.T) {
	sat := &satellite{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: inmemory.New()})

	seen := map[string]bool{}
	var last string
	for i := 0; i < 20; i++ {
		got := arrive(t, e).Said
		if got == last {
			t.Fatalf("the same greeting twice running: %q", got)
		}
		if strings.Count(got, "sir") != 1 {
			t.Fatalf("greeting = %q, want one address", got)
		}
		seen[got] = true
		last = got
	}
	if len(seen) < 2 {
		t.Errorf("twenty arrivals produced one greeting: %v", seen)
	}
}

// giveUp : A satellite that abandons the request while it is speaking, the
// way Home Assistant's rest_command does when the words outlast its ten
// second timeout.
type giveUp struct {
	satellite
	cancel context.CancelFunc
	// cancelled : Whether speaking was itself cut short by that.
	cancelled bool
}

func (g *giveUp) Say(ctx context.Context, message string) error {
	g.cancel()
	// Whatever the caller did, this has to be able to finish. A real
	// satellite blocks here until the words have played.
	select {
	case <-ctx.Done():
		g.cancelled = true
	case <-time.After(10 * time.Millisecond):
	}
	return g.satellite.Say(ctx, message)
}

// The caller hanging up does not stop the greeting, and does not lose the
// reminders said along with it.
//
// Speaking blocks until the words have finished playing and the caller
// does not wait that long: Home Assistant gives up after ten seconds. Tied
// to the request, that cancelled the announcement halfway through, so
// nothing was spoken and nothing was recorded -- and the held reminders
// stayed held, to be lost the same way at the next arrival.
func TestGivingUpOnTheRequestDoesNotStopTheGreeting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := inmemory.New()
	sat := &giveUp{cancel: cancel}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: store})

	at := time.Now().UTC()
	r := remind.Reminder{
		ID: "rem_held", UserID: e.User.ID, Scope: remind.ScopeUser,
		Title: "Wake up", Body: "Wake up",
		DueAt: at.Add(-time.Minute), Status: remind.Pending,
	}
	if err := store.Create(context.Background(), &r); err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := store.Hold(context.Background(), r.ID, at); err != nil {
		t.Fatalf("holding: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/presence/arrived", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+e.Token)
	e.Serve(req)

	if sat.cancelled {
		t.Error("speaking was cut short when the caller gave up")
	}
	said := sat.spoken()
	if len(said) != 1 || !strings.Contains(said[0], "Wake up") {
		t.Fatalf("said %v, want a greeting carrying the held reminder", said)
	}

	// And recorded, so the next arrival does not say it again.
	left, err := store.Waiting(context.Background(), e.User.ID)
	if err != nil {
		t.Fatalf("reading what is still held: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d reminders still held after being said", len(left))
	}
}

// notebook : Somewhere asides are written down.
type notebook struct {
	texts []string
}

func (n *notebook) Arrived(_ context.Context, _ string, text string) {
	n.texts = append(n.texts, text)
}

// The greeting, and everything said along with it, goes into the
// conversation.
//
// Somebody told at the door that they should have done something at ten to
// four replies to that sentence. Spoken and not written down, the reply
// arrived in a conversation showing no sign of them having been spoken to,
// and there was nothing for "how late was I" to refer to.
func TestWhatIsSaidAtTheDoorIsWrittenDown(t *testing.T) {
	store := inmemory.New()
	sat := &satellite{}
	note := &notebook{}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Reminders: store, Announcements: note})

	at := time.Now().UTC()
	r := remind.Reminder{
		ID: "rem_door", UserID: e.User.ID, Scope: remind.ScopeUser,
		Title: "Call the bank", Body: "Call the bank",
		DueAt: at.Add(-time.Minute), Status: remind.Pending,
	}
	if err := store.Create(context.Background(), &r); err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := store.Hold(context.Background(), r.ID, at); err != nil {
		t.Fatalf("holding: %v", err)
	}

	got := arrive(t, e)

	if len(note.texts) != 1 {
		t.Fatalf("wrote down %v, want the one greeting", note.texts)
	}
	if note.texts[0] != got.Said {
		t.Errorf("wrote %q but said %q", note.texts[0], got.Said)
	}
	if !strings.Contains(note.texts[0], "Call the bank") {
		t.Errorf("the held reminder is missing from what was written: %q", note.texts[0])
	}
}

// Nothing is written when nothing was heard.
func TestAGreetingNobodyHeardIsNotWrittenDown(t *testing.T) {
	note := &notebook{}
	sat := &satellite{fail: errors.New("the satellite is unreachable")}
	e := apitest.NewWith(t, apitest.Options{Announcer: sat, Announcements: note})

	arrive(t, e)

	if len(note.texts) != 0 {
		t.Errorf("wrote down %v", note.texts)
	}
}
