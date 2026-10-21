package rules_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/rules"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/world/routine"
	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/job/inmemory"
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

// echoing : A writer that hands back whatever it was asked for.
//
// The real one puts the request into a brief and asks a model for
// JSON. This takes the request straight back out of that brief and
// answers with it, so a test writes the JSON itself and it still
// travels the whole of the real path -- the brief, the reading, the
// sequence being checked, the tree being sound. What is not
// measured here is the model, which is the one part a test could
// never measure anyway.
func echoing() rules.Writer {
	return rules.Writer{
		Ask: func(_ context.Context, _, brief string) (string, error) {
			_, after, found := strings.Cut(brief, "What they asked for: ")
			if !found {
				return "", nil
			}
			said, _, _ := strings.Cut(after, "\n\n")
			return said, nil
		},
		// One go, so a test that means to be refused is refused
		// rather than asked the same thing three times.
		Tries: 1,
	}
}

// adding : rule_add arguments, from a rule written the way the
// writer answers one.
//
// The two halves move into asked, where the echoing writer finds
// them and hands them back; everything else -- the name, how long
// to keep it, whether it follows them out of the room -- stays an
// argument to the tool, because none of those is the writer's
// business. The literals in these tests are unchanged, which is
// the point: what they assert is what they asserted.
func adding(args string) string {
	var all map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &all); err != nil {
		return args
	}
	rule := map[string]json.RawMessage{}
	for _, half := range []string{"when", "perform"} {
		if v, ok := all[half]; ok {
			rule[half] = v
			delete(all, half)
		}
	}
	body, err := json.Marshal(rule)
	if err != nil {
		return args
	}
	all["asked"] = json.RawMessage(strconv.Quote(string(body)))
	out, err := json.Marshal(all)
	if err != nil {
		return args
	}
	return string(out)
}

// changing : rule_update arguments, the same way.
//
// A change goes through the writer too, so the halves travel in
// asked exactly as they do for a new rule. The id, the name and
// everything that is not about what the rule does stay arguments.
func changing(args string) string { return adding(args) }

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
	}, id, look, nil, nil, echoing()) {
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

	out := call(t, by["rule_add"], adding(`{"name":"the morning diary","when":{"wake":[{"at":"08:00"}]},
		"perform":[{"do":"tool","use":"calendar_events","with":{"days":1}},
		{"do":"write","ask":"say what is coming up today"}]}`))
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

	out := call(t, by["rule_add"], adding(`{"name":"stand up","when":{"wake":[{"every":"2h"}],
		"all":[{"source":"presence","op":"==","value":"here"}]},
		"perform":[{"do":"say","text":"Time to stand up."}]}`))
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("refused: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	// The condition is a condition on the rule, and the sequence
	// is only the doing.
	if len(mine[0].When.All) != 1 || len(mine[0].Perform) != 1 {
		t.Fatalf("%d conditions and %d acts", len(mine[0].When.All), len(mine[0].Perform))
	}
	if said := mine[0].When.Describe(); said != "presence is here" {
		t.Errorf("the condition reads %q", said)
	}
	if mine[0].Perform[0].Do != job.DoSay {
		t.Errorf("the sequence is %+v", mine[0].Perform)
	}
	// The older columns are rendered from it, so a listing and a
	// record of a run read the same whichever shape it arrived in.
	if len(mine[0].Steps) != 2 || mine[0].Steps[0].Do != job.Check {
		t.Errorf("the rendered steps are %+v", mine[0].Steps)
	}
}

// A rule runs unattended and may run every few minutes. Reading is
// safe to repeat; writing is not, and nobody is there to notice the
// first time it goes wrong.
func TestARuleMayNotRunSomethingThatChangesThings(t *testing.T) {
	_, by := made()

	out := call(t, by["rule_add"], adding(`{"name":"bad","when":{"wake":[{"every":"5m"}]},
		"perform":[{"do":"tool","use":"calendar_cancel","with":{"id":"x"}},
		{"do":"write","ask":"say something"}]}`))
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
		{"no name", `{"when":{"wake":[{"every":"5m"}]},"perform":[{"do":"say","text":"hello"}]}`},
		{"nothing to wake it", `{"name":"x","perform":[{"do":"say","text":"hello"}]}`},
		{"a clock nobody can read", `{"name":"x","when":{"wake":[{"every":"sometimes"}]},` +
			`"perform":[{"do":"say","text":"hello"}]}`},
		{"nothing to do", `{"name":"x","when":{"wake":[{"every":"5m"}]}}`},
		{"nothing that says anything", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"tool","use":"calendar_events","with":{"days":1}}]}`},
		{"a say with no words", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"say"}]}`},
		{"a write with nothing to work out", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"write"}]}`},
		{"a condition nobody can read", `{"name":"x","when":{"wake":[{"every":"5m"}],` +
			`"all":[{"source":"battery.level","op":"nonsense","value":1}]},` +
			`"perform":[{"do":"say","text":"a"}]}`},
		{"a tool that does not exist", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"tool","use":"nope","with":{"a":1}},{"do":"write","ask":"a"}]}`},
		{"a tool with no arguments", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"tool","use":"calendar_events"},{"do":"write","ask":"a"}]}`},
		{"something a rule does not do", `{"name":"x","when":{"wake":[{"every":"5m"}]},` +
			`"perform":[{"do":"dance"}]}`},
		{"waiting on presence, which never arrives", `{"name":"x",` +
			`"when":{"wake":[{"on":{"source":"presence","op":"==","value":"away"}}]},` +
			`"perform":[{"do":"say","text":"a"}]}`},
		{"too many lookups", `{"name":"x","when":{"wake":[{"every":"5m"}]},"perform":[` +
			`{"do":"tool","use":"calendar_events","with":{"days":1}},` +
			`{"do":"tool","use":"calendar_events","with":{"days":2}},` +
			`{"do":"tool","use":"calendar_events","with":{"days":3}},` +
			`{"do":"tool","use":"calendar_events","with":{"days":4}},` +
			`{"do":"write","ask":"a"}]}`},
	} {
		if out := call(t, by["rule_add"], adding(c.args)); out.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s was accepted: %s", c.why, out.Content)
		}
	}
}

// An identifier from somewhere else calls nothing off.
func TestOnlyTheirOwnRuleIsCalledOff(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], adding(`{"name":"theirs","when":{"wake":[{"every":"5m"}]},"perform":[{"do":"say","text":"hello"}]}`))
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
	all := rules.All(store, rules.Clock{Now: func() time.Time { return noon }, Location: india}, id, look, nil, nil, echoing())

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
	if writes != 5 {
		t.Errorf("%d tools write rules, want add, update, quiet, cancel and allow", writes)
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

	out := call(t, by["rule_add"], adding(`{"name":"battery","when":{"wake":[{"every":"1m"}]},
		"perform":[{"do":"tool","use":"calendar_events","with":{"kind":"battery.level","limit":1}},
		{"do":"write","ask":"say what the battery is at now"}]}`))
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

	out := call(t, by["rule_add"], adding(`{"name":"anything wrong","when":{"wake":[{"every":"30m"}]},
		"perform":[{"do":"write","ask":"say whether anything needs their attention",
		"tools":["calendar_events"]}]}`))
	if out.Outcome == conversation.OutcomeFailed {
		t.Fatalf("rule_add refused: %s", out.Content)
	}

	mine, _ := store.Mine(context.Background(), "usr_1", "rule", 50)
	acts := mine[0].Perform
	if len(acts) != 1 || acts[0].Do != job.DoWrite {
		t.Fatalf("the sequence is %+v", acts)
	}
	if len(acts[0].Tools) != 1 || acts[0].Tools[0] != "calendar_events" {
		t.Errorf("the write may call %v", acts[0].Tools)
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
	out := call(t, by["rule_add"], adding(`{"name":"bad","when":{"wake":[{"every":"5m"}]},
		"perform":[{"do":"write","ask":"say something","tools":["calendar_cancel"]}]}`))
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
	}, nil, echoing()) {
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
	}, id, look, stored{}, nil, echoing()) {
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

	made := call(t, by["rule_add"], adding(`{
		"name": "low battery",
		"when": {
			"wake": [{"on":{"source":"battery.level","op":"<","value":20}}],
			"all":  [{"source":"presence","op":"==","value":"here"}]
		},
		"perform": [
			{"do":"tool","use":"calendar_events","with":{"days":1}},
			{"do":"write","ask":"say how low it is"}
		]
	}`))
	if made.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not make one: %s", made.Content)
	}
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("made %d rules", len(held))
	}
	id := held[0].ID

	// Only the when half, and all of it: a half given is given
	// whole, so the condition has to be sent again with the
	// changed threshold or it goes.
	got := call(t, by["rule_update"], changing(`{"id":"`+id+`","when":{
		"wake": [{"on":{"source":"battery.level","op":"<","value":25}}],
		"all":  [{"source":"presence","op":"==","value":"here"}]
	},"perform":[
		{"do":"tool","use":"calendar_events","with":{"days":1}},
		{"do":"write","ask":"say how low it is"}
	]}`))
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}

	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("changing it left %d rules, want the one mended", len(held))
	}
	after := held[0]
	if after.Schedule != "when battery.level < 25" {
		t.Errorf("schedule reads back as %q", after.Schedule)
	}
	if after.Name != "low battery" {
		t.Errorf("the name was lost: %q", after.Name)
	}
	// The other half was not named, so it is untouched.
	if len(after.Perform) != 2 {
		t.Fatalf("it has %d acts, want the lookup and write it had: %+v",
			len(after.Perform), after.Perform)
	}
	if after.Perform[0].Use != "calendar_events" {
		t.Errorf("the lookup was lost: %+v", after.Perform[0])
	}
	if after.Perform[1].Ask != "say how low it is" {
		t.Errorf("what it says was lost: %+v", after.Perform[1])
	}
	if said := after.When.Describe(); said != "presence is here" {
		t.Errorf("the condition reads %q", said)
	}
}

// TestChangingToFixedWordsDropsTheQuestion : Words and a question
// were alternatives on the same rule and naming one had to take the
// other away, which was a rule of its own to remember. A sequence
// given whole cannot have both, so there is nothing left to
// remember: what is sent is what it does.
func TestChangingToFixedWordsDropsTheQuestion(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], adding(`{"name":"n","when":{"wake":[{"every":"10m"}]},
		"perform":[{"do":"write","ask":"work it out"}]}`))
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	got := call(t, by["rule_update"], changing(`{"id":"`+id+`",
		"when":{"wake":[{"every":"10m"}]},
		"perform":[{"do":"say","text":"the fixed words"}]}`))
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}
	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	acts := held[0].Perform
	if len(acts) != 1 || acts[0].Do != job.DoSay || acts[0].Text != "the fixed words" {
		t.Errorf("it still works the sentence out: %+v", acts)
	}
}

// TestChangingARuleThatIsNotThere : An identifier from a listing
// somebody remembered rather than made this turn.
func TestChangingARuleThatIsNotThere(t *testing.T) {
	_, by := made()
	got := call(t, by["rule_update"],
		changing(`{"id":"job_01M48E09RJW0G247RBST6712D9","when":{"wake":[{"every":"5m"}]}}`))
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it changed a rule that does not exist: %s", got.Content)
	}
}

// TestARuleCannotBeEditedIntoSomethingItCouldNotHaveBeen : The
// change goes through job.Make, so a rule is read by the same thing
// that reads a new one.
func TestARuleCannotBeEditedIntoSomethingItCouldNotHaveBeen(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], adding(`{"name":"n","when":{"wake":[{"every":"10m"}]},
		"perform":[{"do":"say","text":"hello"}]}`))
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	for _, bad := range []string{
		`{"id":"` + id + `","when":{"wake":[{"every":"whenever I feel like it"}]}}`,
		`{"id":"` + id + `","when":{"wake":[{"every":"10m"}],` +
			`"all":[{"source":"hour","op":">=","value":99}]}}`,
		// Nothing would ever wake it.
		`{"id":"` + id + `","when":{"all":[{"source":"hour","op":">=","value":10}]}}`,
		// An operator that does not exist, which only a structured
		// rule can even express.
		`{"id":"` + id + `","when":{"wake":[{"every":"10m"}],` +
			`"all":[{"source":"hour","op":"nonsense","value":10}]}}`,
		`{"id":"` + id + `","perform":[]}`,
		`{"id":"` + id + `","keep":"a while"}`,
	} {
		if got := call(t, by["rule_update"], changing(bad)); got.Outcome != conversation.OutcomeFailed {
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

	got := call(t, by["rule_add"], adding(`{
		"name": "late and low",
		"when": {"wake":[{"every":"20m"}], "all":[
			{"source":"hour","op":">=","value":22},
			{"source":"battery.level","op":"<","value":20}
		]},
		"perform": [{"do":"say","text":"it is late and the battery is low"}]
	}`))
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("two conditions were refused: %s", got.Content)
	}

	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held) != 1 {
		t.Fatalf("made %d rules", len(held))
	}
	if len(held[0].When.All) != 2 {
		t.Fatalf("it has %d conditions, want both: %+v", len(held[0].When.All), held[0].When.All)
	}
	if said := held[0].When.Describe(); said != "hour >= 22 and battery.level < 20" {
		t.Errorf("the conditions read %q", said)
	}
}

// TestChangingARuleKeepsEveryCondition : parts reads the arguments
// back off the steps, and reading only the last check would quietly
// drop the others the first time anybody changed the words.
//
// The halves are separate now, so changing what a rule says cannot
// reach its conditions at all -- which is the same guarantee got
// structurally rather than by remembering to read them all back.
func TestChangingARuleKeepsEveryCondition(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], adding(`{
		"name": "late and low",
		"when": {"wake":[{"every":"20m"}], "all":[
			{"source":"hour","op":">=","value":22},
			{"source":"battery.level","op":"<","value":20}
		]},
		"perform": [{"do":"say","text":"the old words"}]
	}`))
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	id := held[0].ID

	got := call(t, by["rule_update"], changing(`{"id":"`+id+`",
		"when":{"wake":[{"every":"20m"}], "all":[
			{"source":"hour","op":">=","value":22},
			{"source":"battery.level","op":"<","value":20}
		]},
		"perform":[{"do":"say","text":"the new words"}]}`))
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("could not change it: %s", got.Content)
	}

	held, _ = store.Mine(context.Background(), "usr_1", "rule", 10)
	if len(held[0].When.All) != 2 {
		t.Fatalf("changing the words left %d conditions, want both: %+v",
			len(held[0].When.All), held[0].When.All)
	}
	if held[0].Perform[0].Text != "the new words" {
		t.Errorf("the words did not change: %+v", held[0].Perform)
	}
}

// TestARuleCanBePutAsideAndComeBack : Cancelling was the only way to
// stop a rule and it loses the rule: a week's peace meant remembering
// how it was written in order to have it again.
func TestARuleCanBePutAsideAndComeBack(t *testing.T) {
	store, by := made()
	call(t, by["rule_add"], adding(`{"name":"n","when":{"wake":[{"every":"10m"}]},"perform":[{"do":"say","text":"hello"}]}`))
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
		adding(`{"name":"n","when":{"wake":[{"every":"10m"}]},"perform":[{"do":"say","text":"hello"}],"cool":"1h"}`))
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
		adding(`{"name":"m","when":{"wake":[{"every":"10m"}]},"perform":[{"do":"say","text":"hello"}],"cool":"a while"}`)); got.Outcome != conversation.OutcomeFailed {
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
	}, id, look, nil, silent.try, echoing()) {
		by[one.Name] = one
	}

	got := call(t, by["rule_add"],
		adding(`{"name":"late","when":{"wake":[{"every":"20m"}],"all":[{"source":"hour","op":">=","value":22}]},"perform":[{"do":"say","text":"it is late"}]}`))
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
	}, id, look, nil, spoke.try, echoing()) {
		by2[one.Name] = one
	}
	held, _ := store.Mine(context.Background(), "usr_1", "rule", 10)
	changed := call(t, by2["rule_update"],
		changing(`{"id":"`+held[0].ID+`",
			"when":{"wake":[{"every":"20m"}],"all":[{"source":"hour","op":">=","value":22}]},
			"perform":[{"do":"say","text":"it is late, sir"}]}`))
	if !strings.Contains(changed.Content, `it would say: "it is late, sir"`) {
		t.Errorf("a change was not tried:\n%s", changed.Content)
	}
}

// TestARuleIsStillMadeWithNothingToTryItWith : Nil leaves a rule
// made and unproven, which is how every rule was made before this.
func TestARuleIsStillMadeWithNothingToTryItWith(t *testing.T) {
	_, by := made()
	got := call(t, by["rule_add"], adding(`{"name":"n","when":{"wake":[{"every":"10m"}]},"perform":[{"do":"say","text":"hello"}]}`))
	if got.Outcome == conversation.OutcomeFailed {
		t.Fatalf("a rule was refused for want of a trier: %s", got.Content)
	}
	if strings.Contains(got.Content, "Run just now") {
		t.Errorf("it claimed to have tried one with nothing to try it with:\n%s", got.Content)
	}
}
