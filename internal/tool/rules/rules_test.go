package rules_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/job"
	"github.com/DhanushRamesh/personal-assistant/internal/job/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/routine"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/rules"
)

var india = time.FixedZone("IST", 5*3600+1800)

var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, india)

// kept : What the registry would look up, for a rule that reads.
var kept = map[string]tool.Tool{
	"calendar_events": {Name: "calendar_events", Lists: true},
	"calendar_cancel": {Name: "calendar_cancel", Writes: true},
}

func look(name string) (tool.Tool, bool) {
	t, ok := kept[name]
	return t, ok
}

// made : The tools over a fresh store.
func made() (*inmemory.Store, map[string]tool.Tool) {
	store := inmemory.New()
	n := 0
	id := func() string {
		n++
		return "job_01M48E09RJW0G247RBST6712D" + string(rune('0'+n))
	}
	by := map[string]tool.Tool{}
	for _, t := range rules.All(store, rules.Clock{
		Now: func() time.Time { return noon }, Location: india,
	}, id, look, nil) {
		by[t.Name] = t
	}
	return store, by
}

// call : Runs one of them as the person.
func call(t *testing.T, which tool.Tool, args string) tool.Result {
	t.Helper()
	return which.Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1", Channel: chat.ChannelVoice},
		Args:   json.RawMessage(args),
	})
}

// Owner, 6 October 2026, choosing what to build next: *"rules by
// voice first."* Until this, a rule could only be put in by hand.
func TestARuleCanBeSetUpAndListedAndCalledOff(t *testing.T) {
	store, by := made()

	out := call(t, by["rule_add"], `{"name":"the morning diary","schedule":"at 08:00",
		"ask":"say what is coming up today","look_up":[{"tool":"calendar_events","args":"{\"days\":1}"}]}`)
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("rule_add refused: %s", out.Content)
	}
	if !strings.Contains(out.Content, "Created") {
		t.Errorf("it did not acknowledge creating one: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	if len(mine) != 1 {
		t.Fatalf("%d rules were kept", len(mine))
	}
	got := mine[0]
	if got.Schedule != "at 08:00" || got.Name != "the morning diary" {
		t.Errorf("kept as %+v", got)
	}
	// Look things up, then speak.
	if len(got.Steps) != 2 ||
		got.Steps[0].Do != job.Call || got.Steps[0].Tool != "calendar_events" ||
		got.Steps[1].Do != job.Write {
		t.Fatalf("the sequence is %+v", got.Steps)
	}

	listed := call(t, by["rule_list"], `{}`)
	if !strings.Contains(listed.Content, "the morning diary") ||
		!strings.Contains(listed.Content, got.ID) {
		t.Errorf("the listing does not name it with its identifier: %s", listed.Content)
	}

	off := call(t, by["rule_cancel"], `{"id":"`+got.ID+`"}`)
	if off.Outcome == conversation.OutcomeFailed {
		t.Fatalf("rule_cancel refused: %s", off.Content)
	}
	after, _ := store.Get(got.ID)
	if after.State != job.Cancelled {
		t.Errorf("state = %q after calling it off", after.State)
	}
}

// A condition becomes a check before the speaking, so the rule can
// run every two hours and still be quiet in an empty room.
func TestAConditionBecomesACheckBeforeItSpeaks(t *testing.T) {
	store, by := made()

	out := call(t, by["rule_add"], `{"name":"stand up","schedule":"every 2h",
		"when":"presence is here","say":"Time to stand up."}`)
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("refused: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	steps := mine[0].Steps
	if len(steps) != 2 || steps[0].Do != job.Check || steps[1].Do != job.Announce {
		t.Fatalf("the sequence is %+v", steps)
	}
	if steps[0].When != "presence is here" {
		t.Errorf("the check reads %q", steps[0].When)
	}
}

// A rule runs unattended and may run every few minutes. Reading is
// safe to repeat; writing is not, and nobody is there to notice the
// first time it goes wrong.
func TestARuleMayNotRunSomethingThatChangesThings(t *testing.T) {
	_, by := made()

	out := call(t, by["rule_add"], `{"name":"bad","schedule":"every 5m",
		"ask":"say something","look_up":[{"tool":"calendar_cancel"}]}`)
	if out.Outcome != conversation.OutcomeFailed {
		t.Fatalf("a writing tool was accepted into a rule: %s", out.Content)
	}
	if !strings.Contains(out.Content, "calendar_cancel") {
		t.Errorf("it did not say which tool: %s", out.Content)
	}
}

// A rule that cannot work should fail in front of whoever set it
// up, not silently at three in the morning.
func TestAnUnworkableRuleIsRefused(t *testing.T) {
	_, by := made()
	for _, c := range []struct {
		why  string
		args string
	}{
		{"no name", `{"schedule":"every 5m","say":"hello"}`},
		{"no schedule", `{"name":"x","say":"hello"}`},
		{"a schedule nobody can read", `{"name":"x","schedule":"sometimes","say":"hello"}`},
		{"nothing to say", `{"name":"x","schedule":"every 5m"}`},
		{"both ways of saying", `{"name":"x","schedule":"every 5m","say":"a","ask":"b"}`},
		{"a condition nobody can read", `{"name":"x","schedule":"every 5m","say":"a","when":"nonsense"}`},
		{"a tool that does not exist", `{"name":"x","schedule":"every 5m","ask":"a","look_up":[{"tool":"nope"}]}`},
		{"arguments that are not JSON", `{"name":"x","schedule":"every 5m","ask":"a",` +
			`"look_up":[{"tool":"calendar_events","args":"kind=battery"}]}`},
		{"waiting on presence, which never arrives", `{"name":"x","schedule":"when presence is away","say":"a"}`},
		{"too many lookups", `{"name":"x","schedule":"every 5m","ask":"a","look_up":[` +
			`{"tool":"calendar_events"},{"tool":"calendar_events"},` +
			`{"tool":"calendar_events"},{"tool":"calendar_events"}]}`},
	} {
		if out := call(t, by["rule_add"], c.args); out.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s was accepted: %s", c.why, out.Content)
		}
	}
}

// An identifier from somewhere else calls nothing off.
func TestOnlyTheirOwnRuleIsCalledOff(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], `{"name":"theirs","schedule":"every 5m","say":"hello"}`)
	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)

	out := by["rule_cancel"].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_2", Channel: chat.ChannelVoice},
		Args:   json.RawMessage(`{"id":"` + mine[0].ID + `"}`),
	})
	if out.Outcome != conversation.OutcomeFailed {
		t.Errorf("somebody else's rule was called off: %s", out.Content)
	}
	after, _ := store.Get(mine[0].ID)
	if after.State != job.Active {
		t.Errorf("it was stopped anyway: %q", after.State)
	}
}

// A write is refused until its domain has been read, and a rule is
// a domain like any other.
func TestWritingRulesInsistsOnReadingThemFirst(t *testing.T) {
	store, _ := made()
	n := 0
	id := func() string { n++; return "job_01M48E09RJW0G247RBST6712D1" }
	all := rules.All(store, rules.Clock{Now: func() time.Time { return noon }, Location: india}, id, look, nil)

	var writes, lists int
	for _, which := range all {
		if which.Domain != "rule" {
			t.Errorf("%s is not in the rule domain", which.Name)
		}
		if which.Writes {
			writes++
		}
		if which.Lists {
			lists++
		}
	}
	if lists != 1 {
		t.Errorf("%d tools list rules, want exactly one for a write to read first", lists)
	}
	if writes != 2 {
		t.Errorf("%d tools write rules, want add and cancel", writes)
	}
}

// A lookup with no arguments runs on the tool's defaults, which
// for a reading is a week of every kind rather than what that
// thing says now.
//
// Owner, 6 October 2026, on a rule that reported the battery every
// minute: *"but the battery is now 56."* It had been saying 34,
// which was a real reading from the day before -- the tool could
// have answered exactly, and nothing let the rule ask it to.
func TestALookupCarriesItsArguments(t *testing.T) {
	store, by := made()

	out := call(t, by["rule_add"], `{"name":"battery","schedule":"every 1m",
		"ask":"say what the battery is at now",
		"look_up":[{"tool":"calendar_events","args":"{\"kind\":\"battery.level\",\"limit\":1}"}]}`)
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("rule_add refused: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	steps := mine[0].Steps
	if len(steps) != 2 || steps[0].Do != job.Call {
		t.Fatalf("the sequence is %+v", steps)
	}
	var args map[string]any
	if err := json.Unmarshal(steps[0].Args, &args); err != nil {
		t.Fatalf("the arguments were not kept: %v (%s)", err, steps[0].Args)
	}
	if args["kind"] != "battery.level" || args["limit"] != float64(1) {
		t.Errorf("the step runs with %v", args)
	}
}

// A rule written on Monday cannot know what Thursday needs. The
// tools it names are passed to the model, which decides whether
// to call them and with what arguments.
func TestARuleCanBeGivenToolsToDecideWith(t *testing.T) {
	store, by := made()

	out := call(t, by["rule_add"], `{"name":"anything wrong","schedule":"every 30m",
		"ask":"say whether anything needs their attention","tools":["calendar_events"]}`)
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("rule_add refused: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	steps := mine[0].Steps
	if len(steps) != 1 || steps[0].Do != job.Write {
		t.Fatalf("the sequence is %+v", steps)
	}
	if len(steps[0].Tools) != 1 || steps[0].Tools[0] != "calendar_events" {
		t.Errorf("the write may call %v", steps[0].Tools)
	}

	// And what it may reach is readable back, which is the reason
	// for naming them rather than handing over the toolbox.
	listed := call(t, by["rule_list"], `{}`)
	if !strings.Contains(listed.Content, "may call calendar_events") {
		t.Errorf("the listing does not say what it may reach: %s", listed.Content)
	}
}

// A tool that changes something is refused here too, not only as
// a lookup.
func TestAToolAWriteMayCallMustOnlyRead(t *testing.T) {
	_, by := made()
	out := call(t, by["rule_add"], `{"name":"bad","schedule":"every 5m",
		"ask":"say something","tools":["calendar_cancel"]}`)
	if out.Outcome != conversation.OutcomeFailed {
		t.Fatalf("a writing tool was accepted: %s", out.Content)
	}
	if !strings.Contains(out.Content, "calendar_cancel") {
		t.Errorf("it did not say which tool: %s", out.Content)
	}
}

// stored : A routine a listing can read.
type stored []routine.Fact

func (s stored) All(context.Context, string) ([]routine.Fact, error) { return s, nil }
func (s stored) Keep(context.Context, string, []routine.Fact) error  { return nil }

// TestListingARuleShowsWhatElseOneCanAskAbout : Owner, 7 October
// 2026 -- "I'll ask the AI to write the rules by itself with
// examples of the existing rules, and it should use the routine
// too."
//
// Which is why the routine is in this listing and not in a tool of
// its own. rule_add writes, and a writing tool refuses to run until
// the domain has been listed in the same turn, so whatever is here
// is in front of whoever is about to write a rule -- every time,
// without their having to know to ask. The rules are the examples
// and these are the vocabulary.
func TestListingARuleShowsWhatElseOneCanAskAbout(t *testing.T) {
	store := inmemory.New()
	id := func() string { return "job_01M48E09RJW0G247RBST6712D1" }
	by := map[string]tool.Tool{}
	for _, one := range rules.All(store, rules.Clock{
		Now: func() time.Time { return noon }, Location: india,
	}, id, look, stored{
		{Subject: "home", What: routine.Leaves, Hour: 10, Days: 4, Over: 6},
	}) {
		by[one.Name] = one
	}

	got := call(t, by["rule_list"], `{}`)
	for _, want := range []string{
		"usually.home.leaves",
		"home leaves around 10am",
		"4 of 6 days",
		"none of them can be what a rule waits for",
	} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the listing does not offer %q:\n%s", want, got.Content)
		}
	}
}

// TestListingSaysNothingAboutARoutineNobodyHasYet : The first weeks.
// A rule written without these is the rule anybody would have
// written before there were any, and a paragraph explaining that
// there is nothing is worse than silence.
func TestListingSaysNothingAboutARoutineNobodyHasYet(t *testing.T) {
	store := inmemory.New()
	id := func() string { return "job_01M48E09RJW0G247RBST6712D1" }
	by := map[string]tool.Tool{}
	for _, one := range rules.All(store, rules.Clock{
		Now: func() time.Time { return noon }, Location: india,
	}, id, look, stored{}) {
		by[one.Name] = one
	}

	if got := call(t, by["rule_list"], `{}`); strings.Contains(got.Content, "usually") {
		t.Errorf("it spoke about a routine nobody has:\n%s", got.Content)
	}
}
