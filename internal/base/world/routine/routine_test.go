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
