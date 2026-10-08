package calendar

import (
	"testing"
	"time"
)

// TestMovingAnAllDayEventKeepsItAllDay : A birthday moved to another day
// is patched as dates, not timestamps.
//
// It was patched as timestamps, which asks Google to turn a whole-day
// event into a timed one; it refuses the request outright. Two attempts
// to move a birthday to 10 September 2027 failed that way on 28
// September 2026, and the failure was reported to the owner as the date
// being in the past -- which it was not, and adding the same date as a
// new event worked a minute later.
func TestMovingAnAllDayEventKeepsItAllDay(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	day := time.Date(2027, 9, 10, 0, 0, 0, 0, loc)
	yes := true

	patch := patchFor(Amend{Starts: &day, Ends: &day, AllDay: &yes}, loc)

	if patch.Start.DateTime != "" || patch.End.DateTime != "" {
		t.Fatalf("sent a timestamp for a whole-day event: %+v %+v", patch.Start, patch.End)
	}
	if patch.Start.Date != "2027-09-10" {
		t.Errorf("start = %q, want 2027-09-10", patch.Start.Date)
	}
	// Exclusive, as Google counts it, and as toGoogle already writes it.
	if patch.End.Date != "2027-09-11" {
		t.Errorf("end = %q, want the day after", patch.End.Date)
	}
}

// TestATimedEventKeepsItsTimes : The other half of the same branch.
func TestATimedEventKeepsItsTimes(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	from := time.Date(2026, 9, 29, 16, 0, 0, 0, loc)
	to := from.Add(time.Hour)
	no := false

	patch := patchFor(Amend{Starts: &from, Ends: &to, AllDay: &no}, loc)

	if patch.Start.Date != "" || patch.End.Date != "" {
		t.Fatalf("sent a date for a timed event: %+v %+v", patch.Start, patch.End)
	}
	if patch.Start.DateTime != from.Format(time.RFC3339) {
		t.Errorf("start = %q", patch.Start.DateTime)
	}
	if patch.Start.TimeZone != loc.String() {
		t.Errorf("no time zone on a timed event: %q", patch.Start.TimeZone)
	}
}

// TestClearingAFieldIsStatedAsNull : An empty string means remove it, and
// only a null field says so to Google; a patch simply leaves out what it
// does not mention.
func TestClearingAFieldIsStatedAsNull(t *testing.T) {
	loc := time.UTC
	blank := ""

	patch := patchFor(Amend{Where: &blank}, loc)

	var said bool
	for _, f := range patch.NullFields {
		if f == "Location" {
			said = true
		}
	}
	if !said {
		t.Errorf("clearing the place was not stated: %v", patch.NullFields)
	}
}
