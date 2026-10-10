package profile_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/world/event"
)

// played : one listen to a named artist, at a moment.
func played(at time.Time, who string) event.Event {
	return event.Event{Kind: "music.state", OccurredAt: at, ReceivedAt: at,
		Source: "tasker", Payload: []byte(fmt.Sprintf(`{"value":%q}`, who))}
}

var noon2 = time.Date(2026, 10, 10, 12, 0, 0, 0, ist)

// TestWhatIsTrending : the owner's own example -- something played
// yesterday, again today, and through the week, is what is rising.
func TestWhatIsTrending(t *testing.T) {
	var evs []event.Event
	// Three weeks of history so the week window has a week behind it.
	for d := 20; d <= 30; d++ {
		evs = append(evs, played(time.Date(2026, 9, d, 20, 0, 0, 0, ist), "Anirudh"))
	}
	// This week, a new artist, over and over.
	for d := 5; d <= 10; d++ {
		for n := 0; n < 3; n++ {
			evs = append(evs, played(time.Date(2026, 10, d, 9+n*4, 0, 0, 0, ist), "Demi Lovato"))
		}
	}

	got := profile.Trending(evs, ist, noon2)
	if !strings.Contains(got, "this week") {
		t.Fatalf("the week should have moved:\n%s", got)
	}
	if !strings.Contains(got, "Demi Lovato") {
		t.Errorf("the thing that is rising should be named:\n%s", got)
	}
	fmt.Println("  trending:", lastLine(got))
}

// TestAWindowWithNoHistoryIsLeftOut : nine days of data cannot say
// anything about a month, and "new this month" would mean only that
// the server was switched on this month.
func TestAWindowWithNoHistoryIsLeftOut(t *testing.T) {
	var evs []event.Event
	for d := 2; d <= 10; d++ {
		evs = append(evs, played(time.Date(2026, 10, d, 20, 0, 0, 0, ist), "Anirudh"))
	}
	got := profile.Trending(evs, ist, noon2)
	if strings.Contains(got, "this month") || strings.Contains(got, "this year") {
		t.Errorf("a window with nothing behind it should be left out:\n%s", got)
	}
}

// TestNothingMovedSaysNothing : the same amount of the same thing is
// not news, and a description that reported it every hour would be
// one nobody reads.
func TestNothingMovedSaysNothing(t *testing.T) {
	var evs []event.Event
	for d := 20; d <= 30; d++ {
		evs = append(evs, played(time.Date(2026, 9, d, 20, 0, 0, 0, ist), "Anirudh"))
	}
	for d := 1; d <= 10; d++ {
		evs = append(evs, played(time.Date(2026, 10, d, 20, 0, 0, 0, ist), "Anirudh"))
	}
	if got := profile.Trending(evs, ist, noon2); strings.Contains(got, "Anirudh") {
		t.Errorf("a steady habit is not a trend:\n%s", got)
	}
}
