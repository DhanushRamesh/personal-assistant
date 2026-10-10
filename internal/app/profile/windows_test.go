package profile_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/world/event"
)

var ist = time.FixedZone("IST", 5*3600+1800)

// came : One arrival home, in October, at a given hour.
func came(day, hour, minute int) event.Event { return home(10, day, hour, minute) }

// home : One arrival home, on a given month and day.
func home(month, day, hour, minute int) event.Event {
	at := time.Date(2026, time.Month(month), day, hour, minute, 0, 0, ist)
	return event.Event{Kind: "place.entered", OccurredAt: at, ReceivedAt: at,
		Source: "tasker", Payload: []byte(`{"value":"home"}`)}
}

// TestLateTonightIsSaid : the whole point. Three weeks of arriving at
// nine and one arrival at midnight, and the description has to be
// able to say the second thing.
func TestLateTonightIsSaid(t *testing.T) {
	var evs []event.Event
	for day := 1; day <= 9; day++ {
		evs = append(evs, came(day, 21, 5))
	}
	now := time.Date(2026, 10, 10, 0, 30, 0, 0, ist)
	evs = append(evs, came(10, 0, 10)) // tonight, ten past midnight

	got := profile.Windows(evs, ist, now)
	if !strings.Contains(got, "usually around 9pm, today at midnight") {
		t.Fatalf("it should say the habit and tonight:\n%s", got)
	}
	fmt.Println("  late tonight:", lastLine(got))
}

// TestAnOrdinaryDayIsNotRemarkedOn : the thing that keeps it from
// becoming a nag. Arriving at the usual hour says nothing at all.
func TestAnOrdinaryDayIsNotRemarkedOn(t *testing.T) {
	var evs []event.Event
	for day := 1; day <= 10; day++ {
		evs = append(evs, came(day, 21, 5))
	}
	now := time.Date(2026, 10, 10, 21, 30, 0, 0, ist)

	if got := profile.Windows(evs, ist, now); got != "" {
		t.Errorf("an ordinary evening should be left unsaid:\n%s", got)
	}
}

// TestAHabitWithNoHourIsNotLate : somebody who does a thing at no
// particular time cannot do it late, and saying so would be noise.
func TestAHabitWithNoHourIsNotLate(t *testing.T) {
	var evs []event.Event
	for day := 1; day <= 9; day++ {
		evs = append(evs, came(day, 6+day, 0)) // a different hour every day
	}
	now := time.Date(2026, 10, 10, 2, 0, 0, 0, ist)
	evs = append(evs, came(10, 2, 0))

	if got := profile.Windows(evs, ist, now); got != "" {
		t.Errorf("no usual hour means nothing to be late against:\n%s", got)
	}
}

// TestThisWeekDriftsWithoutToday : the middle window earns its place.
// Nothing today, but the week has moved an hour and a half.
func TestThisWeekDriftsWithoutToday(t *testing.T) {
	// Three weeks of nine o'clock against five late nights, so the
	// baseline is not simply this week over again. The first draft
	// of this test used three old days against five new ones --
	// which makes "usually" and "this week" the same hour, so it
	// passed by producing nothing at all.
	var evs []event.Event
	for day := 12; day <= 30; day++ { // September, the baseline
		evs = append(evs, home(9, day, 21, 0))
	}
	for day := 5; day <= 9; day++ { // this week, two hours later
		evs = append(evs, home(10, day, 23, 0))
	}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, ist)

	// Asserted on the habit line and not on the header, which also
	// contains the words "this week" and let two earlier versions of
	// this test pass while producing nothing.
	got := profile.Windows(evs, ist, now)
	if !strings.Contains(got, "usually around 9pm, but around 11pm this week") {
		t.Fatalf("a week that has drifted should say so:\n%s", got)
	}
	fmt.Println("  week drift:", lastLine(got))
}

// lastLine : The habit line, for showing what a test measured.
func lastLine(s string) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(parts[len(parts)-1])
}
