package events_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/event/inmemory"
	"personal-assistant/internal/domain/places"
	placesmemory "personal-assistant/internal/domain/places/inmemory"
)

// told : Records the kinds a batch said had landed.
type told struct {
	mu    sync.Mutex
	kinds []string
}

func (t *told) Arrived(_ string, kinds []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.kinds = append(t.kinds, kinds...)
}

func (t *told) has(kind string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range t.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// drawn : A server with Home drawn, 150 m round its stays.
func drawn(t *testing.T, opts apitest.Options) (*apitest.Env, *inmemory.Store, places.Place) {
	t.Helper()
	store := inmemory.New()
	kept := placesmemory.New()
	opts.DeviceEvents, opts.Places = store, kept
	e := apitest.NewWith(t, opts)
	home := places.Place{ID: places.NewID(), UserID: e.User.ID, Name: "Home",
		Lat: 12.91080, Lng: 80.06215, Radius: 150}
	if err := kept.Add(context.Background(), home); err != nil {
		t.Fatal(err)
	}
	return e, store, home
}

// fixAt : One fix, this many metres north of home, this many minutes ago.
func fixAt(home places.Place, key string, minutesAgo int, metres float64) string {
	at := time.Now().UTC().Add(-time.Duration(minutesAgo) * time.Minute).Format(time.RFC3339)
	return fmt.Sprintf(`{"kind":"location.fix","occurred_at":%q,"payload":{"value":"%.6f,%.6f"},"dedupe_key":%q}`,
		at, home.Lat+metres/111195.0, home.Lng, key)
}

func crossings(store *inmemory.Store, user string) []string {
	var out []string
	for _, e := range store.All(user) {
		if e.Kind == event.Entered || e.Kind == event.Exited {
			out = append(out, e.Source+" "+e.Kind+" "+event.Value(e.Payload))
		}
	}
	return out
}

// A fix inside writes the entry, as the server, and rules hear of it;
// one by the edge does not leave; one past the margin does; the same
// batch sent again writes nothing more.
func TestFixesCrossThePlacesKept(t *testing.T) {
	arrivals := &told{}
	e, store, home := drawn(t, apitest.Options{Arrivals: arrivals})

	post(t, e, `{"source":"tasker","device":"pixel","events":[`+fixAt(home, "f1", 30, 600)+`,`+fixAt(home, "f2", 25, 20)+`]}`)
	if got := crossings(store, e.User.ID); len(got) != 1 || got[0] != "server place.entered Home" {
		t.Fatalf("after arriving: %v", got)
	}
	if !arrivals.has(event.Entered) {
		t.Errorf("rules were told %v, not the entry", arrivals.kinds)
	}
	for _, c := range store.All(e.User.ID) {
		if c.Kind == event.Entered && c.Device != "pixel" {
			t.Errorf("the crossing's device is %q, want the fix's", c.Device)
		}
	}

	post(t, e, `{"source":"tasker","device":"pixel","events":[`+fixAt(home, "f3", 20, 190)+`]}`)
	if got := crossings(store, e.User.ID); len(got) != 1 {
		t.Fatalf("a fix within the margin crossed: %v", got)
	}

	leaving := `{"source":"tasker","device":"pixel","events":[` + fixAt(home, "f4", 15, 260) + `]}`
	post(t, e, leaving)
	post(t, e, leaving)
	got := crossings(store, e.User.ID)
	if len(got) != 2 || got[1] != "server place.exited Home" {
		t.Fatalf("after leaving, and the batch sent again: %v", got)
	}
}

// Tasker's crossing for a place kept here is not stored, and is
// answered as seen so the phone deletes it; one for anywhere else is
// stored as before.
func TestTaskersCrossingsForAKeptPlaceAreIgnored(t *testing.T) {
	e, store, _ := drawn(t, apitest.Options{})
	at := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	_, out := post(t, e, `{"source":"tasker","events":[
		{"kind":"place.entered","occurred_at":"`+at+`","payload":{"value":"home"},"dedupe_key":"t1"},
		{"kind":"place.exited","occurred_at":"`+at+`","payload":{"value":"Gym"},"dedupe_key":"t2"}]}`)

	if got := crossings(store, e.User.ID); len(got) != 1 || got[0] != "tasker place.exited Gym" {
		t.Fatalf("stored %v, want only the gym's", got)
	}
	if strings.Join(out.Seen, ",") != "t1" || strings.Join(out.Stored, ",") != "t2" {
		t.Errorf("stored %v, seen %v", out.Stored, out.Seen)
	}
	if !strings.Contains(e.Logs.String(), "crossings the server keeps itself were ignored") {
		t.Error("the ignoring was not logged")
	}
}

// The clients showing the places hear that one was crossed.
func TestACrossingTellsThePlaces(t *testing.T) {
	was := livehub.Settle
	livehub.Settle = time.Millisecond
	defer func() { livehub.Settle = was }()
	hub := livehub.NewHub()
	notices, stop := hub.Subscribe()
	defer stop()

	e, _, home := drawn(t, apitest.Options{Live: hub})
	post(t, e, `{"source":"tasker","events":[`+fixAt(home, "f1", 5, 0)+`]}`)

	deadline := time.After(time.Second)
	for {
		select {
		case n := <-notices:
			if n.Area == livehub.AreaPlaces {
				return
			}
		case <-deadline:
			t.Fatal("no places notice after a crossing")
		}
	}
}
