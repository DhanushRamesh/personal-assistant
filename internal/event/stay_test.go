package event_test

import (
	"encoding/json"
	"strings"
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

// named : A stay called something, running between two times.
func named(called string, from, to time.Time, lat, lon float64) event.Named {
	return event.Named{
		Stay:   event.Stay{Lat: lat, Lon: lon, From: from, To: to},
		Called: called,
	}
}

// TestMergedJoinsOnePlaceBrokenByDistance : Crossing somewhere larger
// than Near breaks the cluster; the name puts it back together.
func TestMergedJoinsOnePlaceBrokenByDistance(t *testing.T) {
	start := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	in := []event.Named{
		named("Phoenix Marketcity", start, start.Add(40*time.Minute), 12.9910, 80.2180),
		// The far end of the same building, five minutes later.
		named("Phoenix Marketcity", start.Add(45*time.Minute), start.Add(80*time.Minute), 12.9940, 80.2210),
	}
	got := event.Merged(in)
	if len(got) != 1 {
		t.Fatalf("expected one visit, got %d", len(got))
	}
	if !got[0].From.Equal(in[0].From) || !got[0].To.Equal(in[1].To) {
		t.Errorf("the merged visit should span both: %v to %v", got[0].From, got[0].To)
	}
	if got[0].Long() != 80*time.Minute {
		t.Errorf("expected eighty minutes, got %v", got[0].Long())
	}
	// Weighted towards the longer half, and inside the pair either way.
	if got[0].Lat <= 12.9910 || got[0].Lat >= 12.9940 {
		t.Errorf("the middle should sit between the two: %v", got[0].Lat)
	}
}

// TestMergedKeepsTwoVisitsApart : Leaving and coming back is two visits,
// however alike the names are.
func TestMergedKeepsTwoVisitsApart(t *testing.T) {
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	in := []event.Named{
		named("the office", start, start.Add(3*time.Hour), 12.99, 80.21),
		named("the office", start.Add(8*time.Hour), start.Add(11*time.Hour), 12.99, 80.21),
	}
	if got := event.Merged(in); len(got) != 2 {
		t.Fatalf("a five hour gap is two visits, got %d", len(got))
	}
}

// TestMergedLeavesDifferentPlacesAlone : Including a place revisited
// with something else in between.
func TestMergedLeavesDifferentPlacesAlone(t *testing.T) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	in := []event.Named{
		named("the mall", start, start.Add(30*time.Minute), 12.99, 80.21),
		named("the cafe next door", start.Add(32*time.Minute), start.Add(60*time.Minute), 12.991, 80.211),
		named("the mall", start.Add(62*time.Minute), start.Add(90*time.Minute), 12.99, 80.21),
	}
	if got := event.Merged(in); len(got) != 3 {
		t.Fatalf("three different stays, got %d", len(got))
	}
}

// TestMergedIsTransitive : A building broken into three is still one.
func TestMergedIsTransitive(t *testing.T) {
	start := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
	var in []event.Named
	for i := range 3 {
		at := start.Add(time.Duration(i) * 30 * time.Minute)
		in = append(in, named("the airport", at, at.Add(25*time.Minute), 12.99, 80.21))
	}
	got := event.Merged(in)
	if len(got) != 1 {
		t.Fatalf("expected one, got %d", len(got))
	}
	if got[0].Long() != 85*time.Minute {
		t.Errorf("expected the whole span, got %v", got[0].Long())
	}
}

// TestMergedHandlesNothing : Nought and one stay are returned as they are.
func TestMergedHandlesNothing(t *testing.T) {
	if got := event.Merged(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
	one := []event.Named{named("home", time.Now(), time.Now(), 1, 2)}
	if got := event.Merged(one); len(got) != 1 {
		t.Errorf("expected the one, got %d", len(got))
	}
}

// fix : A reading at a place and a time.
func fix(lat, lon float64, at time.Time) event.Fix {
	return event.Fix{Lat: lat, Lon: lon, At: at}
}

// TestSoFarSeesTheOneStillGoingOn : The most describable thing about
// today is usually the part of it that has not finished.
func TestSoFarSeesTheOneStillGoingOn(t *testing.T) {
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	var fixes []event.Fix
	for i := range 40 {
		fixes = append(fixes, fix(12.9110, 80.0624, start.Add(time.Duration(i)*5*time.Minute)))
	}

	if closed := event.Stays(fixes); len(closed) != 0 {
		t.Fatalf("nothing has ended, got %d stays", len(closed))
	}
	got := event.SoFar(fixes)
	if len(got) != 1 {
		t.Fatalf("expected the open one, got %d", len(got))
	}
	if !got[0].Open {
		t.Error("the open stay is not marked open")
	}
	if got[0].Long() < 3*time.Hour {
		t.Errorf("expected the whole run so far, got %v", got[0].Long())
	}
	if !strings.Contains(string(got[0].Payload("the desk")), `"still":true`) {
		t.Errorf("the payload does not say it is still going: %s", got[0].Payload("the desk"))
	}
}

// TestSoFarKeepsTheClosedOnesToo : And in order, with only the last
// one open.
func TestSoFarKeepsTheClosedOnesToo(t *testing.T) {
	start := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var fixes []event.Fix
	for i := range 12 { // two hours at one place
		fixes = append(fixes, fix(12.9110, 80.0624, start.Add(time.Duration(i)*10*time.Minute)))
	}
	later := start.Add(3 * time.Hour)
	for i := range 12 { // then two hours somewhere else
		fixes = append(fixes, fix(12.9910, 80.2180, later.Add(time.Duration(i)*10*time.Minute)))
	}

	got := event.SoFar(fixes)
	if len(got) != 2 {
		t.Fatalf("expected two, got %d", len(got))
	}
	if got[0].Open {
		t.Error("the first one has ended and should not be open")
	}
	if !got[1].Open {
		t.Error("the last one should be open")
	}
}

// TestAnOpenStayTooShortIsNotOne : Or every arrival anywhere starts a
// stay that is mostly thrown away again.
func TestAnOpenStayTooShortIsNotOne(t *testing.T) {
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	got := event.SoFar([]event.Fix{
		fix(12.9110, 80.0624, start),
		fix(12.9110, 80.0624, start.Add(2*time.Minute)),
	})
	if len(got) != 0 {
		t.Errorf("two minutes is not a stay, got %d", len(got))
	}
}

// TestTheOpenStayKeepsItsKeyAsItGrows : Which is what lets it be
// amended in place rather than written again every five minutes.
func TestTheOpenStayKeepsItsKeyAsItGrows(t *testing.T) {
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	var fixes []event.Fix
	for i := range 4 {
		fixes = append(fixes, fix(12.9110, 80.0624, start.Add(time.Duration(i)*5*time.Minute)))
	}
	first := event.SoFar(fixes)[0]

	for i := 4; i < 10; i++ {
		fixes = append(fixes, fix(12.9110, 80.0624, start.Add(time.Duration(i)*5*time.Minute)))
	}
	later := event.SoFar(fixes)[0]

	if first.Key() != later.Key() {
		t.Errorf("the key changed as it grew: %s then %s", first.Key(), later.Key())
	}
	if later.Long() <= first.Long() {
		t.Errorf("it did not grow: %v then %v", first.Long(), later.Long())
	}
}
