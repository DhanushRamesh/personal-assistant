package presence

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

var now = time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)

func ev(source, kind, network string, ago time.Duration) event.Event {
	payload := json.RawMessage(`{}`)
	if network != "" {
		b, _ := json.Marshal(map[string]string{"value": network})
		payload = b
	}
	return event.Event{
		Source: source, Kind: kind,
		OccurredAt: now.Add(-ago), Payload: payload,
	}
}

func TestSameNetworkIsPresent(t *testing.T) {
	got := From([]event.Event{
		ev(Satellite, Joined, "Zoho-Guest", 3*time.Hour),
		ev(Phone, Joined, "Zoho-Guest", time.Hour),
	}, now)

	if got.Where != Present {
		t.Fatalf("where = %s (%s), want present", got.Where, got.Why)
	}
}

// A phone on mobile data lying beside the laptop is on a different
// network and plainly present. So is a phone on the guest network in an
// office whose laptop is on the corporate one -- which is exactly the
// case this was first run in.
func TestADifferentNetworkIsNotAnAbsence(t *testing.T) {
	got := From([]event.Event{
		ev(Satellite, Joined, "Zoho-Corp-TLS", time.Hour),
		ev(Phone, Joined, "Zoho-Guest", time.Hour),
	}, now)

	if got.Where == Away {
		t.Fatalf("a different network must never mean away: %s", got.Why)
	}
	if got.Where != Unknown {
		t.Fatalf("where = %s (%s), want unknown", got.Where, got.Why)
	}
}

// Nothing may conclude an absence from a network, however the events
// fall. Away waits on location.
func TestNothingConcludesAwayFromNetworksAlone(t *testing.T) {
	cases := [][]event.Event{
		{ev(Satellite, Joined, "Home", time.Hour), ev(Phone, Joined, "Other", time.Hour)},
		{ev(Satellite, Joined, "Home", time.Hour), ev(Phone, Left, "Home", time.Minute)},
		{ev(Satellite, Left, "Home", time.Hour), ev(Phone, Joined, "Home", time.Minute)},
		{ev(Phone, Joined, "Home", time.Hour)},
		{ev(Satellite, Joined, "Home", time.Hour)},
		nil,
	}
	for i, events := range cases {
		if got := From(events, now); got.Where == Away {
			t.Errorf("case %d concluded away from networks: %s", i, got.Why)
		}
	}
}

// Being unable to see is not the same as having seen nobody, and a fault
// that silences the house is worse than a gap in a gate.
func TestNothingHeardIsUnknownNotAway(t *testing.T) {
	for name, events := range map[string][]event.Event{
		"neither":      nil,
		"no phone":     {ev(Satellite, Joined, "Home", time.Hour)},
		"no satellite": {ev(Phone, Joined, "Home", time.Hour)},
	} {
		if got := From(events, now); got.Where != Unknown {
			t.Errorf("%s: where = %s, want unknown", name, got.Where)
		}
	}
}

// A phone off every network is not with the assistant, but it is also
// not evidence of being anywhere -- it says so rather than guessing.
func TestAPhoneOnNoNetworkIsUnknown(t *testing.T) {
	got := From([]event.Event{
		ev(Satellite, Joined, "Home", time.Hour),
		ev(Phone, Left, "Home", time.Minute),
	}, now)

	if got.Where != Unknown {
		t.Fatalf("where = %s (%s), want unknown", got.Where, got.Why)
	}
}

// Somebody who has not moved for four hours produces no events at all.
// An old transition is the current state, not a stale reading.
func TestAnOldJoinIsStillTheCurrentNetwork(t *testing.T) {
	got := From([]event.Event{
		ev(Satellite, Joined, "Home", 9*time.Hour),
		ev(Phone, Joined, "Home", 8*time.Hour),
	}, now)

	if got.Where != Present {
		t.Fatalf("where = %s, want present", got.Where)
	}
	if !got.Stale {
		t.Fatal("a phone silent for eight hours should be reported as stale")
	}
}

func TestTheNewestEventPerSourceWins(t *testing.T) {
	got := From([]event.Event{
		ev(Satellite, Joined, "Home", 5*time.Hour),
		ev(Phone, Joined, "Home", 5*time.Hour),
		ev(Phone, Left, "Home", 2*time.Hour),
		ev(Phone, Joined, "Zoho-Guest", time.Hour),
	}, now)

	if got.Where == Away {
		t.Fatalf("a different network must never mean away: %s", got.Why)
	}
	if got.On[Phone] != "Zoho-Guest" {
		t.Fatalf("phone is on %q, want the newest", got.On[Phone])
	}
}

// Events arrive in whatever order a phone flushed them.
func TestOrderDoesNotMatter(t *testing.T) {
	forwards := From([]event.Event{
		ev(Satellite, Joined, "Home", 5*time.Hour),
		ev(Phone, Left, "Home", 2*time.Hour),
		ev(Phone, Joined, "Home", 5*time.Hour),
	}, now)
	backwards := From([]event.Event{
		ev(Phone, Joined, "Home", 5*time.Hour),
		ev(Phone, Left, "Home", 2*time.Hour),
		ev(Satellite, Joined, "Home", 5*time.Hour),
	}, now)

	if forwards.Where != backwards.Where {
		t.Fatalf("%s then %s: order changed the answer",
			forwards.Where, backwards.Where)
	}
}

// Wifi flapped eight times over lunch one afternoon. Each of those would
// otherwise have been a welcome home.
func TestOnlyAwayThenPresentCountsAsArriving(t *testing.T) {
	present := Answer{Where: Present}
	away := Answer{Where: Away}
	unknown := Answer{Where: Unknown}

	if !Arrived(away, present) {
		t.Fatal("away then present is an arrival")
	}
	for name, pair := range map[string][2]Answer{
		"still here":     {present, present},
		"still out":      {away, away},
		"out of a fault": {unknown, present},
		"into a fault":   {present, unknown},
		"leaving":        {present, away},
	} {
		if Arrived(pair[0], pair[1]) {
			t.Errorf("%s should not count as arriving", name)
		}
	}
}

// A network is whatever somebody called their router.
func TestAwkwardNetworkNamesSurvive(t *testing.T) {
	awkward := `the "good" router\`
	got := From([]event.Event{
		ev(Satellite, Joined, awkward, time.Hour),
		ev(Phone, Joined, awkward, time.Hour),
	}, now)

	if got.Where != Present {
		t.Fatalf("where = %s (%s); On = %v", got.Where, got.Why, got.On)
	}
}
