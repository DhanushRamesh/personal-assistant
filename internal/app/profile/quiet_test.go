package profile_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/event"
)

// nights : A fortnight of somebody stopping at one in the morning
// and starting again at nine, with ordinary traffic between.
func nights(days int) []event.Event {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var all []event.Event
	for d := 0; d < days; d++ {
		day := base.AddDate(0, 0, d)
		for _, h := range []int{9, 12, 15, 18, 21, 23} {
			all = append(all, moment(day, h, "wifi.joined", "home"))
		}
		// The last thing, at one the following morning.
		all = append(all, moment(day.AddDate(0, 0, 1), 1, "screen.changed", "Pixel 7"))
	}
	return all
}

func moment(day time.Time, hour int, kind, value string) event.Event {
	payload, _ := json.Marshal(map[string]string{"value": value})
	return event.Event{Kind: kind, Payload: payload,
		OccurredAt: day.Add(time.Duration(hour) * time.Hour)}
}

// TestTheNightIsCounted : The owner's reason for sending the phone's
// locking at all: "it can understand the pattern that at night the
// screen will be locked around 1 am and will be opened again by 9 am
// ... it can understand like I'm sleeping."
//
// Nothing else in the evidence could see it. Things that happen are
// counted by the dozen; what follows what is looked for inside
// ninety minutes. An eight hour silence was invisible.
func TestTheNightIsCounted(t *testing.T) {
	got := profile.Quiet(nights(14), time.UTC)
	if got == "" {
		t.Fatal("a fortnight of nights produced no silence at all")
	}
	// Listed, not summarised: a fortnight of them, each with its own
	// date, so the model can see that they fall on the same hour and
	// can also see when they do not.
	if n := strings.Count(got, "- Tue"); n == 0 {
		t.Errorf("the silences are not dated:\n%s", got)
	}
	if !strings.Contains(got, "1:00 am") {
		t.Errorf("the hour a silence starts is not given:\n%s", got)
	}
	if !strings.Contains(got, "8 hours") {
		t.Errorf("the length is not given:\n%s", got)
	}
	// And it must not say what any of it is. The same shape in the
	// afternoon is something else, and somebody who works nights is
	// described wrongly by anything that assumes.
	for _, guess := range []string{"sleep", "asleep", "in bed"} {
		if strings.Contains(strings.ToLower(got), guess) {
			t.Errorf("the evidence drew the conclusion itself (%q):\n%s", guess, got)
		}
	}
}

// TestAShortGapIsNotASilence : Three hours is the floor. Shorter is a
// meeting, a film, a drive, or a phone left on a desk.
func TestAShortGapIsNotASilence(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	var all []event.Event
	for d := 0; d < 14; d++ {
		day := base.AddDate(0, 0, d)
		// Every two hours, right round the clock: nothing is ever
		// quiet for three.
		for h := 1; h < 24; h += 2 {
			all = append(all, moment(day, h, "wifi.joined", "home"))
		}
	}
	if got := profile.Quiet(all, time.UTC); got != "" {
		t.Errorf("two-hour gaps were called silences:\n%s", got)
	}
}

// TestOneSilenceIsStillListed : Listing is not concluding.
//
// This used to assert the opposite -- that a lone gap produced
// nothing -- which was this file deciding what counted as a routine.
// That is the model's job, and the rule it needs is already in the
// description's own prompt: two occurrences are a coincidence, and
// where the count is small the conclusion is tentative or unsaid.
//
// So one silence is listed, and nothing here calls it a habit.
func TestOneSilenceIsStillListed(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	all := []event.Event{
		moment(base, 9, "wifi.joined", "home"),
		moment(base, 22, "wifi.joined", "home"),
		moment(base.AddDate(0, 0, 1), 9, "wifi.joined", "home"),
	}
	got := profile.Quiet(all, time.UTC)
	if !strings.Contains(got, "- Tue  1 Sep") && !strings.Contains(got, "- Tue 1 Sep") {
		t.Errorf("a silence was not listed:\n%s", got)
	}
	for _, concluded := range []string{"usually", "tends to", "every day", "routine"} {
		if strings.Contains(strings.ToLower(got[strings.Index(got, "- "):]), concluded) {
			t.Errorf("the listing drew a conclusion (%q):\n%s", concluded, got)
		}
	}
}

// TestTheSmallCountRuleIsInThePrompt : What stops one silence
// becoming a routine is said to the model, not decided here.
func TestTheSmallCountRuleIsInThePrompt(t *testing.T) {
	p := profile.Prompt(profile.SoFar, profile.Evidence{Said: said(3)})
	for _, want := range []string{
		"Two occurrences are a coincidence",
		"Where the count is small",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}
