package routine_test

import (
	"testing"
	"time"

	"personal-assistant/internal/base/world/routine"
)

// TestAFactAnswersToASource : The name is what somebody writes in a
// rule, so it is the one thing here that must not drift.
func TestAFactAnswersToASource(t *testing.T) {
	f := routine.Fact{Subject: "Home", What: routine.Leaves, Hour: 10}
	if got := f.Name(); got != "usually.home.leaves" {
		t.Errorf("a rule would be written against %q", got)
	}
	if !routine.Mine(f.Name()) {
		t.Error("its own name is not recognised as a routine")
	}
	if routine.Mine("battery.level") {
		t.Error("a device reading was taken for a routine")
	}
}

// TestTheHourIsPlacedOnToday : What makes the existing operators
// work on a routine unchanged. An hour on its own cannot be compared
// with anything; today at that hour can.
func TestTheHourIsPlacedOnToday(t *testing.T) {
	where := time.FixedZone("IST", 19800)
	now := time.Date(2026, 10, 7, 23, 15, 0, 0, where)

	at := routine.Fact{Subject: "home", What: routine.Arrives, Hour: 21}.
		Today(now, where)

	if at.Hour() != 21 || at.Day() != 7 || at.Month() != time.October {
		t.Errorf("the hour was placed at %s, want nine on the same evening", at)
	}
	if since := now.Sub(at); since != 2*time.Hour+15*time.Minute {
		t.Errorf("two and a quarter hours late reads as %s", since)
	}
}

// TestTheHourAsSomebodyWouldSayIt : This reaches a person's ears --
// "you usually leave around 10am" -- so every hour of the clock has
// to come out as a thing somebody would actually say.
//
// Midnight and midday are the two that go wrong: as numbers they are
// 0 and 12, and read naively they become "0am" and "12pm".
func TestTheHourAsSomebodyWouldSayIt(t *testing.T) {
	for _, c := range []struct {
		hour int
		said string
	}{
		{0, "midnight"},
		{1, "1am"},
		{9, "9am"},
		{11, "11am"},
		{12, "midday"},
		{13, "1pm"},
		{18, "6pm"},
		{23, "11pm"},
	} {
		if got := (routine.Fact{Hour: c.hour}).Said(); got != c.said {
			t.Errorf("hour %d said as %q, want %q", c.hour, got, c.said)
		}
	}
}

// TestTodayIsInTheirZoneAndOnTheHour : A routine is a time of day,
// so it is worked out where the person is rather than where the
// server keeps its clock. Read in UTC, somebody in India who leaves
// at ten would be measured against half past four in the morning.
func TestTodayIsInTheirZoneAndOnTheHour(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	f := routine.Fact{Hour: 10}

	// Late evening UTC is already tomorrow in India, and the routine
	// belongs to the day they are living in.
	now := time.Date(2026, 10, 8, 20, 30, 0, 0, time.UTC)
	got := f.Today(now, india)

	if got.Hour() != 10 || got.Minute() != 0 || got.Second() != 0 {
		t.Errorf("Today = %v, want ten o'clock exactly", got)
	}
	if y, m, d := got.Date(); y != 2026 || m != time.October || d != 9 {
		t.Errorf("Today = %v, want the 9th, which is the day they are in", got)
	}

	// No zone is UTC rather than a panic.
	if f.Today(now, nil).Hour() != 10 {
		t.Errorf("Today with no zone = %v, want ten o'clock in UTC", f.Today(now, nil))
	}
}
