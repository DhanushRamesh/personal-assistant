package rules

import (
	"strings"
	"testing"

	"personal-assistant/internal/domain/job"
)

// The owner looked at the list for a rule he had asked for the day
// before and could not find it. It was there: what it said was
// "when screen.unlocked arrives", because the line is built from the
// wake hooks alone, and the conditions that make it his rule -- the
// five hours, the hour of the day -- were a tap away.
func TestTheSummaryLineSaysWhatGatesARule(t *testing.T) {
	j := job.Job{
		ID: "r1", Name: "slept and woke", Kind: "rule",
		Schedule: "when screen.unlocked arrives",
		When: job.Occasion{
			All: []job.Where{
				{Source: "screen.locked", Op: "older_than", Value: "5h"},
				{Source: "hour", Op: ">=", Value: 8},
			},
		},
	}
	got := described(j)

	if !strings.Contains(got.Schedule, "screen.unlocked") {
		t.Errorf("the trigger went missing: %q", got.Schedule)
	}
	if !strings.Contains(got.Schedule, "5h") {
		t.Errorf("the five hours are still not on the line: %q", got.Schedule)
	}
	// Still a step as well, since that is where the detail lives.
	var when bool
	for _, s := range got.Steps {
		if s.Do == "when" {
			when = true
		}
	}
	if !when {
		t.Error("the conditions are no longer listed as a step")
	}
}

// A rule with nothing to check reads exactly as it did.
func TestARuleWithNoConditionsIsUnchanged(t *testing.T) {
	j := job.Job{ID: "r2", Name: "what is coming", Kind: "rule", Schedule: "every 5m"}
	if got := described(j).Schedule; got != "every 5m" {
		t.Errorf("an ungated rule now reads %q", got)
	}
}

// TestWhyARuleExistsOutlivesWhatItDidLastTime : the two are separate
// fields because they are separate facts.
//
// The reason a rule was written was kept in Why, which Ran overwrites
// with the outcome of every single run -- so it survived exactly until
// the first time the rule worked.
func TestWhyARuleExistsOutlivesWhatItDidLastTime(t *testing.T) {
	j := job.Job{
		ID: "j1", Name: "the phone did not charge", Kind: "noticed",
		Schedule: "when battery.level arrives", State: job.Active,
		Why:     "the condition battery.level < 20 did not hold",
		Because: "charged on 8 of 9 nights, and the two it did not were both days he ran flat by evening",
	}
	got := described(j)
	if got.Because != j.Because {
		t.Errorf("because = %q, want %q", got.Because, j.Because)
	}
	if got.Why != j.Why {
		t.Errorf("why = %q, want %q", got.Why, j.Why)
	}
	if got.Kind != "noticed" {
		t.Errorf("kind = %q, want the marker intact", got.Kind)
	}
}

// TestARuleTheyAskedForExplainsNothing : because is empty for theirs,
// and the field is dropped from the wire rather than sent blank.
func TestARuleTheyAskedForExplainsNothing(t *testing.T) {
	got := described(job.Job{
		ID: "j2", Name: "wake me at seven", Kind: "rule",
		Schedule: "at 07:00", State: job.Active,
	})
	if got.Because != "" {
		t.Errorf("because = %q, want empty for a rule they asked for", got.Because)
	}
}
