package maps_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/tool"
	mapstool "github.com/DhanushRamesh/personal-assistant/internal/app/tool/maps"
	"github.com/DhanushRamesh/personal-assistant/internal/base/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
	"github.com/DhanushRamesh/personal-assistant/internal/base/place"
)

var now = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

// atlas : A map that answers however a test tells it to.
type atlas struct {
	found   []place.Spot
	near    []place.Spot
	went    place.Journey
	err     error
	nearErr error
	mode    string
	asked   []string
}

func (a *atlas) Find(_ context.Context, text, _ string, _ int) ([]place.Spot, error) {
	a.asked = append(a.asked, text)
	return a.found, a.err
}
func (a *atlas) Near(_ context.Context, _, _ float64, what string, _ float64, _ int) ([]place.Spot, error) {
	a.asked = append(a.asked, what)
	return a.near, a.nearErr
}
func (a *atlas) Route(_ context.Context, _, _, _, _ float64, mode string) (place.Journey, error) {
	a.mode = mode
	return a.went, a.err
}

// phone : Where the phone last said they were.
type phone struct {
	at  time.Time
	off bool
}

func (p phone) Recent(_ context.Context, _ string, q event.Query) ([]event.Event, error) {
	if p.off || p.at.Before(q.Since) {
		return nil, nil
	}
	return []event.Event{{Kind: event.Fixed, OccurredAt: p.at,
		Payload: json.RawMessage(`{"value":"12.91100,80.06240"}`)}}, nil
}

func ask(t *testing.T, name string, m mapstool.Maps, r mapstool.Reader, args string) tool.Result {
	t.Helper()
	for _, x := range mapstool.Tools(m, r, mapstool.Clock{Now: func() time.Time { return now }}) {
		if x.Name == name {
			return x.Run(context.Background(), tool.Invocation{
				Caller: tool.Caller{UserID: "u1"}, Args: json.RawMessage(args)})
		}
	}
	t.Fatalf("%s is not registered", name)
	return tool.Result{}
}

func fresh() phone { return phone{at: now.Add(-2 * time.Minute)} }

// TestHowLongIsFromWhereTheyAre : The whole worth of this. A
// distance between two cities is a thing anybody knows; how long it
// takes now, from where they are standing, is what nothing else can
// say.
func TestHowLongIsFromWhereTheyAre(t *testing.T) {
	a := &atlas{
		found: []place.Spot{{Name: "Chennai International Airport",
			Address: "GST Road, Chennai", Lat: 12.98888, Lon: 80.17497}},
		went: place.Journey{Metres: 19700, Takes: 55 * time.Minute, Mode: place.Driving},
	}
	got := ask(t, "place_distance", a, fresh(), `{"to":"Chennai airport"}`)

	for _, want := range []string{"19.7 km", "55 minutes", "Chennai International Airport"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q in: %s", want, got.Content)
		}
	}
	if !strings.Contains(got.Content, "allows for today's traffic") {
		t.Error("nothing says the time is a real one rather than a best case")
	}
}

// TestADrivingTimeAllowsForTraffic : Measured across Chennai,
// nineteen minutes without traffic against fifty-three with. A
// travel time three times short is not a worse answer than none --
// it is acted on, and the thing is missed.
func TestADrivingTimeAllowsForTraffic(t *testing.T) {
	a := &atlas{found: []place.Spot{{Name: "there", Lat: 1, Lon: 1}},
		went: place.Journey{Metres: 1000, Takes: time.Minute}}

	ask(t, "place_distance", a, fresh(), `{"to":"there"}`)
	if a.mode != place.Driving {
		t.Errorf("mode %q, want driving", a.mode)
	}
	ask(t, "place_distance", a, fresh(), `{"to":"there","walking":true}`)
	if a.mode != place.Walking {
		t.Errorf("mode %q, want walking", a.mode)
	}
}

// TestAStalePositionIsRefused : A journey time from an hour-old
// position is wrong in the one way that matters: it is believed.
func TestAStalePositionIsRefused(t *testing.T) {
	a := &atlas{found: []place.Spot{{Name: "there", Lat: 1, Lon: 1}}}
	old := phone{at: now.Add(-3 * time.Hour)}

	got := ask(t, "place_distance", a, old, `{"to":"there"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it answered from a stale position: %s", got.Content)
	}
	if !strings.Contains(got.Content, "has not reported where it is") {
		t.Errorf("it did not say why: %s", got.Content)
	}
}

// TestNoRoadIsNotAGuess : An island and a wrong coordinate both
// produce no route, and inventing a time for either is worse than
// saying so.
func TestNoRoadIsNotAGuess(t *testing.T) {
	a := &atlas{found: []place.Spot{{Name: "nowhere", Lat: 1, Lon: 1}}}
	got := ask(t, "place_distance", a, fresh(), `{"to":"nowhere"}`)
	if !strings.Contains(got.Content, "No road") {
		t.Errorf("it invented a journey: %s", got.Content)
	}
}

// TestNothingFoundIsNotNothingThere : The map not having something
// is a fact about the map. Sky Hub Mall is the standing proof.
func TestNothingFoundIsNotNothingThere(t *testing.T) {
	got := ask(t, "place_find", &atlas{}, fresh(), `{"place":"Sky Hub Mall"}`)
	if !strings.Contains(got.Content, "That is the map, not the world") {
		t.Errorf("an empty map stood as proof: %s", got.Content)
	}
}

// TestAnUnknownSortIsRefusedLoudly : Geoapify answers an unknown
// category with an empty list and no error, so a typed-through
// "unicorn" would come back as "nothing near you" -- a lie that
// looks like an answer.
func TestAnUnknownSortIsRefusedLoudly(t *testing.T) {
	a := &atlas{nearErr: errors.New(`place: nothing here knows what "unicorn" is`)}
	got := ask(t, "place_near", a, fresh(), `{"sort":"unicorn"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("an unknown sort passed silently: %s", got.Content)
	}
}

// TestNearMeNeedsToKnowWhereTheyAre : It has no other way.
func TestNearMeNeedsToKnowWhereTheyAre(t *testing.T) {
	got := ask(t, "place_near", &atlas{}, phone{off: true}, `{"sort":"food"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it searched around a position it did not have: %s", got.Content)
	}
}

// TestNoMapIsSaidPlainly : Different from a search that found
// nothing.
func TestNoMapIsSaidPlainly(t *testing.T) {
	for _, name := range []string{"place_find", "place_near", "place_distance"} {
		got := ask(t, name, nil, fresh(), `{"place":"x","to":"x"}`)
		if got.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s pretended to have a map: %s", name, got.Content)
		}
	}
}
