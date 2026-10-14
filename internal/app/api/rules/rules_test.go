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

// TestARuleIsSentInItsTwoHalves : what sets it off and what has to
// hold, as the tree it is kept as, and what it does -- beside the
// sentences, which stay as they were.
func TestARuleIsSentInItsTwoHalves(t *testing.T) {
	j := job.Job{
		ID: "r1", Name: "morning", Kind: "rule", Schedule: "at 08:00",
		When: job.Occasion{
			Wake: []job.Hook{{At: "08:00"}, {On: &job.Where{Source: "door.opened", Op: "arrives"}, After: "5m"}},
			All: []job.Where{
				{Source: "presence", Op: "==", Value: "here"},
				{Any: []job.Where{
					{Source: "day", Op: "==", Value: "weekdays"},
					{Source: "battery.level", Op: "<", Value: 20, Ago: "15m"},
				}},
			},
		},
		Perform: []job.Act{{Do: job.DoTool, Use: "calendar_today"}, {Do: job.DoSay, Text: "good morning"}},
	}
	got := described(j)
	w := got.When
	if len(w.Wake) != 2 || w.Wake[0].Says != "at 08:00" || !w.Wake[0].Clock ||
		w.Wake[1].Clock || !strings.Contains(w.Wake[1].Says, "door.opened") || w.Wake[1].After != "5m" {
		t.Errorf("wake = %+v", w.Wake)
	}
	if w.Join != "all" || len(w.Conditions) != 2 {
		t.Fatalf("conditions = %+v", w)
	}
	if c := w.Conditions[0]; c.Source != "presence" || c.Op != "is" || c.Value != "here" || c.Says != "presence is here" || c.Held != nil {
		t.Errorf("first = %+v", c)
	}
	g := w.Conditions[1]
	if g.Join != "any" || len(g.Conditions) != 2 || g.Conditions[1].Ago != "15m" {
		t.Errorf("group = %+v", g)
	}
	if len(got.Perform) != 2 || got.Perform[0].Says != "calendar_today" || got.Perform[1].Says != "good morning" {
		t.Errorf("perform = %+v", got.Perform)
	}

	// Tried, each condition carries what it answered.
	v := job.Verdict{Held: false, Parts: []job.Verdict{{Held: true}, {Held: false, Parts: []job.Verdict{{Held: false}, {Held: false}}}}}
	tried := whenOf(j, &v)
	if h := tried.Conditions[0].Held; h == nil || !*h {
		t.Errorf("presence held = %v", h)
	}
	if h := tried.Conditions[1].Conditions[0].Held; h == nil || *h {
		t.Errorf("weekdays held = %v", h)
	}
}

// A rule kept in the older shape, with no wake of its own, is shown
// with its written schedule as the one thing that sets it off.
func TestARuleWithNoWakeShowsItsSchedule(t *testing.T) {
	got := described(job.Job{ID: "r3", Name: "low", Kind: "rule", Schedule: "when battery.level < 20"}).When
	if len(got.Wake) != 1 || got.Wake[0].Says != "when battery.level < 20" || got.Wake[0].Clock {
		t.Errorf("wake = %+v", got.Wake)
	}
	if got.Conditions == nil || got.Join != "" {
		t.Errorf("conditions = %+v, join %q", got.Conditions, got.Join)
	}
}
