package places_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/api/apitest"
	placesapi "personal-assistant/internal/app/api/places"
	"personal-assistant/internal/base/world/event"
	eventmemory "personal-assistant/internal/base/world/event/inmemory"
	"personal-assistant/internal/domain/places"
	placesmemory "personal-assistant/internal/domain/places/inmemory"
)

func list(t *testing.T, e *apitest.Env) []placesapi.Place {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/places", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/places: %d %s", rec.Code, rec.Body)
	}
	var out placesapi.ListResponse
	e.Decode(t, rec, &out)
	return out.Places
}

func create(t *testing.T, e *apitest.Env, body string) placesapi.Place {
	t.Helper()
	rec := e.Do(t, http.MethodPost, "/v1/places", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /v1/places: %d %s", rec.Code, rec.Body)
	}
	var out placesapi.Place
	e.Decode(t, rec, &out)
	return out
}

// The wire names are the contract the client's map reads.
func TestAPlaceIsDrawnListedChangedAndDeleted(t *testing.T) {
	e := apitest.New(t)
	if got := list(t, e); len(got) != 0 {
		t.Fatalf("a new person has places: %+v", got)
	}

	p := create(t, e, `{"name":"  Gym ","lat":12.9,"lng":80.1,"radius_m":120}`)
	if p.ID == "" || p.Name != "Gym" || p.Lat != 12.9 || p.Lng != 80.1 || p.RadiusM != 120 ||
		p.Inside || p.Since != nil || p.CreatedAt.IsZero() {
		t.Fatalf("created %+v", p)
	}

	rec := e.Do(t, http.MethodGet, "/v1/places", "")
	var raw map[string][]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, k := range []string{"id", "name", "lat", "lng", "radius_m", "created_at", "updated_at", "inside"} {
		if _, ok := raw["places"][0][k]; !ok {
			t.Errorf("listing has no %q: %s", k, rec.Body)
		}
	}
	if _, ok := raw["places"][0]["since"]; ok {
		t.Errorf("since is present for a place never crossed: %s", rec.Body)
	}

	rec = e.Do(t, http.MethodPatch, "/v1/places/"+p.ID, `{"radius_m":300}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: %d %s", rec.Code, rec.Body)
	}
	var changed placesapi.Place
	e.Decode(t, rec, &changed)
	if changed.RadiusM != 300 || changed.Name != "Gym" || changed.Lat != 12.9 {
		t.Errorf("patched %+v: only the radius should change", changed)
	}

	if rec := e.Do(t, http.MethodDelete, "/v1/places/"+p.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if got := list(t, e); len(got) != 0 {
		t.Errorf("still listed after delete: %+v", got)
	}
	if rec := e.Do(t, http.MethodDelete, "/v1/places/"+p.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE twice: %d", rec.Code)
	}
	if rec := e.Do(t, http.MethodPatch, "/v1/places/"+p.ID, `{"radius_m":100}`); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH a deleted one: %d", rec.Code)
	}
}

// Every problem is said at once, in words somebody can act on.
func TestABadPlaceIsRefusedWithEveryReason(t *testing.T) {
	e := apitest.New(t)
	for _, c := range []struct {
		body string
		want []string
	}{
		{`{"name":"","lat":95,"lng":80,"radius_m":10}`,
			[]string{"needs a name", "latitude", "radius"}},
		{`{"name":"` + strings.Repeat("x", 61) + `","lat":1,"lng":181,"radius_m":2001}`,
			[]string{"60 characters", "longitude", "radius"}},
		{`{"name":"Gym"}`, []string{"needs a latitude", "needs a longitude", "needs a radius"}},
		{`{"name":"Gym","lat":1,"lng":1,"radius_m":100,"colour":"red"}`, []string{"not valid JSON"}},
	} {
		rec := e.Do(t, http.MethodPost, "/v1/places", c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", c.body, rec.Code)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%s: %s does not say %q", c.body, rec.Body, w)
			}
		}
		if c.want[0] == "needs a latitude" && strings.Contains(rec.Body.String(), "between 20") {
			t.Errorf("a missing radius was also called out of range: %s", rec.Body)
		}
	}

	p := create(t, e, `{"name":"Gym","lat":1,"lng":1,"radius_m":100}`)
	if rec := e.Do(t, http.MethodPatch, "/v1/places/"+p.ID, `{"radius_m":10}`); rec.Code != http.StatusBadRequest {
		t.Errorf("PATCH to 10 m: %d", rec.Code)
	}
}

// "home" and "Home" are one place to everything that reads crossings.
func TestANameIsKeptOnceWhateverItsCase(t *testing.T) {
	e := apitest.New(t)
	create(t, e, `{"name":"Home","lat":1,"lng":1,"radius_m":100}`)
	rec := e.Do(t, http.MethodPost, "/v1/places", `{"name":"HOME","lat":2,"lng":2,"radius_m":100}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second home: %d %s", rec.Code, rec.Body)
	}
	gym := create(t, e, `{"name":"Gym","lat":1,"lng":1,"radius_m":100}`)
	if rec := e.Do(t, http.MethodPatch, "/v1/places/"+gym.ID, `{"name":"home"}`); rec.Code != http.StatusConflict {
		t.Errorf("renamed onto home: %d", rec.Code)
	}
}

func TestSomebodyElsesPlaceIsNotFound(t *testing.T) {
	store := placesmemory.New()
	e := apitest.NewWith(t, apitest.Options{Places: store})
	theirs := places.Place{ID: places.NewID(), UserID: "usr_someone_else", Name: "Theirs",
		Lat: 1, Lng: 1, Radius: 100}
	if err := store.Add(context.Background(), theirs); err != nil {
		t.Fatal(err)
	}
	if rec := e.Do(t, http.MethodPatch, "/v1/places/"+theirs.ID, `{"radius_m":200}`); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH: %d", rec.Code)
	}
	if rec := e.Do(t, http.MethodDelete, "/v1/places/"+theirs.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE: %d", rec.Code)
	}
	if got := list(t, e); len(got) != 0 {
		t.Errorf("listed somebody else's: %+v", got)
	}
}

// Home and Office come from the stays already written, once: deleting
// Home does not bring it back.
func TestHomeAndOfficeAreMadeFromTheStaysOnce(t *testing.T) {
	events := eventmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: events})
	at := time.Now().Add(-time.Hour)
	var stays []*event.Event
	for i, s := range []string{
		`{"value":"home","at":"12.91080,80.06215"}`,
		`{"value":"Home","at":"12.91084,80.06219"}`,
		`{"value":"home","at":"12.91082,80.06217"}`,
	} {
		ev, err := event.New(e.User.ID, "server", "", event.Stayed, at.Add(-time.Duration(i)*time.Hour), at,
			json.RawMessage(s), "stay-"+string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		stays = append(stays, ev)
	}
	if _, _, err := events.Record(context.Background(), e.User.ID, stays); err != nil {
		t.Fatal(err)
	}

	got := list(t, e)
	if len(got) != 1 || got[0].Name != "Home" || got[0].Lat != 12.91082 || got[0].Lng != 80.06217 ||
		got[0].RadiusM != places.SeedRadius {
		t.Fatalf("seeded %+v, want only Home at the median of its stays", got)
	}
	if rec := e.Do(t, http.MethodDelete, "/v1/places/"+got[0].ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d", rec.Code)
	}
	if again := list(t, e); len(again) != 0 {
		t.Errorf("home came back: %+v", again)
	}
}

// Inside and since come from the server's own latest crossing.
func TestAListingSaysWhetherTheyAreInside(t *testing.T) {
	events := eventmemory.New()
	e := apitest.NewWith(t, apitest.Options{DeviceEvents: events})
	gym := create(t, e, `{"name":"Gym","lat":1,"lng":1,"radius_m":100}`)

	at := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
	in, _ := event.New(e.User.ID, places.Source, "pixel", event.Entered, at, at,
		json.RawMessage(`{"value":"gym"}`), "in")
	tasker, _ := event.New(e.User.ID, "tasker", "pixel", event.Exited, at.Add(time.Minute), at,
		json.RawMessage(`{"value":"Gym"}`), "out")
	if _, _, err := events.Record(context.Background(), e.User.ID, []*event.Event{in, tasker}); err != nil {
		t.Fatal(err)
	}
	got := list(t, e)
	if len(got) != 1 || got[0].ID != gym.ID || !got[0].Inside || got[0].Since == nil || !got[0].Since.Equal(at) {
		t.Fatalf("listed %+v, want inside since the server's entry", got)
	}
}
