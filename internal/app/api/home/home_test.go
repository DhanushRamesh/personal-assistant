package home_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/app/api/home"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/domain/calendar"
	"personal-assistant/internal/domain/remind"
	"personal-assistant/internal/domain/remind/inmemory"
)

// diary : Calendars that answer with what they were given, or fail.
type diary struct {
	events []calendar.Event
	unread []string
	err    error
}

func (d diary) Everywhere(context.Context, string, time.Time, time.Time) ([]calendar.Event, []string, error) {
	return d.events, d.unread, d.err
}

func decode[T any](t *testing.T, e *apitest.Env, path string) T {
	t.Helper()
	rec := e.Get(t, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got T
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return got
}

// The health reading says what the server can see: since when, restarts,
// the database, the satellite, and the last turn -- and nothing it cannot.
func TestServerSaysWhatItCanSee(t *testing.T) {
	hub := livehub.NewHub()
	hub.Publish(livehub.Event{Kind: livehub.SatelliteState, State: "listening", At: time.Now()})
	started := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	e := apitest.NewWith(t, apitest.Options{
		Live:      hub,
		StartedAt: started,
		Restarts:  func(context.Context) (int, bool) { return 3, true },
	})

	got := decode[home.ServerResponse](t, e, "/v1/home/server")
	if !got.StartedAt.Equal(started) {
		t.Errorf("started_at = %v, want %v", got.StartedAt, started)
	}
	if got.Restarts == nil || *got.Restarts != 3 {
		t.Errorf("restarts = %v, want 3", got.Restarts)
	}
	if got.Database != "ok" {
		t.Errorf("database = %q, want ok", got.Database)
	}
	if got.Satellite == nil || got.Satellite.State != "listening" {
		t.Errorf("satellite = %+v, want listening", got.Satellite)
	}
	if got.LastTurn != nil {
		t.Errorf("last_turn = %+v, want none before anything was asked", got.LastTurn)
	}
}

// Outside a supervisor, and with no satellite heard, those are left out
// rather than reported as zero.
func TestServerLeavesOutWhatItCannotRead(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{})
	rec := e.Get(t, "/v1/home/server")
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"restarts", "satellite", "last_turn"} {
		if _, ok := raw[field]; ok {
			t.Errorf("%s present, want it left out: %s", field, rec.Body.String())
		}
	}
}

// Today holds what has not ended, in time order, and the reminders due by
// the end of tomorrow -- not those further off.
func TestTodayHoldsWhatIsStillToCome(t *testing.T) {
	now := time.Now()
	store := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{
		Reminders: store,
		Diary: diary{
			events: []calendar.Event{
				{Title: "Later", Starts: now.Add(3 * time.Hour), Ends: now.Add(4 * time.Hour), Calendar: "Work"},
				{Title: "Over", Starts: now.Add(-3 * time.Hour), Ends: now.Add(-2 * time.Hour)},
				{Title: "Under way", Starts: now.Add(-time.Hour), Ends: now.Add(time.Hour)},
			},
			unread: []string{"Holidays"},
		},
	})
	add := func(title string, due time.Time) {
		r, err := remind.New(e.User.ID, "", remind.ScopeUser, title, title, due, remind.Once)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Create(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	add("Next week", now.AddDate(0, 0, 7))
	add("Soon", now.Add(30*time.Minute))

	got := decode[home.TodayResponse](t, e, "/v1/home/today")
	if got.Diary != "ok" {
		t.Errorf("diary = %q, want ok", got.Diary)
	}
	titles := []string{}
	for _, ev := range got.Events {
		titles = append(titles, ev.Title)
	}
	if len(titles) != 2 || titles[0] != "Under way" || titles[1] != "Later" {
		t.Errorf("events = %v, want [Under way Later]", titles)
	}
	if len(got.Unread) != 1 || got.Unread[0] != "Holidays" {
		t.Errorf("unread = %v, want [Holidays]", got.Unread)
	}
	if len(got.Reminders) != 1 || got.Reminders[0].Title != "Soon" {
		t.Errorf("reminders = %+v, want only Soon", got.Reminders)
	}
}

// A diary that fails is said to be unavailable; with none connected it
// says so; the reminders are answered either way.
func TestTodaySaysWhyTheDiaryIsEmpty(t *testing.T) {
	failing := apitest.NewWith(t, apitest.Options{Diary: diary{err: errors.New("google is down")}})
	if got := decode[home.TodayResponse](t, failing, "/v1/home/today"); got.Diary != "unavailable" || got.Events == nil || got.Reminders == nil {
		t.Errorf("failing diary: %+v", got)
	}
	none := apitest.NewWith(t, apitest.Options{})
	if got := decode[home.TodayResponse](t, none, "/v1/home/today"); got.Diary != "not_connected" {
		t.Errorf("no diary = %q, want not_connected", got.Diary)
	}
}
