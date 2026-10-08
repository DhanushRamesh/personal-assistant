package job_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
)

// shaped : A rule written the old way, reshaped, as compact JSON.
func shaped(t *testing.T, schedule string, steps ...job.Step) (job.Occasion, []job.Act, string) {
	t.Helper()
	j, err := job.New("job_1", "usr_1", "a rule", "rule", schedule, steps, noon)
	if err != nil {
		t.Fatalf("job.New(%q): %v", schedule, err)
	}
	when, acts, err := job.Reshape(j)
	if err != nil {
		t.Fatalf("Reshape(%q): %v", schedule, err)
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(map[string]any{"when": when, "perform": acts})
	return when, acts, b.String()
}

func say(text string) job.Step { return job.Step{Do: job.Announce, Say: text} }

// TestTheScheduleBecomesHooks : The old shape kept what wakes a
// rule as a sentence and what qualifies it as steps. Both halves
// of that sentence become hooks, and several hooks mean any one of
// them fires it -- which is also how two triggers on one rule can
// be written at all.
func TestTheScheduleBecomesHooks(t *testing.T) {
	for _, c := range []struct {
		schedule string
		want     string
	}{
		{"every 30m", `{"every":"30m"}`},
		{"at 08:00", `{"at":"08:00"}`},
		{"cron 0 9 * * 1-5", `{"cron":"0 9 * * 1-5"}`},
		{"when battery.level < 20", `{"on":{"source":"battery.level","op":"<","value":20}}`},
		{"when power.plugged arrives then 15m",
			`{"on":{"source":"power.plugged","op":"arrives"},"after":"15m"}`},
	} {
		_, _, got := shaped(t, c.schedule, say("hello"))
		if !strings.Contains(got, c.want) {
			t.Errorf("%q gave\n  %s\nwanted it to contain\n  %s", c.schedule, got, c.want)
		}
	}

	// A clock and a trigger is two hooks, not a choice between them.
	when, _, _ := shaped(t, "every 30m also when battery.level < 20", say("hello"))
	if len(when.Wake) != 2 {
		t.Errorf("a rule with both halves gave %d hooks: %+v", len(when.Wake), when.Wake)
	}
}

// TestEveryCheckBecomesACondition : A check could only ever read a
// source, never what the step before it returned, so every one of
// them was a precondition wherever it sat. They all move, and
// nothing is left for a check to do.
func TestEveryCheckBecomesACondition(t *testing.T) {
	when, acts, got := shaped(t, "every 10m",
		job.Step{Do: job.Call, Tool: "calendar_events"},
		job.Step{Do: job.Check, When: "hour < 18"},
		job.Step{Do: job.Check, When: "battery.level < 25"},
		job.Step{Do: job.Write, Ask: "say something"})

	if len(when.All) != 2 {
		t.Errorf("got %d conditions, want 2: %s", len(when.All), got)
	}
	if len(acts) != 2 {
		t.Errorf("got %d acts, want 2 (the check is gone): %s", len(acts), got)
	}
	for _, a := range acts {
		if a.Do == "check" {
			t.Error("a check survived into perform")
		}
	}
	// The order of what is left is the order it was written in.
	if acts[0].Do != job.DoTool || acts[1].Do != job.DoWrite {
		t.Errorf("the steps were reordered: %+v", acts)
	}
}

// TestAlternativesBecomeAGroup : "or" within one condition is a
// group; one condition on its own stays one, because wrapping a
// single thing is a layer for a reader to see through.
func TestAlternativesBecomeAGroup(t *testing.T) {
	when, _, got := shaped(t, "every 10m",
		job.Step{Do: job.Check, When: "hour >= 22 or day is weekend"},
		say("hello"))

	if len(when.All) != 1 {
		t.Fatalf("got %d conditions: %s", len(when.All), got)
	}
	group := when.All[0]
	if !group.Group() || len(group.Any) != 2 {
		t.Fatalf("alternatives did not become a group: %s", got)
	}
	if group.Any[0].Source != "hour" || group.Any[1].Source != "day" {
		t.Errorf("group reads %+v", group.Any)
	}

	// One alternative is not a group.
	when, _, got = shaped(t, "every 10m",
		job.Step{Do: job.Check, When: "hour >= 22"}, say("hello"))
	if when.All[0].Group() {
		t.Errorf("wrapped a single condition in a group: %s", got)
	}
}

// TestTheMiniLanguagesBecomeStructure : The two fields that held a
// notation invented here. Neither survives: a count becomes keys,
// and "15m ago" becomes a key of its own, because in a value field
// it looked like an ordinary value and writing "15m" by mistake
// gave a rule that compared the battery against fifteen.
func TestTheMiniLanguagesBecomeStructure(t *testing.T) {
	when, _, got := shaped(t, "every 10m",
		job.Step{Do: job.Check, When: "counted call.missed is amma by value within 2h >= 2"},
		job.Step{Do: job.Check, When: "battery.level <= 20m ago"},
		say("hello"))

	count := when.All[0]
	if count.Count == nil {
		t.Fatalf("a count stayed a string: %s", got)
	}
	want := job.Counted{Kind: "call.missed", Is: "amma", By: "value", Within: "2h"}
	if *count.Count != want {
		t.Errorf("count read as %+v, want %+v", *count.Count, want)
	}
	if count.Source != "" {
		t.Errorf("a count kept a source as well: %q", count.Source)
	}
	if count.Op != ">=" || count.Value != 2 {
		t.Errorf("count compared as %q %v", count.Op, count.Value)
	}

	past := when.All[1]
	if past.Ago != "20m" {
		t.Errorf("ago read as %q, want %q -- this is where 20m0s became 20", past.Ago, "20m")
	}
	if past.Value != nil {
		t.Errorf("a comparison with its own past kept a value as well: %v", past.Value)
	}
}

// TestWordOperatorsBecomeSymbols : is and is not are what somebody
// says and what a rule is read back as; == and != are what is
// stored, so that a dictated rule and a generated one converge on
// the same thing.
func TestWordOperatorsBecomeSymbols(t *testing.T) {
	when, _, _ := shaped(t, "every 10m",
		job.Step{Do: job.Check, When: "presence is here"},
		job.Step{Do: job.Check, When: "battery.level is not 100"},
		job.Step{Do: job.Check, When: "link.google older than 20m"},
		say("hello"))

	for i, want := range []string{"==", "!=", "older_than"} {
		if got := when.All[i].Op; got != want {
			t.Errorf("condition %d stored %q, want %q", i, got, want)
		}
	}
	// within is already one word and keeps it.
	when, _, _ = shaped(t, "every 10m",
		job.Step{Do: job.Check, When: "battery.level within 1h"}, say("hello"))
	if got := when.All[0].Op; got != "within" {
		t.Errorf("within stored as %q", got)
	}
}

// TestEveryStepKindConverts : Nothing may be left behind, because a
// rule half converted is a rule that does half of what it did.
func TestEveryStepKindConverts(t *testing.T) {
	_, acts, got := shaped(t, "every 10m",
		job.Step{Do: job.Call, Tool: "coming_due", Args: []byte(`{"saying":"looking"}`)},
		job.Step{Do: job.Relay},
		job.Step{Do: job.Write, Ask: "say it", Tools: []string{"event_recent"}},
		job.Step{Do: job.Announce, Say: "fixed words"})

	want := []string{job.DoTool, job.DoRelay, job.DoWrite, job.DoSay}
	if len(acts) != len(want) {
		t.Fatalf("got %d acts: %s", len(acts), got)
	}
	for i, w := range want {
		if acts[i].Do != w {
			t.Errorf("act %d is %q, want %q", i, acts[i].Do, w)
		}
	}
	if acts[0].Use != "coming_due" || !strings.Contains(string(acts[0].With), "looking") {
		t.Errorf("a tool lost its name or its arguments: %+v", acts[0])
	}
	if acts[2].Ask != "say it" || len(acts[2].Tools) != 1 {
		t.Errorf("a write lost something: %+v", acts[2])
	}
	if acts[3].Text != "fixed words" {
		t.Errorf("an announce lost its words: %+v", acts[3])
	}
}

// TestALengthReadsAsItWasWritten : A converted rule has to read as
// the one it came from, or nobody will believe the conversion.
func TestALengthReadsAsItWasWritten(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want string
	}{
		{20 * time.Minute, "20m"},
		{90 * time.Minute, "90m"},
		{2 * time.Hour, "2h"},
		{30 * time.Second, "30s"},
	} {
		when, _, _ := shaped(t, "every 10m",
			job.Step{Do: job.Check, When: "battery.level <= " + c.want + " ago"}, say("x"))
		if got := when.All[0].Ago; got != c.want {
			t.Errorf("%v read back as %q, want %q", c.in, got, c.want)
		}
	}
}
