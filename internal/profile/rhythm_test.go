package profile_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
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

// arrived, left : The two ends of a visit.
func arrived(place string, at time.Time) event.Event { return happened("place.entered", place, at) }
func left(place string, at time.Time) event.Event    { return happened("place.exited", place, at) }

// An arrival paired with the departure after it is how long they
// stayed, which is the half that lets anything be ready before it is
// asked for.
func TestWhereTheyGoSaysWhenAndForHowLong(t *testing.T) {
	var events []event.Event
	for d := 1; d <= 4; d++ {
		events = append(events, arrived("office", day(d, 9, 30)), left("office", day(d, 18, 0)))
	}

	got := profile.Rhythm(events, india())

	for _, want := range []string{
		"Where they went:", "office", "there on 4 days",
		"arriving usually around 9am", "leaving usually around 6pm", "usually staying 8 hours",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in it:\n%s", want, got)
		}
	}
}

// A departure nobody reported is not a night spent at the office.
func TestAVisitThatNeverEndedIsNotCountedAsALength(t *testing.T) {
	events := []event.Event{
		arrived("office", day(1, 9, 0)), left("office", day(1, 17, 0)),
		arrived("office", day(2, 9, 0)), // the phone went flat
		arrived("office", day(3, 9, 0)), left("office", day(4, 20, 0)),
	}

	got := profile.Rhythm(events, india())

	if strings.Contains(got, "staying") {
		t.Errorf("a stay was reported from one pairing and a two-day gap:\n%s", got)
	}
}

// Passing the door is not going somewhere.
func TestPassingThroughIsNotAStay(t *testing.T) {
	var events []event.Event
	for d := 1; d <= 3; d++ {
		events = append(events, arrived("petrol station", day(d, 8, 0)), left("petrol station", day(d, 8, 1)))
	}

	got := profile.Rhythm(events, india())

	if !strings.Contains(got, "petrol station") {
		t.Errorf("the place was dropped entirely:\n%s", got)
	}
	if strings.Contains(got, "staying") {
		t.Errorf("a minute at the door was reported as a stay:\n%s", got)
	}
}

// What was run for them says what they keep wanting, in a fixed
// vocabulary however each question was phrased.
func TestWhatWasRunForThemIsCounted(t *testing.T) {
	ran := func(at time.Time, names ...string) conversation.Message {
		m := conversation.Message{At: at}
		for _, n := range names {
			m.ToolCalls = append(m.ToolCalls, conversation.ToolCall{Name: n})
		}
		return m
	}
	called := []conversation.Message{
		ran(day(1, 9, 0), "calendar_events", "calendar_events"),
		ran(day(2, 9, 0), "calendar_events", "reminder_set"),
		ran(day(3, 9, 0), "tool_describe"),
	}

	got := profile.Asking(called, india())

	if !strings.Contains(got, "calendar_events: 3 times, on 2 days") {
		t.Errorf("the diary readings were not counted:\n%s", got)
	}
	if !strings.Contains(got, "reminder_set: 1 time, on 1 day") {
		t.Errorf("the reminder was not counted:\n%s", got)
	}
	// Describing a tool is the deferring mechanism costing a round, not
	// something the person wanted.
	if strings.Contains(got, "tool_describe") {
		t.Errorf("the describing mechanism was counted as something they asked for:\n%s", got)
	}
	if !strings.Contains(got, "never by the name of a tool") {
		t.Errorf("nothing stops it writing about tools:\n%s", got)
	}
}

// Nothing run is nothing said.
func TestNothingRunSaysNothing(t *testing.T) {
	if got := profile.Asking(nil, india()); got != "" {
		t.Errorf("an empty month produced %q", got)
	}
}

// TestThingsThatHappenTogetherSurviveSomethingInBetween : The pairing
// that matters is two devices reporting one departure, and before this
// anything landing between them hid it entirely.
func TestThingsThatHappenTogetherSurviveSomethingInBetween(t *testing.T) {
	utc := time.UTC
	var evs []event.Event
	day := time.Date(2026, 9, 10, 8, 0, 0, 0, utc)
	for d := range 6 {
		at := day.AddDate(0, 0, d)
		// Leaving home: the geofence, something unrelated, then the
		// network dropping two minutes later.
		evs = append(evs,
			happened("place.exited", "home", at),
			happened("battery.low", "17", at.Add(30*time.Second)),
			happened("network.left", "Dhanush", at.Add(2*time.Minute)),
			// And arriving at the office forty minutes later.
			happened("place.entered", "office", at.Add(42*time.Minute)),
			happened("network.joined", "Zoho-Guest", at.Add(44*time.Minute)),
		)
	}

	got := profile.Rhythm(evs, utc)
	for _, want := range []string{
		`place.exited "home", then network.left "Dhanush"`,
		`place.entered "office", then network.joined "Zoho-Guest"`,
		`place.exited "home", then place.entered "office"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the pairing %q was not found in:\n%s", want, got)
		}
	}
	// And the commute is given as a length the model can use.
	if !strings.Contains(got, "42 minutes") {
		t.Errorf("the gap between leaving and arriving is missing:\n%s", got)
	}
}

// TestAPairingNeedsMoreThanOnce : Two coincidences are not a routine,
// and one written into a description is acted on like a real one.
func TestAPairingNeedsMoreThanOnce(t *testing.T) {
	utc := time.UTC
	at := time.Date(2026, 9, 10, 8, 0, 0, 0, utc)
	got := profile.Rhythm([]event.Event{
		happened("place.exited", "somewhere", at),
		happened("network.left", "once-only", at.Add(time.Minute)),
	}, utc)
	if strings.Contains(got, `place.exited "somewhere", then network.left "once-only"`) {
		t.Errorf("a single coincidence was reported as a pattern:\n%s", got)
	}
}
