package profile_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/profile"
)

// india : The timezone their days are counted in. A week of evenings
// lands on the wrong date in UTC.
func india() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		panic(err)
	}
	return loc
}

// happened : One reported thing.
func happened(kind, value string, at time.Time) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload, OccurredAt: at}
}

// day : A moment, in their own time.
func day(d, h, m int) time.Time {
	return time.Date(2026, 10, d, h, m, 0, 0, india())
}

// A run of the same thing in quick succession is one occurrence of it.
// A laptop that rejoined the same network twenty-five times in a day
// otherwise reads as the most significant thing in the week.
func TestARunOfTheSameThingIsCountedOnce(t *testing.T) {
	var events []event.Event
	for i := 0; i < 25; i++ {
		events = append(events, happened("network.joined", "Dhanush", day(1, 9, i)))
	}

	got := profile.Rhythm(events, india())

	if !strings.Contains(got, "1 time") {
		t.Errorf("twenty-five joins in an hour were not collapsed:\n%s", got)
	}
}

// What they do most comes first, because that is what a description
// should be built around.
func TestWhatTheyDoMostComesFirst(t *testing.T) {
	events := []event.Event{
		happened("place.entered", "office", day(1, 10, 0)),
		happened("place.entered", "office", day(2, 10, 5)),
		happened("place.entered", "office", day(3, 9, 50)),
		happened("place.entered", "gym", day(3, 19, 0)),
	}

	got := profile.Rhythm(events, india())

	office := strings.Index(got, `place.entered "office"`)
	gym := strings.Index(got, `place.entered "gym"`)
	if office < 0 || gym < 0 {
		t.Fatalf("a place is missing:\n%s", got)
	}
	if office > gym {
		t.Errorf("the thing done three times is listed below the thing done once:\n%s", got)
	}
	for _, want := range []string{"3 times", "on 3 of 3 days", "usually around 10am"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in it:\n%s", want, got)
		}
	}
}

// An hour is only given when there is one. "Usually around 2pm" for
// something that happens at any hour is the kind of detail that gets
// written into a description and then believed.
func TestAnHourIsNotInventedForSomethingSpreadAcrossTheDay(t *testing.T) {
	events := []event.Event{
		happened("screen.on", "", day(1, 7, 0)),
		happened("screen.on", "", day(2, 14, 0)),
		happened("screen.on", "", day(3, 23, 0)),
	}

	got := profile.Rhythm(events, india())

	if strings.Contains(got, "usually around") {
		t.Errorf("an hour was invented for something spread across the day:\n%s", got)
	}
	if !strings.Contains(got, "at no particular hour") {
		t.Errorf("the spread was not reported:\n%s", got)
	}
}

// What follows what is the sequence the person means by "I always do
// this then that".
func TestWhatFollowsWhatIsReported(t *testing.T) {
	var events []event.Event
	for d := 1; d <= 3; d++ {
		events = append(events,
			happened("place.exited", "home", day(d, 8, 30)),
			happened("place.entered", "office", day(d, 9, 10)))
	}

	got := profile.Rhythm(events, india())

	if !strings.Contains(got, `place.exited "home", then place.entered "office"`) {
		t.Errorf("the sequence was not found:\n%s", got)
	}
	if !strings.Contains(got, "40 minutes") {
		t.Errorf("the usual gap was not reported:\n%s", got)
	}
}

// Two things that happened next to each other once are not a routine.
func TestACoincidenceIsNotARoutine(t *testing.T) {
	events := []event.Event{
		happened("place.exited", "home", day(1, 8, 30)),
		happened("call.missed", "mum", day(1, 8, 40)),
	}

	got := profile.Rhythm(events, india())

	if strings.Contains(got, "following another") {
		t.Errorf("one coincidence was reported as a sequence:\n%s", got)
	}
}

// Things far apart did not follow one another, whatever the order they
// are stored in.
func TestThingsFarApartAreNotASequence(t *testing.T) {
	var events []event.Event
	for d := 1; d <= 3; d++ {
		events = append(events,
			happened("place.exited", "home", day(d, 8, 0)),
			happened("place.entered", "gym", day(d, 20, 0)))
	}

	got := profile.Rhythm(events, india())

	if strings.Contains(got, "then") {
		t.Errorf("twelve hours apart was called a sequence:\n%s", got)
	}
}

// Nothing reported is nothing said, rather than a heading with nothing
// under it.
func TestNothingReportedSaysNothing(t *testing.T) {
	if got := profile.Rhythm(nil, india()); got != "" {
		t.Errorf("an empty week produced %q", got)
	}
}
