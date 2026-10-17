package places_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/world/event"
	eventmemory "personal-assistant/internal/base/world/event/inmemory"
	"personal-assistant/internal/domain/places"
	"personal-assistant/internal/domain/places/inmemory"
)

const user = "usr_tester"

var t0 = time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

// home : 150 m round the stays already called home.
var home = places.Place{ID: "plc_home", UserID: user, Name: "Home",
	Lat: 12.91080, Lng: 80.06215, Radius: 150}

// north : A position this many metres due north of a place's centre.
func north(p places.Place, metres float64) (float64, float64) {
	return p.Lat + metres/111195.0, p.Lng
}

// fix : A location.fix this many minutes after t0, this far north of p.
func fix(t *testing.T, p places.Place, minutes int, metres float64) *event.Event {
	t.Helper()
	lat, lng := north(p, metres)
	body, _ := json.Marshal(map[string]string{"value": fmt.Sprintf("%.6f,%.6f", lat, lng)})
	at := t0.Add(time.Duration(minutes) * time.Minute)
	e, err := event.New(user, "tasker", "pixel", event.Fixed, at, at, body,
		fmt.Sprintf("fix-%d-%v", minutes, metres))
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	return e
}

// kinds : What crossings were written, as "kind name" lines.
func kinds(es []*event.Event) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Kind+" "+event.Value(e.Payload))
	}
	return out
}

func TestTheDistanceIsAlongTheGround(t *testing.T) {
	// A degree of latitude is about 111.2 km anywhere.
	if d := places.Metres(12, 80, 13, 80); math.Abs(d-111195) > 50 {
		t.Errorf("a degree north = %.0f m", d)
	}
	// Home to the office, measured on a map at about 9.9 km.
	if d := places.Metres(12.91080, 80.06215, 12.82991, 80.05143); d < 9000 || d > 9300 {
		t.Errorf("home to office = %.0f m", d)
	}
}

// Every problem with a place is reported at once.
func TestEverythingWrongIsSaidTogether(t *testing.T) {
	_, err := places.Clean(places.Place{Name: "  ", Lat: 91, Lng: -181, Radius: 10})
	problems, ok := err.(places.Problems)
	if !ok || len(problems) != 4 {
		t.Fatalf("err = %v, want four problems", err)
	}
	long := strings.Repeat("é", places.MaxName+1)
	if _, err := places.Clean(places.Place{Name: long, Radius: 100}); err == nil {
		t.Error("a name of 61 characters was accepted")
	}
	p, err := places.Clean(places.Place{Name: "  Gym ", Lat: 1, Lng: 2, Radius: places.MaxRadius})
	if err != nil || p.Name != "Gym" {
		t.Errorf("Clean = %+v, %v", p, err)
	}
}

func TestAFixInsideEntersIt(t *testing.T) {
	out := places.Crossed(user, []places.Place{home}, nil,
		[]*event.Event{fix(t, home, 0, 500), fix(t, home, 5, 100)}, t0)
	if got := kinds(out); len(got) != 1 || got[0] != "place.entered Home" {
		t.Fatalf("crossings = %v", got)
	}
	e := out[0]
	if e.Source != places.Source || e.Device != "pixel" || !e.OccurredAt.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("crossing = %+v: want source server, the fix's device and its time", e)
	}
}

// The edge is where readings wobble. Within Margin past it nothing
// changes; only past the margin is it left.
func TestAWobbleAtTheEdgeDoesNotLeave(t *testing.T) {
	inside := map[string]places.Standing{home.ID: {Inside: true, Since: t0.Add(-time.Hour)}}
	wobble := []*event.Event{
		fix(t, home, 0, 160), fix(t, home, 5, 140), fix(t, home, 10, 195),
	}
	if out := places.Crossed(user, []places.Place{home}, inside, wobble, t0); len(out) != 0 {
		t.Fatalf("a wobble within the margin crossed: %v", kinds(out))
	}
	gone := append(wobble, fix(t, home, 15, 150+places.Margin+20), fix(t, home, 16, 150+places.Margin+80))
	out := places.Crossed(user, []places.Place{home}, inside, gone, t0)
	if got := kinds(out); len(got) != 1 || got[0] != "place.exited Home" {
		t.Fatalf("crossings = %v, want one exit", got)
	}
	// Timed at the first of the two out past the edge.
	if !out[0].OccurredAt.Equal(t0.Add(15 * time.Minute)) {
		t.Errorf("left at %v, want the first fix out", out[0].OccurredAt)
	}
}

// One fix far out between fixes at home is the phone's cached reading,
// not a step out of the door: it leaves nothing (16 October, twice in
// the night, the same point 235 m east).
func TestALoneStrayFixDoesNotLeave(t *testing.T) {
	inside := map[string]places.Standing{home.ID: {Inside: true, Since: t0.Add(-time.Hour)}}
	night := []*event.Event{fix(t, home, 0, 5), fix(t, home, 4, 235), fix(t, home, 7, 3), fix(t, home, 25, 235), fix(t, home, 41, 4)}
	if out := places.Crossed(user, []places.Place{home}, inside, night, t0); len(out) != 0 {
		t.Fatalf("a lone stray fix crossed: %v", kinds(out))
	}
	// The first of the two may have come in an earlier batch.
	out := places.Crossed(user, []places.Place{home}, inside, []*event.Event{fix(t, home, 10, 400)}, t0, fix(t, home, 8, 300))
	if got := kinds(out); len(got) != 1 || got[0] != "place.exited Home" || !out[0].OccurredAt.Equal(t0.Add(8*time.Minute)) {
		t.Fatalf("crossings = %v at %v, want an exit at the earlier fix", got, out)
	}
}

// Outside but within the margin is not inside, so it does not enter.
func TestTheMarginDoesNotEnter(t *testing.T) {
	if out := places.Crossed(user, []places.Place{home}, nil,
		[]*event.Event{fix(t, home, 0, 180)}, t0); len(out) != 0 {
		t.Fatalf("crossed from within the margin: %v", kinds(out))
	}
}

// Fixes are taken in the order they happened, not the order they came.
func TestFixesAreTakenInOrder(t *testing.T) {
	out := places.Crossed(user, []places.Place{home}, nil, []*event.Event{
		fix(t, home, 20, 0), fix(t, home, 15, 950), fix(t, home, 10, 900), fix(t, home, 0, 0),
	}, t0)
	want := []string{"place.entered Home", "place.exited Home", "place.entered Home"}
	if got := kinds(out); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("crossings = %v, want %v", got, want)
	}
}

// Two overlapping places are entered independently.
func TestTwoPlacesAreIndependent(t *testing.T) {
	gym := places.Place{ID: "plc_gym", UserID: user, Name: "Gym", Lat: home.Lat, Lng: home.Lng + 0.002, Radius: 150}
	both := []places.Place{home, gym}
	out := places.Crossed(user, both, nil, []*event.Event{fix(t, home, 0, 0)}, t0)
	if got := kinds(out); len(got) != 1 || got[0] != "place.entered Home" {
		t.Fatalf("at home's centre: %v", got)
	}
	// Between them: inside both. Home is already entered.
	standing := map[string]places.Standing{home.ID: {Inside: true, Since: t0}}
	mid := fix(t, places.Place{Lat: home.Lat, Lng: home.Lng + 0.001}, 5, 0)
	out = places.Crossed(user, both, standing, []*event.Event{mid}, t0)
	if got := kinds(out); len(got) != 1 || got[0] != "place.entered Gym" {
		t.Fatalf("between them: %v, want only the gym entered", got)
	}
}

// A spooled fix from before the last crossing cannot rewrite it.
func TestAFixOlderThanTheLastCrossingIsPassedOver(t *testing.T) {
	standing := map[string]places.Standing{home.ID: {Inside: true, Since: t0.Add(time.Hour)}}
	out := places.Crossed(user, []places.Place{home}, standing, []*event.Event{fix(t, home, 30, 900)}, t0)
	if len(out) != 0 {
		t.Fatalf("crossed from an older fix: %v", kinds(out))
	}
}

// Cross reads where they stand from the server's own crossings and
// writes what changed; the same fix again writes nothing more.
func TestCrossWritesOnceAndOnlyOnce(t *testing.T) {
	ctx := context.Background()
	store := eventmemory.New()
	inside := fix(t, home, 0, 10)
	if _, _, err := store.Record(ctx, user, []*event.Event{inside}); err != nil {
		t.Fatal(err)
	}
	out, err := places.Cross(ctx, store, user, []places.Place{home}, []*event.Event{inside}, t0)
	if err != nil || len(out) != 1 {
		t.Fatalf("Cross = %v, %v", kinds(out), err)
	}
	// The same reading resent under another key, and a later one
	// still inside: already entered, so nothing.
	again := *inside
	again.DedupeKey = "resent"
	out, err = places.Cross(ctx, store, user, []places.Place{home},
		[]*event.Event{&again, fix(t, home, 5, 20)}, t0)
	if err != nil || len(out) != 0 {
		t.Fatalf("Cross again = %v, %v", kinds(out), err)
	}
	// And the very same crossing worked out again is a duplicate
	// key, not a second event.
	dup := places.Crossed(user, []places.Place{home}, nil, []*event.Event{inside}, t0)
	stored, _, _ := store.Record(ctx, user, dup)
	if len(stored) != 0 {
		t.Fatalf("a crossing for the same fix was stored twice")
	}
	n := 0
	for _, e := range store.All(user) {
		if e.Kind == event.Entered {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d entries stored, want 1", n)
	}
}

// Tasker's crossings for the same name do not count as the server's
// state: only what the server wrote does.
func TestTaskersCrossingsAreNotTheState(t *testing.T) {
	body := json.RawMessage(`{"value":"home"}`)
	tasker, _ := event.New(user, "tasker", "", event.Entered, t0, t0, body, "t")
	server, _ := event.New(user, places.Source, "", event.Exited, t0.Add(-time.Hour), t0, body, "s")
	got := places.Latest([]event.Event{*tasker, *server}, "Home")
	if got.Inside || !got.Since.Equal(t0.Add(-time.Hour)) {
		t.Errorf("standing = %+v, want outside since the server's exit", got)
	}
}

// stay : A place.stayed called name at a position.
func stay(t *testing.T, name string, lat, lng float64, n int) event.Event {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"value": name, "at": fmt.Sprintf("%.5f,%.5f", lat, lng), "minutes": 60})
	at := t0.Add(-time.Duration(n) * time.Hour)
	e, err := event.New(user, "server", "", event.Stayed, at, at, body, fmt.Sprintf("stay-%d", n))
	if err != nil {
		t.Fatal(err)
	}
	return *e
}

// Home and Office come from the median of their stays, once.
func TestHomeAndOfficeAreSeededOnce(t *testing.T) {
	ctx := context.Background()
	events := eventmemory.New()
	var stays []*event.Event
	for _, s := range []event.Event{
		stay(t, "home", 12.91080, 80.06215, 1),
		stay(t, "Home", 12.91082, 80.06217, 2),
		stay(t, "home", 12.95000, 80.10000, 3), // one stray stay
		stay(t, "office", 12.82991, 80.05143, 4),
		stay(t, "Some cafe", 12.9, 80.0, 5),
	} {
		stays = append(stays, &s)
	}
	if _, _, err := events.Record(ctx, user, stays); err != nil {
		t.Fatal(err)
	}
	store := inmemory.New()
	made, err := places.Seed(ctx, store, events, user, t0)
	if err != nil || len(made) != 2 {
		t.Fatalf("Seed = %+v, %v", made, err)
	}
	all, _ := store.List(ctx, user)
	if all[0].Name != "Home" || all[0].Lat != 12.91082 || all[0].Lng != 80.06217 || all[0].Radius != places.SeedRadius {
		t.Errorf("home = %+v, want the median of its stays", all[0])
	}
	if all[1].Name != "Office" || all[1].Lat != 12.82991 {
		t.Errorf("office = %+v", all[1])
	}
	// Deleted, and not brought back.
	_ = store.Remove(ctx, user, all[0].ID)
	if made, _ := places.Seed(ctx, store, events, user, t0); len(made) != 0 {
		t.Errorf("seeded again: %+v", made)
	}
}

// Nothing to seed from still marks it done.
func TestNoStaysSeedsNothingButCounts(t *testing.T) {
	ctx := context.Background()
	store := inmemory.New()
	made, err := places.Seed(ctx, store, eventmemory.New(), user, t0)
	if err != nil || len(made) != 0 {
		t.Fatalf("Seed = %+v, %v", made, err)
	}
	if done, _ := store.Seeded(ctx, user); !done {
		t.Error("not marked seeded")
	}
}

// stayedAt : A place.stayed the server wrote, this far north of p, called name.
func stayedAt(t *testing.T, p places.Place, metres float64, name, key string) *event.Event {
	t.Helper()
	lat, lng := north(p, metres)
	body, _ := json.Marshal(map[string]any{"at": fmt.Sprintf("%.5f,%.5f", lat, lng), "value": name, "minutes": 35})
	e, err := event.New(user, "server", "", event.Stayed, t0, t0, body, key)
	if err != nil {
		t.Fatalf("event.New: %v", err)
	}
	return e
}

func TestKeepingAPlaceNamesTheStaysAlreadyInIt(t *testing.T) {
	ctx := context.Background()
	events := eventmemory.New()
	gym := places.Place{ID: "plc_gym", UserID: user, Name: "Gym & Pool", Lat: 12.88214, Lng: 80.08377, Radius: 150}
	if _, _, err := events.Record(ctx, user, []*event.Event{
		stayedAt(t, gym, 40, "Namma Veedu Vasanta Bhavan", "stay-in"),
		stayedAt(t, gym, 900, "Somewhere else", "stay-out"),
		stayedAt(t, gym, 10, "Gym & Pool", "stay-named"),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	n, err := places.Rename(ctx, events, user, gym)
	if err != nil || n != 1 {
		t.Fatalf("Rename = %d, %v; want 1 stay renamed", n, err)
	}
	stays, _ := events.Recent(ctx, user, event.Query{Kind: event.Stayed})
	names := map[string]string{}
	for _, e := range stays {
		names[e.DedupeKey] = event.Value(e.Payload)
	}
	want := map[string]string{"stay-in": "Gym & Pool", "stay-out": "Somewhere else", "stay-named": "Gym & Pool"}
	for key, name := range want {
		if names[key] != name {
			t.Errorf("stay %s is called %q; want %q", key, names[key], name)
		}
	}
	// Again, nothing is left to rename.
	if n, _ := places.Rename(ctx, events, user, gym); n != 0 {
		t.Errorf("a second Rename changed %d; want 0", n)
	}
}
