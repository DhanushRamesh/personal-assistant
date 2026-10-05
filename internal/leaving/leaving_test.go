package leaving_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/leaving"
)

var film = leaving.Appointment{
	What:  "Meesaya Murukku 2",
	When:  time.Date(2026, 10, 4, 18, 10, 0, 0, time.UTC),
	Where: "Seven Screen's Cinemas, SKY HUB, GST Rd, Kilambakkam",
}

func at(h, m int) time.Time {
	return time.Date(2026, 10, 4, h, m, 0, 0, time.UTC)
}

// TestNothingIsSaidUntilItIsTimeToGo : The whole value is the moment.
// Said an hour early it is a diary entry read back, which they wrote
// themselves and already know.
func TestNothingIsSaidUntilItIsTimeToGo(t *testing.T) {
	takes := 20 * time.Minute // so the moment is 17:40
	for _, when := range []time.Time{at(16, 0), at(17, 0), at(17, 30)} {
		if said, ok := leaving.Leaving(film, takes, when); ok {
			t.Errorf("spoke at %s, which is too early: %q", when.Format("15:04"), said)
		}
	}
	said, ok := leaving.Leaving(film, takes, at(17, 41))
	if !ok {
		t.Fatal("said nothing at the moment itself")
	}
	if !strings.Contains(said, "Meesaya Murukku 2") {
		t.Errorf("did not name it: %q", said)
	}
}

// TestItStopsBeingUsefulOnceItHasStarted : Being told to leave for
// something that began half an hour ago is an accusation with a clock
// on it.
func TestItStopsBeingUsefulOnceItHasStarted(t *testing.T) {
	if _, ok := leaving.Leaving(film, 20*time.Minute, at(18, 5)); !ok {
		t.Error("said nothing five minutes before it starts")
	}
	if said, ok := leaving.Leaving(film, 20*time.Minute, at(18, 40)); ok {
		t.Errorf("spoke half an hour after it began: %q", said)
	}
}

// TestPastTheMomentItSaysWhatIsLeft : Counting backwards from a
// departure already missed reads as a reproach. What is left is the
// useful half.
func TestPastTheMomentItSaysWhatIsLeft(t *testing.T) {
	said, ok := leaving.Leaving(film, 20*time.Minute, at(17, 55))
	if !ok {
		t.Fatal("said nothing")
	}
	if strings.Contains(said, "Time to set off") {
		t.Errorf("told them to set off after the moment had gone: %q", said)
	}
	if !strings.Contains(said, "15 minutes from now") {
		t.Errorf("did not say what is left: %q", said)
	}
}

// TestADurationIsNotClaimedToTheMinute : A journey is not known that
// well, and saying twenty-three minutes claims it is.
func TestADurationIsNotClaimedToTheMinute(t *testing.T) {
	said, _ := leaving.Leaving(film, 23*time.Minute, at(17, 38))
	if strings.Contains(said, "23") {
		t.Errorf("claimed a journey to the minute: %q", said)
	}
	if !strings.Contains(said, "20 minutes") {
		t.Errorf("did not round to something sayable: %q", said)
	}
}

// TestNothingToWorkOutIsNotFetched : A route costs a call to somebody
// else's service, so the cheap test comes first.
func TestNothingToWorkOutIsNotFetched(t *testing.T) {
	now := at(17, 0)
	if !leaving.Worth(film, now) {
		t.Error("an entry an hour away with an address was not worth a route")
	}
	nowhere := film
	nowhere.Where = ""
	if leaving.Worth(nowhere, now) {
		t.Error("an entry with nowhere to go was worth a route")
	}
	far := film
	far.When = now.Add(5 * time.Hour)
	if leaving.Worth(far, now) {
		t.Error("an entry five hours off was worth a route")
	}
	gone := film
	gone.When = now.Add(-time.Hour)
	if leaving.Worth(gone, now) {
		t.Error("an entry an hour past was worth a route")
	}
}

// TestNoJourneyIsNoAnnouncement : Where the route could not be worked
// out there is no moment to name, and a guess at one is worse than
// silence.
func TestNoJourneyIsNoAnnouncement(t *testing.T) {
	if said, ok := leaving.Leaving(film, 0, at(17, 50)); ok {
		t.Errorf("spoke without knowing how far it is: %q", said)
	}
}
