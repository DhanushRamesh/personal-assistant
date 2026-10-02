package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// at : A reading, so many minutes into the day.
func at(minute int, lat, lon float64) event.Fix {
	return event.Fix{
		At:  time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute),
		Lat: lat, Lon: lon,
	}
}

// Sitting in one place long enough is somewhere they went.
func TestSittingStillLongEnoughIsAStay(t *testing.T) {
	// Twenty minutes in one spot, with the scatter a phone really
	// produces, then away.
	fixes := []event.Fix{
		at(0, 12.91080, 80.06227),
		at(5, 12.91100, 80.06242),
		at(10, 12.91090, 80.06235),
		at(15, 12.91100, 80.06243),
		at(20, 12.91085, 80.06230),
		at(25, 12.95000, 80.10000), // somewhere else entirely
	}

	stays := event.Stays(fixes)

	if len(stays) != 1 {
		t.Fatalf("found %d stays, want 1: %+v", len(stays), stays)
	}
	if got := stays[0].Long(); got != 20*time.Minute {
		t.Errorf("stayed %v, want 20m: the end is the last reading inside it, "+
			"not the one that ended it", got)
	}
}

// Passing through is not going somewhere. The owner's number: more
// than ten minutes.
func TestPassingThroughIsNotAStay(t *testing.T) {
	fixes := []event.Fix{
		at(0, 12.91080, 80.06227),
		at(5, 12.91090, 80.06235),
		at(10, 12.95000, 80.10000),
	}

	if stays := event.Stays(fixes); len(stays) != 0 {
		t.Errorf("five minutes somewhere was recorded as a stay: %+v", stays)
	}
}

// Somebody still sitting there has not stayed a known length of time
// yet. Writing it down now records a ten-minute visit to a restaurant
// they are still in.
func TestAStayStillHappeningIsNotWrittenDown(t *testing.T) {
	fixes := []event.Fix{
		at(0, 12.91080, 80.06227),
		at(5, 12.91100, 80.06242),
		at(10, 12.91090, 80.06235),
		at(15, 12.91100, 80.06243),
	}

	if stays := event.Stays(fixes); len(stays) != 0 {
		t.Errorf("a stay that has not ended was written down: %+v", stays)
	}
}

// A phone that stopped reporting did not mean somebody slept at the
// office. The stay ends at its last reading.
func TestSilenceEndsAStayAtItsLastReading(t *testing.T) {
	fixes := []event.Fix{
		at(0, 12.91080, 80.06227),
		at(10, 12.91100, 80.06242),
		at(15, 12.91090, 80.06235),
		at(600, 12.91095, 80.06240), // ten hours later, same place
	}

	stays := event.Stays(fixes)

	if len(stays) != 1 {
		t.Fatalf("found %d stays, want 1: %+v", len(stays), stays)
	}
	if got := stays[0].Long(); got != 15*time.Minute {
		t.Errorf("stayed %v, want 15m: the hours of silence are not time spent there", got)
	}
}

// A slow walk is not one place, however gradually it moves.
func TestAWalkIsNotOnePlace(t *testing.T) {
	var fixes []event.Fix
	// Thirty metres every five minutes for two hours: never more than
	// Near from the last reading, far more than Near from the first.
	for i := 0; i <= 24; i++ {
		fixes = append(fixes, at(i*5, 12.91080+float64(i)*0.00027, 80.06227))
	}

	for _, s := range event.Stays(fixes) {
		if s.Long() > 30*time.Minute {
			t.Errorf("a two-hour walk produced a %v stay at one place", s.Long())
		}
	}
}

// The same place twice is the same place, though the coordinates never
// repeat. Without this nothing counting where somebody goes ever counts
// to two.
func TestASecondStayBorrowsTheFirstsName(t *testing.T) {
	first := event.Stay{Lat: 12.91090, Lon: 80.06235}
	payload, err := json.Marshal(map[string]any{"value": "the usual café", "at": first.Where()})
	if err != nil {
		t.Fatal(err)
	}
	known := []event.Event{{Kind: event.Stayed, Payload: payload}}

	// Forty metres away, which is scatter rather than somewhere else.
	second := event.Stay{Lat: 12.91125, Lon: 80.06255}

	if got := event.Called(second, known); got != "the usual café" {
		t.Errorf("called it %q, want the name the first stay gave it", got)
	}
}

// Somewhere genuinely different is not given the other place's name.
func TestAnotherPlaceKeepsItsOwnName(t *testing.T) {
	here := event.Stay{Lat: 12.91090, Lon: 80.06235}
	payload, _ := json.Marshal(map[string]any{"value": "home", "at": here.Where()})
	known := []event.Event{{Kind: event.Stayed, Payload: payload}}

	// Four kilometres away.
	elsewhere := event.Stay{Lat: 12.94690, Lon: 80.06235}

	if got := event.Called(elsewhere, known); got == "home" {
		t.Errorf("somewhere four kilometres away was called home")
	}
}

// The same readings produce the same key, so settling twice writes one
// row.
func TestAStayKeyDoesNotMove(t *testing.T) {
	fixes := []event.Fix{
		at(0, 12.91080, 80.06227), at(5, 12.91100, 80.06242),
		at(15, 12.91090, 80.06235), at(25, 12.95000, 80.10000),
	}

	once, twice := event.Stays(fixes), event.Stays(fixes)
	if len(once) != 1 || once[0].Key() != twice[0].Key() {
		t.Errorf("keys %v and %v", once, twice)
	}
}

// A reading that will not parse is one of several hundred, and a stay
// is none the worse for missing it.
func TestAReadingThatWillNotParseIsSkipped(t *testing.T) {
	for _, bad := range []string{`{"value":"%gl_coordinates"}`, `{"value":""}`, `{}`, `{"value":"1"}`} {
		if _, ok := event.ReadFix(event.Event{Kind: event.Fixed, Payload: json.RawMessage(bad)}); ok {
			t.Errorf("%s was read as a position", bad)
		}
	}
	good := `{"value":"12.9108472,80.0622726"}`
	if _, ok := event.ReadFix(event.Event{Kind: event.Fixed, Payload: json.RawMessage(good)}); !ok {
		t.Errorf("%s was not read as a position", good)
	}
}

// crossed : A geofence crossing.
func crossed(kind, place string, minute int) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": place})
	return event.Event{
		Kind:       kind,
		Payload:    payload,
		OccurredAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute),
	}
}

// when : A moment, so many minutes into the same day.
func when(minute int) time.Time {
	return time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC).Add(time.Duration(minute) * time.Minute)
}

// A place the person drew themselves names the stay. Their rule:
// geofences first.
func TestAStayInsideAGeofenceTakesItsName(t *testing.T) {
	fences := event.Fences([]event.Event{
		crossed(event.Entered, "office", 0),
		crossed(event.Exited, "office", 480),
	}, when(600))

	s := event.Stay{From: when(60), To: when(200), Lat: 12.9109, Lon: 80.0623}

	if got := event.In(s, fences); got != "office" {
		t.Errorf("called it %q, want office", got)
	}
}

// Somebody still at the office has not stopped being there because
// they have not left yet.
func TestACrossingThatWasNeverClosedRunsToNow(t *testing.T) {
	fences := event.Fences([]event.Event{crossed(event.Entered, "office", 0)}, when(600))

	s := event.Stay{From: when(300), To: when(400)}

	if got := event.In(s, fences); got != "office" {
		t.Errorf("called it %q, want office: an open crossing should still cover it", got)
	}
}

// A crossing out of somewhere nothing says they entered is ignored,
// or it swallows every stay before it.
func TestALeavingWithNoArrivalIsIgnored(t *testing.T) {
	fences := event.Fences([]event.Event{crossed(event.Exited, "office", 300)}, when(600))

	if len(fences) != 0 {
		t.Errorf("an unmatched departure produced %+v", fences)
	}
}

// A place drawn inside another is the more particular answer.
func TestTheSmallerPlaceWins(t *testing.T) {
	fences := event.Fences([]event.Event{
		crossed(event.Entered, "home", 0),
		crossed(event.Entered, "badminton court", 100),
		crossed(event.Exited, "badminton court", 200),
		crossed(event.Exited, "home", 480),
	}, when(600))

	s := event.Stay{From: when(120), To: when(180)}

	if got := event.In(s, fences); got != "badminton court" {
		t.Errorf("called it %q, want badminton court", got)
	}
}

// Somewhere they never drew a circle around has no name from this, and
// falls through to whatever else can name it.
func TestAStayOutsideEveryGeofenceIsUnnamed(t *testing.T) {
	fences := event.Fences([]event.Event{
		crossed(event.Entered, "office", 0),
		crossed(event.Exited, "office", 100),
	}, when(600))

	s := event.Stay{From: when(300), To: when(360)}

	if got := event.In(s, fences); got != "" {
		t.Errorf("a stay nowhere near a geofence was called %q", got)
	}
}

// Arriving somewhere else ends wherever they were. Android drops
// geofence departures, and without this a missed one leaves somebody
// at the office from last night until the end of time.
func TestArrivingSomewhereElseEndsWhereTheyWere(t *testing.T) {
	fences := event.Fences([]event.Event{
		crossed(event.Entered, "office", 0),
		// no departure from the office was ever reported
		crossed(event.Entered, "home", 120),
	}, when(600))

	if len(fences) != 2 {
		t.Fatalf("found %d stretches, want 2: %+v", len(fences), fences)
	}
	if fences[0].Name != "office" || !fences[0].To.Equal(when(120)) {
		t.Errorf("the office did not end when they got home: %+v", fences[0])
	}
	if fences[1].Name != "home" || !fences[1].To.Equal(when(600)) {
		t.Errorf("home should still be open: %+v", fences[1])
	}
}

// Somewhere entered, left, and entered again is two stretches, not one
// listed twice. The second arrival reported itself as still open once
// per time the name had ever been opened.
func TestLeavingAndComingBackIsTwoStretchesNotADuplicate(t *testing.T) {
	fences := event.Fences([]event.Event{
		crossed(event.Entered, "home", 0),
		crossed(event.Exited, "home", 100),
		crossed(event.Entered, "home", 200),
	}, when(600))

	if len(fences) != 2 {
		t.Fatalf("found %d stretches, want 2: %+v", len(fences), fences)
	}
	open := 0
	for _, f := range fences {
		if f.To.Equal(when(600)) {
			open++
		}
	}
	if open != 1 {
		t.Errorf("%d stretches are still open, want 1: %+v", open, fences)
	}
}
