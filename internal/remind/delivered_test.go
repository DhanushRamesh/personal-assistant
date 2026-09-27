package remind_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

var ist = time.FixedZone("IST", 5*3600+1800)

// held : Two reminders that were kept back, thirteen minutes apart.
func twoHeld() []remind.Reminder {
	base := time.Date(2026, 9, 27, 12, 38, 0, 0, ist)
	return []remind.Reminder{
		{Title: "Water", Body: "Time to go drink water, sir.", DueAt: base},
		{Title: "Eat", Body: "Time to go down and eat, sir.", DueAt: base.Add(13 * time.Minute)},
	}
}

// The fault this was written for. Said one at a time, two reminders
// repeat the preamble and the address for each: "You should have heard
// this at 12:38, sir. Time to drink water, sir. You should have heard
// this at 12:51, sir. Time to go down and eat, sir."
func TestSeveralAreSaidAsAGroup(t *testing.T) {
	held := twoHeld()
	got := remind.Delivered(held, held[0].DueAt.Add(20*time.Minute), ist)

	if n := strings.Count(got, "sir"); n > 1 {
		t.Errorf("addressed %d times in one breath:\n%s", n, got)
	}
	if n := strings.Count(got, "should have heard"); n > 0 {
		t.Errorf("the preamble is repeated per reminder:\n%s", got)
	}
	for _, want := range []string{"Two things", "12:38", "12:51", "drink water", "down and eat"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// One on its own is said as it would have been said, in full.
func TestOneIsSaidAsItself(t *testing.T) {
	held := twoHeld()[:1]
	got := remind.Delivered(held, held[0].DueAt.Add(20*time.Minute), ist)

	if !strings.Contains(got, "Time to go drink water, sir.") {
		t.Errorf("Delivered = %q, want the reminder as written", got)
	}
	if strings.Contains(got, "Two things") {
		t.Errorf("one reminder was announced as a list: %q", got)
	}
}

// The past-tense wording is used in a group too, where there is one.
func TestAGroupPrefersThePastTenseWording(t *testing.T) {
	held := twoHeld()
	held[0].SaidLate = "You should have drunk some water"
	got := remind.Delivered(held, held[0].DueAt.Add(20*time.Minute), ist)

	if !strings.Contains(got, "you should have drunk some water") {
		t.Errorf("the past wording was not used:\n%s", got)
	}
}

// Nothing held says nothing at all, rather than an empty preamble.
func TestNothingHeldSaysNothing(t *testing.T) {
	if got := remind.Delivered(nil, time.Now(), ist); got != "" {
		t.Errorf("Delivered = %q, want empty", got)
	}
}
