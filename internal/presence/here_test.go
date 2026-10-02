package presence_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/presence"
)

func moment(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, time.UTC) }

// at : A reading from the phone.
func at(lat, lon float64, when time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": event.Coordinates(lat, lon)})
	return event.Event{Kind: event.Fixed, Source: presence.Phone, Payload: payload, OccurredAt: when}
}

// standing : Where the assistant last knew itself to be.
func standing(lat, lon float64, when time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": event.Coordinates(lat, lon)})
	return event.Event{Kind: event.Standing, Source: "server", Payload: payload, OccurredAt: when}
}

// Their desk and their phone, a few metres apart.
func TestThePhoneBesideTheAssistantIsPresent(t *testing.T) {
	got := presence.Near([]event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.91100, 80.06242, moment(11, 58)),
	}, moment(12, 0))

	if got.Where != presence.Present {
		t.Errorf("where = %q (%s), want present", got.Where, got.Why)
	}
}

// Four kilometres away is away, whatever network either is on.
func TestThePhoneMilesAwayIsAway(t *testing.T) {
	got := presence.Near([]event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.94690, 80.06235, moment(11, 58)),
	}, moment(12, 0))

	if got.Where != presence.Away {
		t.Errorf("where = %q (%s), want away", got.Where, got.Why)
	}
}

// A laptop carried to the office is still a laptop being spoken to.
// The moment somebody speaks to it there, it knows where it is, and
// the person standing in front of it is present.
func TestAnAssistantThatMovedIsStillWithThem(t *testing.T) {
	office := []event.Event{
		standing(12.91090, 80.06235, moment(8, 0)),  // yesterday's desk
		standing(12.99165, 80.21757, moment(10, 0)), // spoken to at the office
		at(12.99170, 80.21760, moment(11, 58)),
	}

	if got := presence.Near(office, moment(12, 0)); got.Where != presence.Present {
		t.Errorf("where = %q (%s), want present", got.Where, got.Why)
	}
}

// An assistant nobody has spoken to out loud does not know where it
// is, and that is not an absence.
func TestAnAssistantThatDoesNotKnowWhereItIsSaysSo(t *testing.T) {
	got := presence.Near([]event.Event{at(12.91100, 80.06242, moment(11, 58))}, moment(12, 0))

	if got.Where != presence.Unknown {
		t.Errorf("where = %q, want unknown", got.Where)
	}
}

// A phone that stopped reporting cannot say where anybody is, however
// close its last reading was.
func TestAQuietPhoneCannotSayWhereAnybodyIs(t *testing.T) {
	got := presence.Near([]event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.91100, 80.06242, moment(9, 5)),
	}, moment(12, 0))

	if got.Where != presence.Unknown || !got.Stale {
		t.Errorf("where = %q stale = %v, want unknown and stale", got.Where, got.Stale)
	}
}

// Coming back is present now and away at the reading before.
func TestComingBackIsArriving(t *testing.T) {
	events := []event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.94690, 80.06235, moment(11, 50)), // four km away
		at(12.91100, 80.06242, moment(11, 55)), // at the desk
	}

	if !presence.Arriving(events, moment(11, 56)) {
		t.Error("coming back was not seen as arriving")
	}
}

// Sitting still is not arriving, however many readings there are.
func TestSittingStillIsNotArriving(t *testing.T) {
	events := []event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.91100, 80.06242, moment(11, 50)),
		at(12.91105, 80.06240, moment(11, 55)),
	}

	if presence.Arriving(events, moment(11, 56)) {
		t.Error("sitting still was greeted as an arrival")
	}
}

// One reading ever is nothing to have arrived from.
func TestOneReadingIsNotAnArrival(t *testing.T) {
	events := []event.Event{
		standing(12.91090, 80.06235, moment(9, 0)),
		at(12.91100, 80.06242, moment(11, 55)),
	}

	if presence.Arriving(events, moment(11, 56)) {
		t.Error("a single reading was greeted as an arrival")
	}
}
