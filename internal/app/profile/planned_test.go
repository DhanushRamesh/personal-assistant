package profile_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/app/profile"
	"github.com/DhanushRamesh/personal-assistant/internal/domain/calendar"
)

var here = time.FixedZone("IST", 5*3600+1800)

// A diary entry says somebody meant to be somewhere. Whether they
// were is in the readings, if it is anywhere, and the description
// had never been shown the first half at all.
func TestTheDiaryIsGivenAsPlansAndNotAsFacts(t *testing.T) {
	at := time.Date(2026, 10, 8, 16, 0, 0, 0, here)
	got := profile.Planned([]calendar.Event{{
		Title:  "Dentist",
		Where:  "Apollo",
		Starts: at,
		Ends:   at.Add(45 * time.Minute),
		Mine:   true,
	}}, here)

	for _, want := range []string{"diary", "Dentist", "Apollo", "4:00 pm", "Thu 8 Oct", "45 minutes"} {
		if !strings.Contains(got, want) {
			t.Errorf("the entry is missing %q:\n%s", want, got)
		}
	}

	// And the rule that makes it safe to show at all.
	p := profile.Prompt(profile.SoFar, profile.Evidence{Planned: got})
	for _, want := range []string{
		"not evidence they were",
		"say nothing about it at all",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
}

// Somebody else's plan that they were asked to is a weaker
// statement of intent than one they made.
func TestAnInvitationIsMarkedAsOne(t *testing.T) {
	at := time.Date(2026, 10, 8, 11, 0, 0, 0, here)
	mine := profile.Planned([]calendar.Event{
		{Title: "Standup", Starts: at, Ends: at.Add(time.Hour), Mine: true}}, here)
	theirs := profile.Planned([]calendar.Event{
		{Title: "Standup", Starts: at, Ends: at.Add(time.Hour), Mine: false}}, here)

	if strings.Contains(mine, "invited") {
		t.Errorf("their own entry was marked as an invitation:\n%s", mine)
	}
	if !strings.Contains(theirs, "invited") {
		t.Errorf("an invitation was not marked:\n%s", theirs)
	}
}

// An all-day entry has no hour, and inventing one would be the
// description manufacturing precision.
func TestAnAllDayEntryIsNotGivenAnHour(t *testing.T) {
	got := profile.Planned([]calendar.Event{{
		Title:  "Diwali",
		Starts: time.Date(2026, 11, 8, 0, 0, 0, 0, here),
		AllDay: true,
	}}, here)

	if !strings.Contains(got, "all day") {
		t.Errorf("it is not marked all day:\n%s", got)
	}
	if strings.Contains(got, "12:00 am") {
		t.Errorf("midnight was invented as its hour:\n%s", got)
	}
}

// Nothing in the diary is nothing in the prompt, rather than a
// heading with no entries under it.
func TestAnEmptyDiarySaysNothing(t *testing.T) {
	if got := profile.Planned(nil, here); got != "" {
		t.Errorf("an empty diary produced %q", got)
	}
	if got := profile.Planned([]calendar.Event{{Title: "  "}}, here); got != "" {
		t.Errorf("an entry with no title produced %q", got)
	}
}
