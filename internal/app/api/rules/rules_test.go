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
