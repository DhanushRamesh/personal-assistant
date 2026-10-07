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
	}, id, look, nil, nil) {
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
	all := rules.All(store, rules.Clock{Now: func() time.Time { return noon }, Location: india}, id, look, nil, nil)

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
	if writes != 4 {
		t.Errorf("%d tools write rules, want add, update, quiet and cancel", writes)
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
	}, nil) {
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
	}, id, look, stored{}, nil) {
		by[one.Name] = one
	}

	if got := call(t, by["rule_list"], `{}`); strings.Contains(got.Content, "usually") {
		t.Errorf("it spoke about a routine nobody has:\n%s", got.Content)
	}
}

// TestChangingARuleLeavesTheRestOfItAlone : A rule could be made and
// called off and nothing else, so moving a threshold meant writing a
// new rule and cancelling the old one. Six near-duplicate battery
// rules are what that looks like after a week: tuning one was more
// work than adding another.
//
// Everything a rule is made of can be read back off its steps, so a
// change can name one thing and leave the rest.
func TestChangingARuleLeavesTheRestOfItAlone(t *testing.T) {
	store, by := made()

	made := call(t, by["rule_add"], `{
		"name": "low battery",
		"schedule": "when battery.level < 20",
		"when": "presence is here",
		"ask": "say how low it is",
		"look_up": [{"tool":"calendar_events","args":"{\"days\":1}"}]
	}`)
	if made.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not make one: %s", made.Content)
	}
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("made %d rules", len(held))
	}
	id := held[0].ID

	// Only the threshold.
	got := call(t, by["rule_update"], `{"id":"`+id+`","schedule":"when battery.level < 25"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}

	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("changing it left %d rules, want the one mended", len(held))
	}
	after := held[0]
	if after.Schedule != "when battery.level < 25" {
		t.Errorf("schedule is %q", after.Schedule)
	}
	if after.Name != "low battery" {
		t.Errorf("the name was lost: %q", after.Name)
	}
	if len(after.Steps) != 3 {
		t.Fatalf("it has %d steps, want the lookup, check and write it had: %+v",
			len(after.Steps), after.Steps)
	}
	if after.Steps[0].Tool != "calendar_events" {
		t.Errorf("the lookup was lost: %+v", after.Steps[0])
	}
	if after.Steps[1].When != "presence is here" {
		t.Errorf("the condition was lost: %+v", after.Steps[1])
	}
	if after.Steps[2].Ask != "say how low it is" {
		t.Errorf("what it says was lost: %+v", after.Steps[2])
	}
}

// TestChangingToFixedWordsDropsTheQuestion : Words and a question are
// alternatives, so naming one takes the other away. Without that,
// turning a rule from a worked-out sentence into fixed words would be
// refused for having both.
func TestChangingToFixedWordsDropsTheQuestion(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], `{"name":"n","schedule":"every 10m","ask":"work it out"}`)
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	got := call(t, by["rule_update"], `{"id":"`+id+`","say":"the fixed words"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}
	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	last := held[0].Steps[len(held[0].Steps)-1]
	if last.Do != job.Announce || last.Say != "the fixed words" {
		t.Errorf("it still works the sentence out: %+v", last)
	}
}

// TestChangingARuleThatIsNotThere : An identifier from a listing
// somebody remembered rather than made this turn.
func TestChangingARuleThatIsNotThere(t *testing.T) {
	_, by := made()
	got := call(t, by["rule_update"],
		`{"id":"job_01M48E09RJW0G247RBST6712D9","schedule":"every 5m"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it changed a rule that does not exist: %s", got.Content)
	}
}

// TestARuleCannotBeEditedIntoSomethingItCouldNotHaveBeen : The change
// goes through job.New, so a schedule is read by the same thing that
// reads a new one.
func TestARuleCannotBeEditedIntoSomethingItCouldNotHaveBeen(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], `{"name":"n","schedule":"every 10m","say":"hello"}`)
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	for _, bad := range []string{
		`{"id":"` + id + `","schedule":"whenever I feel like it"}`,
		`{"id":"` + id + `","when":"hour >= 99"}`,
		`{"id":"` + id + `","keep":"a while"}`,
	} {
		if got := call(t, by["rule_update"], bad); got.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s was accepted: %s", bad, got.Content)
		}
	}
	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if held[0].Schedule != "every 10m" {
		t.Errorf("a refused change still landed: %q", held[0].Schedule)
	}
}

// TestARuleDictatedWithTwoConditions : The gap this closes. The
// engine ands across steps and assemble made exactly one check, so a
// rule asked for aloud could have one condition with alternatives
// inside it and no second. That changed the shape of a real rule.
func TestARuleDictatedWithTwoConditions(t *testing.T) {
	store, by := made()

	got := call(t, by["rule_add"], `{
		"name": "late and low",
		"schedule": "every 20m",
		"when": "hour >= 22 and battery.level < 20",
		"say": "it is late and the battery is low"
	}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("two conditions were refused: %s", got.Content)
	}

	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("made %d rules", len(held))
	}
	var checks []string
	for _, step := range held[0].Steps {
		if step.Do == job.Check {
			checks = append(checks, step.When)
		}
	}
	if len(checks) != 2 {
		t.Fatalf("it has %d checks, want one per condition: %v", len(checks), checks)
	}
	if checks[0] != "hour >= 22" || checks[1] != "battery.level < 20" {
		t.Errorf("the checks read %v", checks)
	}
}

// TestChangingARuleKeepsEveryCondition : parts reads the arguments
// back off the steps, and reading only the last check would quietly
// drop the others the first time anybody changed the words.
func TestChangingARuleKeepsEveryCondition(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], `{
		"name": "late and low",
		"schedule": "every 20m",
		"when": "hour >= 22 and battery.level < 20",
		"say": "the old words"
	}`)
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	got := call(t, by["rule_update"], `{"id":"`+id+`","say":"the new words"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}

	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	var checks []string
	for _, step := range held[0].Steps {
		if step.Do == job.Check {
			checks = append(checks, step.When)
		}
	}
	if len(checks) != 2 {
		t.Fatalf("changing the words left %d checks, want both: %v", len(checks), checks)
	}
}

// TestARuleCanBePutAsideAndComeBack : Cancelling was the only way to
// stop a rule and it loses the rule: a week's peace meant remembering
// how it was written in order to have it again.
func TestARuleCanBePutAsideAndComeBack(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], `{"name":"n","schedule":"every 10m","say":"hello"}`)
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	got := call(t, by["rule_quiet"], `{"id":"`+id+`","how_long":"7d"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not quieten it: %s", got.Content)
	}
	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if !held[0].Resting(noon) {
		t.Error("it is not resting")
	}
	if !held[0].Resting(noon.AddDate(0, 0, 6)) {
		t.Error("it woke up inside the week")
	}
	if held[0].Resting(noon.AddDate(0, 0, 8)) {
		t.Error("it is still resting after the week")
	}
	// And the rule itself is untouched: being quiet is not being
	// cancelled.
	if held[0].State != job.Active || len(held[0].Steps) != 1 {
		t.Errorf("quieting it changed the rule: %+v", held[0])
	}

	if got := call(t, by["rule_quiet"], `{"id":"`+id+`"}`); got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not start it again: %s", got.Content)
	}
	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if held[0].Resting(noon) {
		t.Error("it is still resting after being started again")
	}
}

// TestARuleThatHoldsOffAfterSpeaking : The only thing stopping a rule
// repeating itself was the exact sentence it said last time, so the
// same news in different words went through.
func TestARuleThatHoldsOffAfterSpeaking(t *testing.T) {
	store, by := made()
	got := call(t, by["rule_add"],
		`{"name":"n","schedule":"every 10m","say":"hello","cool":"1h"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a cooling rule was refused: %s", got.Content)
	}
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	if held[0].Cooling() != time.Hour {
		t.Errorf("it cools for %s, want an hour", held[0].Cooling())
	}

	// And a length of time that is not one is caught when it is
	// written, in front of whoever wrote it.
	if got := call(t, by["rule_add"],
		`{"name":"m","schedule":"every 10m","say":"hello","cool":"a while"}`); got.Outcome != conversation.OutcomeFailed {
		t.Error("a cool of \"a while\" was accepted")
	}
}

// tried : A trier that reports whatever it is told to.
type tried struct{ out job.Trial }

func (t tried) try(context.Context, job.Job) job.Trial { return t.out }

// TestARuleIsTriedTheMomentItIsMade : The tools refuse a rule that
// cannot be read. What nothing caught was a rule that reads
// perfectly and is wrong -- a condition that can never hold, a
// trigger on the wrong thing. Two of those were written in one
// evening and both were found by running them afterwards.
//
// So the answer comes back with the rule, and the model sees in the
// same turn that its rule would say nothing.
func TestARuleIsTriedTheMomentItIsMade(t *testing.T) {
	store := inmemory.New()
	id := func() string { return "job_01M48E09RJW0G247RBST6712D1" }
	silent := tried{job.Trial{Stopped: true, Why: "the check hour >= 22 did not pass"}}

	by := map[string]tool.Tool{}
	for _, one := range rules.All(store, rules.Clock{
		Now: func() time.Time { return noon }, Location: india,
	}, id, look, nil, silent.try) {
		by[one.Name] = one
	}

	got := call(t, by["rule_add"],
		`{"name":"late","schedule":"every 20m","when":"hour >= 22","say":"it is late"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not make one: %s", got.Content)
	}
	for _, want := range []string{"Created:", "it would say nothing", "hour >= 22 did not pass"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the answer does not carry %q:\n%s", want, got.Content)
		}
	}

	// And the same on a change, which is where a rule is most
	// likely to be quietly broken.
	spoke := tried{job.Trial{Said: "it is late, sir"}}
	by2 := map[string]tool.Tool{}
	for _, one := range rules.All(store, rules.Clock{
		Now: func() time.Time { return noon }, Location: india,
	}, id, look, nil, spoke.try) {
		by2[one.Name] = one
	}
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	changed := call(t, by2["rule_update"],
		`{"id":"`+held[0].ID+`","say":"it is late, sir"}`)
	if !strings.Contains(changed.Content, `it would say: "it is late, sir"`) {
		t.Errorf("a change was not tried:\n%s", changed.Content)
	}
}

// TestARuleIsStillMadeWithNothingToTryItWith : Nil leaves a rule
// made and unproven, which is how every rule was made before this.
func TestARuleIsStillMadeWithNothingToTryItWith(t *testing.T) {
	_, by := made()
	got := call(t, by["rule_add"], `{"name":"n","schedule":"every 10m","say":"hello"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a rule was refused for want of a trier: %s", got.Content)
	}
	if strings.Contains(got.Content, "Run just now") {
		t.Errorf("it claimed to have tried one with nothing to try it with:\n%s", got.Content)
	}
}
