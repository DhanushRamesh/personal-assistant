package profile_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/world/event"
)

var asOf = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// happened : One thing, at a given number of days before asOf, spread
// across the hours so nothing folds into its neighbour.
func occurred(kind, value string, daysAgo ...float64) []event.Event {
	out := make([]event.Event, 0, len(daysAgo))
	for _, d := range daysAgo {
		payload, _ := json.Marshal(map[string]string{"value": value})
		out = append(out, event.Event{
			Kind:       kind,
			Payload:    payload,
			OccurredAt: asOf.Add(-time.Duration(d * float64(24*time.Hour))),
		})
	}
	return out
}

// TestAThingThatStoppedIsReported : The reason this exists. Something
// that stopped appears in no list of what happened, so no amount of
// sending more evidence produces it. It is only the difference
// between two counts.
func TestAThingThatStoppedIsReported(t *testing.T) {
	var all []event.Event
	// Four weeks of going to the gym, and none this week.
	all = append(all, occurred("place.stayed", "gym", 27, 25, 22, 20, 15, 13, 9)...)
	// Something steady throughout, which must not be reported.
	all = append(all, occurred("wifi.joined", "home", 26, 19, 12, 5, 3, 1)...)

	got := profile.Changed(all, time.UTC, asOf)
	if !strings.Contains(got, "No longer happening") {
		t.Fatalf("a habit that stopped was not noticed:\n%s", got)
	}
	if !strings.Contains(got, "gym") {
		t.Errorf("the thing that stopped is not named:\n%s", got)
	}
	if strings.Contains(got, "No longer happening:\n- wifi") ||
		strings.Contains(got, "home\": 6 times before, none this week") {
		t.Errorf("something still happening was reported as stopped:\n%s", got)
	}
}

// TestSomethingNewIsReported : The other direction.
func TestSomethingNewIsReported(t *testing.T) {
	var all []event.Event
	all = append(all, occurred("wifi.joined", "home", 26, 19, 12, 5, 3, 1)...)
	all = append(all, occurred("bluetooth.changed", "car", 5, 4, 2, 1)...)

	got := profile.Changed(all, time.UTC, asOf)
	if !strings.Contains(got, "New this week") || !strings.Contains(got, "car") {
		t.Errorf("something new was not noticed:\n%s", got)
	}
}

// TestTwoOccurrencesAreNotAChange : Two of anything is a coincidence,
// and a description announcing that somebody has stopped doing
// something on the strength of two is worse than one that says
// nothing.
func TestTwoOccurrencesAreNotAChange(t *testing.T) {
	var all []event.Event
	all = append(all, occurred("place.stayed", "barber", 20, 14)...)
	all = append(all, occurred("wifi.joined", "home", 26, 19, 12, 5, 3, 1)...)

	if got := profile.Changed(all, time.UTC, asOf); strings.Contains(got, "barber") {
		t.Errorf("two occurrences were called a habit that ended:\n%s", got)
	}
}

// TestOneEveningIsNotAHabitThatEnded : Three in one evening is one
// occasion. Counting days as well as times is what tells them apart.
func TestOneEveningIsNotAHabitThatEnded(t *testing.T) {
	var all []event.Event
	// Three, well apart so none folds, but all on the same day.
	all = append(all, occurred("music.state", "a song", 20, 20-0.2, 20-0.4)...)
	all = append(all, occurred("wifi.joined", "home", 26, 19, 12, 5, 3, 1)...)

	if got := profile.Changed(all, time.UTC, asOf); strings.Contains(got, "a song") {
		t.Errorf("one evening was called a habit that ended:\n%s", got)
	}
}

// TestNoHistoryReportsNothing : The first four weeks. Everything a
// device has ever sent is new, and saying so describes the day the
// phone was set up rather than the person.
func TestNoHistoryReportsNothing(t *testing.T) {
	all := occurred("wifi.joined", "home", 5, 4, 3, 2, 1)
	if got := profile.Changed(all, time.UTC, asOf); got != "" {
		t.Errorf("a week of history produced a comparison:\n%s", got)
	}
}
