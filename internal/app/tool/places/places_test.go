package places_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/tool"
	places "github.com/DhanushRamesh/personal-assistant/internal/app/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/base/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
)

// india : Where their days are counted.
func india() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	return loc
}

// now : A fixed afternoon, so a test reads the same every day.
func now() time.Time { return time.Date(2026, 10, 2, 17, 0, 0, 0, india()) }

// clock : Today, in their timezone.
func clock() places.Clock { return places.Clock{Now: now, Location: india()} }

// hhmm : A moment today.
func hhmm(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, india()) }

// reader : Events to hand back.
type reader struct{ events []event.Event }

func (r reader) Recent(context.Context, string, event.Query) ([]event.Event, error) {
	return r.events, nil
}

// crossed : A geofence crossing.
func crossed(kind, place string, at time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": place})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at}
}

// stayed : A stay the server worked out.
func stayed(place string, at time.Time, minutes int) event.Event {
	payload, _ := json.Marshal(map[string]any{
		"value": place, "minutes": minutes, "at": "12.91100,80.06242"})
	return event.Event{Kind: event.Stayed, Payload: payload, OccurredAt: at}
}

// ask : Runs the tool.
func ask(t *testing.T, events []event.Event, args string) tool.Result {
	t.Helper()
	all := places.Tools(reader{events: events}, clock())
	if len(all) != 1 {
		t.Fatalf("%d tools, want 1", len(all))
	}
	return all[0].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "u1", Channel: chat.ChannelVoice},
		Args:   json.RawMessage(args),
	})
}

// The question that started this. Nothing about a network may appear.
func TestItAnswersWhereTheyWentWithoutMentioningNetworks(t *testing.T) {
	noise, _ := json.Marshal(map[string]string{"value": "Dhanush_EXT"})
	got := ask(t, []event.Event{
		crossed(event.Exited, "home", hhmm(10, 22)),
		crossed(event.Entered, "home", hhmm(10, 50)),
		{Kind: "network.joined", Payload: noise, OccurredAt: hhmm(11, 0)},
		{Kind: "network.left", Payload: noise, OccurredAt: hhmm(11, 5)},
	}, `{"days":1}`)

	if strings.Contains(strings.ToLower(got.Content), "network") ||
		strings.Contains(got.Content, "Dhanush") {
		t.Errorf("a wifi name reached the answer:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "home") {
		t.Errorf("home is missing:\n%s", got.Content)
	}
}

// Somebody who has not left is still there, and the answer says so
// rather than inventing a departure.
func TestSomebodyStillSomewhereIsSaidToBeThere(t *testing.T) {
	got := ask(t, []event.Event{crossed(event.Entered, "office", hhmm(9, 40))}, `{"days":1}`)

	if !strings.Contains(got.Content, "still there") {
		t.Errorf("an open visit was not reported as open:\n%s", got.Content)
	}
}

// A stay inside a geofence is the same visit twice. The crossing wins:
// it is the boundary actually being crossed.
func TestAStayInsideAGeofenceIsNotListedTwice(t *testing.T) {
	got := ask(t, []event.Event{
		crossed(event.Entered, "office", hhmm(9, 40)),
		crossed(event.Exited, "office", hhmm(13, 0)),
		stayed("office", hhmm(9, 45), 190),
	}, `{"days":1}`)

	if n := strings.Count(got.Content, "office"); n != 1 {
		t.Errorf("office appears %d times, want 1:\n%s", n, got.Content)
	}
}

// Somewhere they never drew a circle around still shows, because that
// is the whole point of working stays out.
func TestAPlaceWithNoGeofenceStillShows(t *testing.T) {
	got := ask(t, []event.Event{
		crossed(event.Exited, "home", hhmm(11, 0)),
		stayed("Phoenix Marketcity", hhmm(11, 30), 85),
		crossed(event.Entered, "home", hhmm(13, 30)),
	}, `{"days":1}`)

	if !strings.Contains(got.Content, "Phoenix Marketcity") {
		t.Errorf("a stay outside every geofence was dropped:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "1 hour 25 minutes") {
		t.Errorf("how long they were there is missing:\n%s", got.Content)
	}
}

// Asking about one place answers about that place.
func TestAskingAboutOnePlaceNarrowsIt(t *testing.T) {
	got := ask(t, []event.Event{
		crossed(event.Entered, "office", hhmm(9, 40)),
		crossed(event.Exited, "office", hhmm(13, 0)),
		stayed("Phoenix Marketcity", hhmm(14, 0), 60),
	}, `{"days":1,"place":"office"}`)

	if strings.Contains(got.Content, "Phoenix") {
		t.Errorf("it answered about somewhere else too:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "office") {
		t.Errorf("office is missing:\n%s", got.Content)
	}
}

// A day with nothing recorded is not a day they stayed in. The two
// look alike and only one of them is a fact.
func TestNothingRecordedIsNotSaidToBeADayAtHome(t *testing.T) {
	got := ask(t, nil, `{"days":1}`)

	if !strings.Contains(got.Content, "nothing was recorded") {
		t.Errorf("an unwatched day was not reported as unwatched:\n%s", got.Content)
	}
}

// TestGoingOutAndComingBackIsVisible : The bug this exists for.
//
// Home until half past ten and home again from quarter to eleven
// read as having been home the whole time, and the assistant
// answered "no, you stayed home, sir" to somebody who had gone out.
// The evidence was there; the fifteen minutes between the two
// entries existed only if you subtracted them.
func TestGoingOutAndComingBackIsVisible(t *testing.T) {
	got := ask(t, []event.Event{
		stayed("home", hhmm(8, 0), 145),   // 08:00 to 10:25
		stayed("home", hhmm(10, 45), 125), // 10:45 onwards
	}, `{"days":1}`)

	if !strings.Contains(got.Content, "nothing recorded from 10:25 to 10:45") {
		t.Errorf("the stretch they were out is not shown:\n%s", got.Content)
	}
	// And it must not decide what the stretch was.
	for _, guess := range []string{"went out", "left home", "errand"} {
		if strings.Contains(strings.ToLower(got.Content), guess) {
			t.Errorf("the evidence drew the conclusion (%q):\n%s", guess, got.Content)
		}
	}
}

// TestBackToBackStaysAreNotAGap : Two stays that meet have nothing
// between them, and a line saying so on every pair would bury the
// ones that matter.
func TestBackToBackStaysAreNotAGap(t *testing.T) {
	got := ask(t, []event.Event{
		stayed("home", hhmm(8, 0), 120),   // 08:00 to 10:00
		stayed("office", hhmm(10, 0), 60), // 10:00 to 11:00
	}, `{"days":1}`)

	if strings.Contains(got.Content, "nothing recorded") {
		t.Errorf("two stays that meet were reported as a gap:\n%s", got.Content)
	}
}

// stayedAt : A stay a service named, with the address and the
// distance it came with.
func stayedAt(place string, at time.Time, minutes int, address string, metres int) event.Event {
	payload, _ := json.Marshal(map[string]any{
		"value": place, "minutes": minutes, "at": "12.87382,80.07728",
		"address": address, "metres": metres})
	return event.Event{Kind: event.Stayed, Payload: payload, OccurredAt: at}
}

// TestADoubtfulNameIsOfferedAsAGuess : Owner went to a cinema at Sky
// Hub Mall and the stay was recorded as "Flavors non veg
// restaurant" -- the nearest thing any map had, a hundred and
// twenty-seven metres away across a trunk road.
//
// Nothing here can invent the mall. What it can do is stop a guess
// reading like a fact.
func TestADoubtfulNameIsOfferedAsAGuess(t *testing.T) {
	got := ask(t, []event.Event{
		stayedAt("Flavors non veg restaurant", hhmm(18, 5), 170,
			"Grand Southern Trunk Road, Urapakkam, Vandalur - 603210", 127),
	}, `{"days":1}`)

	for _, want := range []string{"Grand Southern Trunk Road", "127m", "may not be where they were"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q in:\n%s", want, got.Content)
		}
	}
}

// TestAPlaceTheyNamedSaysNothingExtra : Home is home. An address and
// a warning on every line of a day spent at home is noise, and they
// drew that fence themselves.
func TestAPlaceTheyNamedSaysNothingExtra(t *testing.T) {
	got := ask(t, []event.Event{stayed("home", hhmm(9, 0), 120)}, `{"days":1}`)
	if strings.Contains(got.Content, "may not be where") || strings.Contains(got.Content, " -- on ") {
		t.Errorf("a place they named was second-guessed:\n%s", got.Content)
	}
}

// TestACloseNameIsStated : Eighteen metres is inside the shop. Saying
// a true thing as a guess is its own kind of wrong.
func TestACloseNameIsStated(t *testing.T) {
	got := ask(t, []event.Event{
		stayedAt("Muthu Supermarket", hhmm(11, 0), 10,
			"Velachery Main Road, Chennai - 600042", 18),
	}, `{"days":1}`)

	if strings.Contains(got.Content, "may not be where they were") {
		t.Errorf("a name from 18m was doubted:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "Velachery Main Road") {
		t.Errorf("the address was dropped:\n%s", got.Content)
	}
}
