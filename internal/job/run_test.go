package job_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
	"github.com/DhanushRamesh/personal-assistant/internal/job/inmemory"
)

var india = time.FixedZone("IST", 5*3600+1800)

// noon : The moment the clock is held at.
var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, india)

// hands : A Doer that records what it was asked to do.
type hands struct {
	did     []string
	saw     []int
	heard   [][]string
	carried [][]job.Ran
	before  []string
	says    string
	got     map[string]string
	stopAt  string
	failAt  string
	failure error
}

func (h *hands) Do(_ context.Context, run job.Turn, s job.Step) (job.Outcome, error) {
	h.saw = append(h.saw, len(run.Already))
	h.heard = append(h.heard, job.Tools(run.Already))
	h.carried = append(h.carried, run.Already)
	h.before = append(h.before, run.Before)
	what := string(s.Do) + ":" + s.Say + s.Tool + s.When
	h.did = append(h.did, what)
	if h.failAt != "" && strings.Contains(what, h.failAt) {
		return job.Outcome{}, h.failure
	}
	if h.stopAt != "" && strings.Contains(what, h.stopAt) {
		return job.Outcome{Stop: true, Why: "the check " + what + " did not pass"}, nil
	}
	return job.Outcome{Got: h.got[s.Tool], Said: h.says}, nil
}

// runner : A runner over a store, with the clock held still.
func runner(s *inmemory.Store, h *hands, now time.Time) *job.Runner {
	return &job.Runner{Store: s, Doer: h, Where: india, Now: func() time.Time { return now }}
}

// put : A job due now.
func put(t *testing.T, s *inmemory.Store, schedule string, steps ...job.Step) job.Job {
	t.Helper()
	j, err := job.New("job_1", "usr_1", "a job", "rule", schedule, steps, noon.Add(-time.Minute))
	if err != nil {
		t.Fatalf("job.New: %v", err)
	}
	j.Next = noon
	s.Add(j)
	return j
}

// Owner: *"not just a single task, a sequence of tasks can be
// done."* A reminder is already two of them -- say the words, set
// the alarm -- written into a function. Here it is a row.
func TestASequenceRunsInOrder(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "at 06:40",
		job.Step{Do: job.Announce, Say: "take the tablets"},
		job.Step{Do: job.Call, Tool: "alarm_set"})

	if got := runner(s, h, noon).Once(context.Background()); got.Ran != 1 {
		t.Fatalf("pass = %+v, want it run", got)
	}
	if len(h.did) != 2 || h.did[0] != "announce:take the tablets" || h.did[1] != "tool:alarm_set" {
		t.Errorf("the sequence ran as %v", h.did)
	}
}

// A step may stop the ones after it. That is the whole of the
// conditional in this engine: "only if the battery is low" is a
// step that checks and stops, not an if.
func TestAStepCanStopTheRest(t *testing.T) {
	s, h := inmemory.New(), &hands{stopAt: "battery"}
	put(t, s, "every 5m",
		job.Step{Do: job.Call, Tool: "battery"},
		job.Step{Do: job.Announce, Say: "you are low"})

	if got := runner(s, h, noon).Once(context.Background()); got.Stopped != 1 {
		t.Fatalf("pass = %+v, want the rest stopped", got)
	}
	if len(h.did) != 1 {
		t.Errorf("it carried on past the stop: %v", h.did)
	}
}

// A failing step is not a stop. A phone alarm that could not be
// set must not swallow the words that go with it -- the rule the
// reminder and its alarm already follow.
func TestAFailedStepDoesNotSwallowTheRest(t *testing.T) {
	s := inmemory.New()
	h := &hands{failAt: "alarm", failure: errors.New("the phone is asleep")}
	put(t, s, "at 06:40",
		job.Step{Do: job.Call, Tool: "alarm"},
		job.Step{Do: job.Announce, Say: "take the tablets"})

	got := runner(s, h, noon).Once(context.Background())
	if got.Failed != 1 || got.Ran != 1 {
		t.Fatalf("pass = %+v, want one failure and the run finished", got)
	}
	if len(h.did) != 2 {
		t.Errorf("the words were lost with the alarm: %v", h.did)
	}
	after, _ := s.Get("job_1")
	if after.Why == "" {
		t.Error("nothing records what went wrong")
	}
}

// One run leaves one record, however many of its steps failed.
func TestOneRunIsRecordedOnce(t *testing.T) {
	s := inmemory.New()
	h := &hands{failAt: "tool", failure: errors.New("no")}
	put(t, s, "every 5m",
		job.Step{Do: job.Call, Tool: "one"},
		job.Step{Do: job.Call, Tool: "two"})

	runner(s, h, noon).Once(context.Background())
	after, _ := s.Get("job_1")
	if !after.Next.Equal(noon.Add(5 * time.Minute)) {
		t.Errorf("it next runs at %s, not five minutes on", after.Next)
	}
}

// A server that was off over the moment comes back owing runs, and
// yesterday's eight o'clock done at noon is worse than skipped.
// The same judgement the outbox makes, for the same reason.
func TestAMomentLongPassedIsSkipped(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "at 08:00", job.Step{Do: job.Announce, Say: "good morning"})

	// Four hours after it was due.
	got := runner(s, h, noon.Add(4*time.Hour)).Once(context.Background())
	if got.Skipped != 1 {
		t.Fatalf("pass = %+v, want it skipped", got)
	}
	if len(h.did) != 0 {
		t.Errorf("a stale job ran anyway: %v", h.did)
	}
	after, _ := s.Get("job_1")
	if after.State != job.Active {
		t.Errorf("a daily job ended rather than waiting for tomorrow: %q", after.State)
	}
}

// A one-off is finished when it has run; a repeat comes round.
func TestAOneOffIsDoneAndARepeatIsNot(t *testing.T) {
	for _, c := range []struct {
		schedule string
		want     job.State
	}{
		{"once 2026-10-06 12:00", job.Done},
		{"every 10m", job.Active},
		{"at 06:40", job.Active},
	} {
		t.Run(c.schedule, func(t *testing.T) {
			s, h := inmemory.New(), &hands{}
			put(t, s, c.schedule, job.Step{Do: job.Announce, Say: "hello"})

			runner(s, h, noon).Once(context.Background())
			after, _ := s.Get("job_1")
			if after.State != c.want {
				t.Errorf("state = %q, want %q", after.State, c.want)
			}
		})
	}
}

// A clock time means that hour where they are standing, and
// tomorrow if it has already gone.
func TestAClockTimeIsInTheirOwnZone(t *testing.T) {
	next, err := job.Following("at 06:40", noon, india)
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if next.Hour() != 6 || next.Minute() != 40 {
		t.Errorf("next run is %02d:%02d", next.Hour(), next.Minute())
	}
	if next.Day() != 7 {
		t.Errorf("six forty had gone, so it should be tomorrow: %s", next)
	}
}

// A rule that cannot work should fail in front of whoever wrote
// it, not silently at three in the morning.
func TestAnUnworkableJobIsRefused(t *testing.T) {
	ok := []job.Step{{Do: job.Announce, Say: "hello"}}
	for _, c := range []struct {
		why      string
		schedule string
		steps    []job.Step
	}{
		{"no schedule", "", ok},
		{"nothing to do", "every 5m", nil},
		{"a schedule nobody can read", "sometimes", ok},
		{"an hour that is not one", "at 99:99", ok},
	} {
		if _, err := job.New("j", "u", "n", "rule", c.schedule, c.steps, noon); err == nil {
			t.Errorf("%s was accepted", c.why)
		}
	}
	if err := (job.Step{Do: job.Announce}).Valid(); err == nil {
		t.Error("an announcement with nothing to say was accepted")
	}
	if err := (job.Step{Do: "dance"}).Valid(); err == nil {
		t.Error("a step nobody can carry out was accepted")
	}
}

// A sequence is one turn. The tools refuse to write something they
// have not read in this turn, so a step has to know what the steps
// before it ran -- which is what makes [list, add] mean anything,
// and the reason sequences exist at all.
func TestAStepKnowsWhatTheSequenceAlreadyRan(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "every 5m",
		job.Step{Do: job.Call, Tool: "task_list"},
		job.Step{Do: job.Call, Tool: "task_add"})

	runner(s, h, noon).Once(context.Background())
	if len(h.saw) != 2 {
		t.Fatalf("ran %d steps", len(h.saw))
	}
	if h.saw[0] != 0 || h.saw[1] != 1 {
		t.Errorf("the second step saw %d earlier tools, wanted 1", h.saw[1])
	}
}

// A rule that notices a thing notices it again on the next run,
// and the one after, for as long as the thing stays true. What
// stops it is being told what it said last time -- the sentence
// is the key, so nothing has to identify the occasion.
func TestARuleIsToldWhatItSaidLastTime(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 5m",
		[]job.Step{{Do: job.Write, Ask: "anything worth saying?"}}, noon)
	if err != nil {
		t.Fatalf("job.New: %v", err)
	}
	j.Next, j.Said = noon, "The phone has gone quiet, sir."
	s.Add(j)

	runner(s, h, noon).Once(context.Background())
	if len(h.before) != 1 {
		t.Fatalf("ran %d steps", len(h.before))
	}
	if h.before[0] != "The phone has gone quiet, sir." {
		t.Errorf("the step was told %q", h.before[0])
	}
}

// What a run said is kept, so the next one can be compared
// against it.
func TestWhatWasSaidIsRemembered(t *testing.T) {
	s := inmemory.New()
	h := &hands{says: "The phone has gone quiet, sir."}
	put(t, s, "every 5m", job.Step{Do: job.Write, Ask: "anything?"})

	runner(s, h, noon).Once(context.Background())
	after, _ := s.Get("job_1")
	if after.Said != "The phone has gone quiet, sir." {
		t.Errorf("it remembered %q", after.Said)
	}
}

// And forgotten when a run says nothing, so a thing that goes
// away and comes back is announced again. Without this a phone
// quiet in the morning could never be reported quiet at night.
func TestSayingNothingForgetsWhatWasSaid(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 5m",
		[]job.Step{{Do: job.Check, When: "battery.level < 20"}}, noon)
	if err != nil {
		t.Fatalf("job.New: %v", err)
	}
	j.Next, j.Said = noon, "The phone has gone quiet, sir."
	s.Add(j)

	// The check stops it, so nothing is said.
	h.stopAt = "check"
	runner(s, h, noon).Once(context.Background())

	after, _ := s.Get("job_1")
	if after.Said != "" {
		t.Errorf("it still remembers %q after saying nothing", after.Said)
	}
}

// A moment that passed while nothing was running leaves the
// memory alone: nothing was said, and nothing was decided against
// saying either.
func TestASkippedRunForgetsNothing(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "at 08:00",
		[]job.Step{{Do: job.Write, Ask: "anything?"}}, noon)
	if err != nil {
		t.Fatalf("job.New: %v", err)
	}
	j.Next, j.Said = noon, "Something, sir."
	s.Add(j)

	runner(s, h, noon.Add(4*time.Hour)).Once(context.Background())
	after, _ := s.Get("job_1")
	if after.Said != "Something, sir." {
		t.Errorf("a skipped run forgot %q", after.Said)
	}
}

// TestASilentRunIsNotRecordedAsHavingSpoken : The record of what the
// assistant considered saying logged "said" thirty times an hour for
// a rule that had been silent all afternoon, quoting a sentence from
// hours earlier.
//
// A step that stops the sequence hands back what this rule said last
// time, so the memory that stops it repeating itself survives a run
// that found nothing. That is a thing to carry and never a thing
// that was spoken, and one variable was doing both.
func TestASilentRunIsNotRecordedAsHavingSpoken(t *testing.T) {
	s := inmemory.New()
	j, err := job.New("job_1", "usr_1", "anything amiss", "rule", "every 2m",
		[]job.Step{
			{Do: job.Call, Tool: "situation_find"},
			{Do: job.Write, Ask: "say what was found"},
		}, noon)
	if err != nil {
		t.Fatal(err)
	}
	// It spoke an hour ago, and is still holding that sentence.
	j.Said = "Someone the phone could not name has tried to reach you."
	j.Next = noon
	s.Add(j)

	kept := &journal{}
	running := &job.Runner{
		Store: s, Doer: stops{}, Where: india, Journal: kept,
		Now: func() time.Time { return noon },
	}
	running.Once(context.Background())

	if len(kept.runs) != 1 {
		t.Fatalf("recorded %d runs, want one", len(kept.runs))
	}
	got := kept.runs[0]
	if got.Said != "" {
		t.Errorf("a silent run was written down as having said %q", got.Said)
	}
	if !got.Stopped {
		t.Error("the run was not recorded as having stopped")
	}

	// And the memory still survives, which is what the carrying is
	// for: the rule must not say that sentence again when the thing
	// comes back.
	after, _ := s.Get("job_1")
	if after.Said != j.Said {
		t.Errorf("it forgot what it had said: %q", after.Said)
	}
}

// stops : A doer whose first step finds nothing and carries the
// memory forward, as the real tool and write steps do.
type stops struct{}

func (stops) Do(_ context.Context, run job.Turn, _ job.Step) (job.Outcome, error) {
	return job.Outcome{Stop: true, Why: "situation_find found nothing", Said: run.Before}, nil
}

// journal : Keeps what was written down.
type journal struct{ runs []job.Record }

func (j *journal) Wrote(_ context.Context, r job.Record) error {
	j.runs = append(j.runs, r)
	return nil
}

// TestATrialSaysWhatItWouldSayAndSaysNothing : A rule could be made
// and then nothing done but wait for the world to set it off. That is
// how a rule that was wrong stayed wrong for a day, and why a rule
// written in the evening could not be shown to work at all.
//
// The same steps through the same code against the same readings,
// because a trial down a different path would be testing the path.
// Only the announcing is held back.
func TestATrialSaysWhatItWouldSayAndSaysNothing(t *testing.T) {
	s := inmemory.New()
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 10m",
		[]job.Step{
			{Do: job.Call, Tool: "event_recent"},
			{Do: job.Announce, Say: "the battery is low"},
		}, noon)
	if err != nil {
		t.Fatal(err)
	}
	j.Next = noon
	s.Add(j)

	kept := &journal{}
	running := &job.Runner{
		Store: s, Doer: &hands{}, Where: india, Journal: kept,
		Now: func() time.Time { return noon },
	}
	tried := running.Try(context.Background(), j)

	if len(tried.Steps) != 2 {
		t.Fatalf("it ran %d steps, want both", len(tried.Steps))
	}
	if tried.Stopped {
		t.Error("it stopped, and nothing in it stops")
	}

	// Nothing written down: not the run, not what it said, not when
	// it next runs. A trial that moved the rule on would be a trial
	// that changed the thing being tried.
	if len(kept.runs) != 0 {
		t.Errorf("a trial was written into the record: %+v", kept.runs)
	}
	after, _ := s.Get("job_1")
	if after.Said != "" {
		t.Errorf("a trial was remembered as something said: %q", after.Said)
	}
	if !after.Last.IsZero() {
		t.Error("a trial was recorded as a run")
	}
	if !after.Next.Equal(noon) {
		t.Errorf("a trial moved its next run to %s", after.Next)
	}
}

// TestATrialIsToldWhatTheRuleLastSaid : So it answers the question
// actually being asked -- would this speak, now, as things stand. A
// rule holding a sentence it has already said will say so, which is
// the truth and is worth seeing.
func TestATrialIsToldWhatTheRuleLastSaid(t *testing.T) {
	s := inmemory.New()
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 10m",
		[]job.Step{{Do: job.Write, Ask: "say what is amiss"}}, noon)
	if err != nil {
		t.Fatal(err)
	}
	j.Said = "it was already said"
	s.Add(j)

	seen := &remembers{}
	running := &job.Runner{
		Store: s, Doer: seen, Where: india,
		Now: func() time.Time { return noon },
	}
	running.Try(context.Background(), j)

	if seen.before != "it was already said" {
		t.Errorf("the trial was told %q was said last time", seen.before)
	}
	if !seen.trying {
		t.Error("the step was not told this was a trial, so it would have spoken")
	}
}

// remembers : A doer that keeps what it was told about the turn.
type remembers struct {
	before string
	trying bool
}

func (r *remembers) Do(_ context.Context, run job.Turn, _ job.Step) (job.Outcome, error) {
	r.before, r.trying = run.Before, run.Trying
	return job.Outcome{}, nil
}

// TestACheckThatCannotBeReadStopsTheRule : A step that fails does not
// stop the sequence, deliberately -- a phone alarm that could not be
// set must not swallow the words that go with it.
//
// A check is the opposite. Its whole purpose is to withhold, so a
// broken one must withhold. It did not: a probe with three
// conditions, the third unreadable, said its words aloud in the room.
// The step meant to stop it failed, and failing was taken for
// permission.
func TestACheckThatCannotBeReadStopsTheRule(t *testing.T) {
	s := inmemory.New()
	j := job.Job{
		ID: "job_1", UserID: "usr_1", Name: "a rule", Kind: "rule",
		Schedule: "every 10m", State: job.Active, Next: noon,
		Steps: []job.Step{
			{Do: job.Check, When: "hour >= 99"},
			{Do: job.Announce, Say: "it should never say this"},
		},
	}
	s.Add(j)

	h := &hands{}
	runner(s, h, noon).Once(context.Background())

	for _, did := range h.did {
		if did == string(job.Announce) {
			t.Fatal("the rule spoke although its condition could not be read")
		}
	}
	after, _ := s.Get("job_1")
	if after.Said != "" {
		t.Errorf("it remembered saying %q", after.Said)
	}
}
