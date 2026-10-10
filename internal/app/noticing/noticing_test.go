package noticing_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/memory"
)

func watcher(answer string, err error) noticing.Watcher {
	return noticing.Watcher{
		Ask: func(_ context.Context, _, _ string) (string, error) { return answer, err },
		Describe: func(_ context.Context, _ string) string {
			return "He charged the phone on 8 of 9 nights."
		},
	}
}

// TestNothingIsTheOrdinaryAnswer : most hours contain nothing worth
// being woken for, and a pass that felt obliged to produce something
// would be the whole problem.
func TestNothingIsTheOrdinaryAnswer(t *testing.T) {
	got, err := watcher(`{"nothing": "nothing has moved"}`, nil).
		Wondered(context.Background(), "u", noticing.Useful, nil)
	if err != nil {
		t.Fatalf("wondering: %v", err)
	}
	if got.Asked != "" {
		t.Errorf("it should have asked for nothing, got %q", got.Asked)
	}
}

// TestSomethingWorthHaving : and when there is, it comes back as a
// sentence somebody else can turn into a rule.
func TestSomethingWorthHaving(t *testing.T) {
	got, err := watcher(`{"rule":"tell me if the phone has not charged overnight","name":"the phone did not charge","because":"8 of 9 nights"}`, nil).
		Wondered(context.Background(), "u", noticing.Useful, nil)
	if err != nil {
		t.Fatalf("wondering: %v", err)
	}
	if !strings.Contains(got.Asked, "charged overnight") {
		t.Errorf("asked: %q", got.Asked)
	}
	if got.Name == "" || got.Why == "" {
		t.Errorf("a rule he wrote should carry its name and its reason: %+v", got)
	}
}

// TestAnswerThatCannotBeReadWritesNothing : the safe way for this to
// fail. A missed rule costs one unremarked pattern; a misread one
// runs for ever.
func TestAnswerThatCannotBeReadWritesNothing(t *testing.T) {
	for _, answer := range []string{"I think you should...", "", "{oh dear"} {
		got, err := watcher(answer, nil).Wondered(context.Background(), "u", noticing.Useful, nil)
		if err != nil || got.Asked != "" {
			t.Errorf("%q should have written nothing, got %+v (%v)", answer, got, err)
		}
	}
}

// TestFencedJSONIsRead : what models actually return when asked for
// JSON, and refusing it would be refusing the commonest right answer.
func TestFencedJSONIsRead(t *testing.T) {
	got, _ := watcher("```json\n{\"rule\":\"say when the laptop is unplugged all day\",\"name\":\"n\",\"because\":\"b\"}\n```", nil).
		Wondered(context.Background(), "u", noticing.Useful, nil)
	if !strings.Contains(got.Asked, "unplugged") {
		t.Errorf("fenced JSON should be read: %q", got.Asked)
	}
}

// TestNothingKnownMeansNoRule : nothing is known about them yet, so
// nothing about them is worth a standing rule.
func TestNothingKnownMeansNoRule(t *testing.T) {
	w := watcher(`{"rule":"anything","name":"n","because":"b"}`, nil)
	w.Describe = func(context.Context, string) string { return "   " }
	got, _ := w.Wondered(context.Background(), "u", noticing.Useful, nil)
	if got.Asked != "" {
		t.Errorf("with nothing known it should write nothing, got %q", got.Asked)
	}
}

// TestAModelThatFailsIsReported : unlike an unreadable answer, a
// broken model is a fault and is said so.
func TestAModelThatFailsIsReported(t *testing.T) {
	if _, err := watcher("", errors.New("no model")).Wondered(context.Background(), "u", noticing.Useful, nil); err == nil {
		t.Error("a failing model should be reported")
	}
}

// TestTheAllowance : one a day, and six hours between, taken from the
// rules themselves so a restart does not hand him a fresh allowance.
func TestTheAllowance(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	mine := []job.Job{{Kind: noticing.Kind, Made: now.Add(-2 * time.Hour)}}
	if noticing.Allowed(mine, noticing.Useful, now) {
		t.Error("one written two hours ago should stop another")
	}
	old := []job.Job{{Kind: noticing.Kind, Made: now.Add(-30 * time.Hour)}}
	if !noticing.Allowed(old, noticing.Useful, now) {
		t.Error("yesterday's should not count against today")
	}
	if !noticing.Allowed(nil, noticing.Useful, now) {
		t.Error("having written none, he may write one")
	}
	// Rules the person asked for do not spend his allowance.
	theirs := []job.Job{{Kind: "rule", Made: now.Add(-time.Minute)}}
	if !noticing.Allowed(theirs, noticing.Useful, now) {
		t.Error("their rules are not his")
	}
}

// his : one of his own rules, with what it has done.
func his(id, name string, ageDays, ran, said int) noticing.Spoke {
	return noticing.Spoke{
		Job: job.Job{ID: id, Name: name, Kind: noticing.Kind,
			Why:  noticing.Mark + "a pattern",
			Made: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour)},
		Ran: ran, Said: said,
	}
}

// TestKeepingIsTheOrdinaryAnswer : the mirror of writing. There the
// risk was too many rules; here it is tidying away the one that
// would have mattered next week.
func TestKeepingIsTheOrdinaryAnswer(t *testing.T) {
	id, _, err := watcher(`{"keep":"they all still stand"}`, nil).
		Retired(context.Background(), "u", []noticing.Spoke{his("job_1", "a", 30, 40, 0)})
	if err != nil || id != "" {
		t.Errorf("it should have kept them, got %q (%v)", id, err)
	}
}

// TestARuleTooYoungIsNotReconsidered : written Tuesday and retired
// Wednesday is not changing his mind, it is thrashing.
func TestARuleTooYoungIsNotReconsidered(t *testing.T) {
	id, _, _ := watcher(`{"retire":"job_1","because":"no"}`, nil).
		Retired(context.Background(), "u", []noticing.Spoke{his("job_1", "a", 2, 0, 0)})
	if id != "" {
		t.Errorf("a two-day-old rule should not be up for retirement, got %q", id)
	}
}

// TestTheirRulesAreNotHis : rules the person asked for are theirs to
// keep or drop, and he has no business tidying them away.
func TestTheirRulesAreNotHis(t *testing.T) {
	theirs := noticing.Spoke{Job: job.Job{ID: "job_2", Kind: "rule",
		Made: time.Now().Add(-90 * 24 * time.Hour)}}
	id, _, _ := watcher(`{"retire":"job_2","because":"old"}`, nil).
		Retired(context.Background(), "u", []noticing.Spoke{theirs})
	if id != "" {
		t.Errorf("he should not retire their rule, got %q", id)
	}
}

// TestAnIdHeWasNotShown : a model naming a rule it was not given has
// invented one, and acting on that would let a bad answer delete
// somebody's reminder.
func TestAnIdHeWasNotShown(t *testing.T) {
	id, _, _ := watcher(`{"retire":"job_999","because":"made up"}`, nil).
		Retired(context.Background(), "u", []noticing.Spoke{his("job_1", "a", 30, 10, 0)})
	if id != "" {
		t.Errorf("an id he was never shown should be refused, got %q", id)
	}
}

// TestOneHeWasShownIsRetired : and when it is genuinely his, old
// enough and named correctly, it goes.
func TestOneHeWasShownIsRetired(t *testing.T) {
	id, why, _ := watcher(`{"retire":"job_1","because":"he has stopped cycling"}`, nil).
		Retired(context.Background(), "u", []noticing.Spoke{his("job_1", "the helmet", 30, 60, 0)})
	if id != "job_1" {
		t.Fatalf("it should have retired job_1, got %q", id)
	}
	if why == "" {
		t.Error("retiring one should say why")
	}
}

// kept : one memory, with an age.
func kept(id, subject, body string, ageDays int) memory.Memory {
	return memory.Memory{ID: id, Subject: subject, Body: body,
		CreatedAt: time.Now().Add(-time.Duration(ageDays) * 24 * time.Hour)}
}

// TestNothingHasBeenReplaced : a person is allowed to be more than
// one thing, and most pairs that look contradictory are two facts.
func TestNothingHasBeenReplaced(t *testing.T) {
	id, _, err := watcher(`{"stands":"both are true"}`, nil).Superseded(
		context.Background(), "u", []memory.Memory{
			kept("mem_1", "Favourite song", "Everybody Wants to Rule the World", 10),
			kept("mem_2", "Fireflies", "Fireflies by Owl City is a favourite", 3),
		})
	if err != nil || id != "" {
		t.Errorf("it should have left them alone, got %q (%v)", id, err)
	}
}

// TestTheOlderOneGoes : and where a thing can only have one value,
// the earlier one is the one set aside.
func TestTheOlderOneGoes(t *testing.T) {
	id, why, _ := watcher(`{"supersede":"mem_1","because":"he has moved to Bangalore"}`, nil).
		Superseded(context.Background(), "u", []memory.Memory{
			kept("mem_1", "Location", "Lives in Chennai.", 30),
			kept("mem_2", "Location", "Lives in Bangalore now.", 2),
		})
	if id != "mem_1" {
		t.Fatalf("the older one should go, got %q", id)
	}
	if why == "" {
		t.Error("setting one aside should say what replaced it")
	}
}

// TestOneTooYoungIsNotReconsidered : written this morning and set
// aside this afternoon is thrashing.
func TestOneTooYoungIsNotReconsidered(t *testing.T) {
	id, _, _ := watcher(`{"supersede":"mem_1","because":"no"}`, nil).Superseded(
		context.Background(), "u", []memory.Memory{
			kept("mem_1", "a", "a", 0),
			kept("mem_2", "b", "b", 0),
		})
	if id != "" {
		t.Errorf("a memory kept hours ago should not be up for it, got %q", id)
	}
}

// TestOneAlreadySetAsideIsNotOffered : and is not shown again.
func TestOneAlreadySetAsideIsNotOffered(t *testing.T) {
	gone := kept("mem_1", "Location", "Lives in Chennai.", 30)
	gone.StaleAt = time.Now().Add(-time.Hour)
	id, _, _ := watcher(`{"supersede":"mem_1","because":"again"}`, nil).Superseded(
		context.Background(), "u", []memory.Memory{gone, kept("mem_2", "b", "b", 5)})
	if id != "" {
		t.Errorf("one already set aside should not come back, got %q", id)
	}
}

// TestAMemoryHeWasNotShown : the same guard as the rules. A model
// naming one it was not given has invented it, and acting on that
// would throw away something they asked to keep.
func TestAMemoryHeWasNotShown(t *testing.T) {
	id, _, _ := watcher(`{"supersede":"mem_999","because":"made up"}`, nil).Superseded(
		context.Background(), "u", []memory.Memory{
			kept("mem_1", "a", "a", 10), kept("mem_2", "b", "b", 10),
		})
	if id != "" {
		t.Errorf("an id he was never shown should be refused, got %q", id)
	}
}

// TestWhatEveryRuleHasDoneIsShown : owner, on a rule that announced
// the laptop's ordinary night -- *"the model should look at the
// current rules and how useful they are... it has to question
// itself -- is that rule useful?"*
//
// It cannot ask that of a list of names. The record is the answer:
// ran so many times, spoke so few, and this is what it last said.
func TestWhatEveryRuleHasDoneIsShown(t *testing.T) {
	theirs := noticing.Spoke{
		Job: job.Job{ID: "job_2", Name: "a device has gone quiet",
			Kind: "rule", Schedule: "every 5m",
			Said: "Your laptop has gone quiet, sir."},
		Ran: 529, Said: 6,
	}

	var asked string
	w := noticing.Watcher{
		Ask: func(_ context.Context, _, ask string) (string, error) {
			asked = ask
			return `{"nothing":"no"}`, nil
		},
		Describe: func(context.Context, string) string { return "He charged it on 8 of 9 nights." },
	}
	if _, err := w.Wondered(context.Background(), "u", noticing.Useful,
		[]noticing.Spoke{theirs, his("job_1", "the helmet is still on", 30, 3, 0)}); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"a device has gone quiet", "every 5m",
		"ran 529 times", "said something 6 times",
		"Your laptop has gone quiet",
		"they asked for this", "you wrote this yourself",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the judge was not shown %q:\n%s", want, asked)
		}
	}
}

// TestTheJudgeIsGivenNoIDs : the list read while deciding what to
// add carries no ids, so nothing on it can be named. Taking a rule
// back is a different pass with a different prompt, and it is shown
// ids only for his own.
func TestTheJudgeIsGivenNoIDs(t *testing.T) {
	var asked string
	w := noticing.Watcher{
		Ask: func(_ context.Context, _, ask string) (string, error) {
			asked = ask
			return `{"nothing":"no"}`, nil
		},
		Describe: func(context.Context, string) string { return "He charged it on 8 of 9 nights." },
	}
	if _, err := w.Wondered(context.Background(), "u", noticing.Useful,
		[]noticing.Spoke{his("job_1", "the helmet is still on", 30, 3, 0)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(asked, "job_1") {
		t.Errorf("the judge deciding what to add was handed a rule's id:\n%s", asked)
	}
}

// TestTheirRulesAreContextWhenRetiring : he may not retire one of
// theirs, but he is shown them. Whether one of his own still earns
// its place is a question about everything that is watching.
func TestTheirRulesAreContextWhenRetiring(t *testing.T) {
	theirs := noticing.Spoke{
		Job: job.Job{ID: "job_2", Name: "battery at 20", Kind: "rule",
			Made: time.Now().Add(-90 * 24 * time.Hour)},
		Ran: 3, Said: 1,
	}

	var asked string
	w := noticing.Watcher{
		Ask: func(_ context.Context, _, ask string) (string, error) {
			asked = ask
			return `{"keep":"all of them"}`, nil
		},
		Describe: func(context.Context, string) string { return "He charged it on 8 of 9 nights." },
	}
	if _, _, err := w.Retired(context.Background(), "u",
		[]noticing.Spoke{theirs, his("job_1", "the helmet is still on", 30, 3, 0)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "battery at 20") {
		t.Errorf("their rules were not shown as context:\n%s", asked)
	}
	if strings.Contains(asked, "job_2") {
		t.Errorf("he was handed the id of a rule he may not retire:\n%s", asked)
	}
	if !strings.Contains(asked, "job_1") {
		t.Errorf("he was not given the id of his own rule:\n%s", asked)
	}
}
