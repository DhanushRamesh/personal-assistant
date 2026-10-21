package job_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/job/inmemory"
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
	// withhold : What the conditions answer, where a test is about
	// a rule that does not hold. Empty is a rule that does.
	withhold string
	failure  error
}

func (h *hands) Holds(_ context.Context, _ string, _ job.Occasion) (bool, string, error) {
	if h.withhold != "" {
		h.did = append(h.did, "when:"+h.withhold)
		return false, h.withhold, nil
	}
	return true, "", nil
}

func (h *hands) Do(_ context.Context, run job.Turn, s job.Act) (job.Outcome, error) {
	h.saw = append(h.saw, len(run.Already))
	h.heard = append(h.heard, job.Tools(run.Already))
	h.carried = append(h.carried, run.Already)
	h.before = append(h.before, run.Before)
	what := s.Do + ":" + s.Text + s.Use
	h.did = append(h.did, what)
	if h.failAt != "" && strings.Contains(what, h.failAt) {
		return job.Outcome{}, h.failure
	}
	if h.stopAt != "" && strings.Contains(what, h.stopAt) {
		return job.Outcome{Stop: true, Why: what + " found nothing"}, nil
	}
	return job.Outcome{Got: h.got[s.Use], Said: h.says}, nil
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
	if len(h.did) != 2 || h.did[0] != "say:take the tablets" || h.did[1] != "tool:alarm_set" {
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

func (stops) Holds(_ context.Context, _ string, _ job.Occasion) (bool, string, error) {
	return true, "", nil
}

func (stops) Do(_ context.Context, run job.Turn, _ job.Act) (job.Outcome, error) {
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

	if len(tried.Acts) != 2 {
		t.Fatalf("it ran %d steps, want both", len(tried.Acts))
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

func (r *remembers) Holds(_ context.Context, _ string, _ job.Occasion) (bool, string, error) {
	return true, "", nil
}

func (r *remembers) Do(_ context.Context, run job.Turn, _ job.Act) (job.Outcome, error) {
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

// TestALookupThatFailedStopsTheSequence : The other half of the
// rule that a broken condition withholds.
//
// A check that fails leaves the steps after it unguarded, and that
// was fixed. A lookup that fails leaves them *uninformed*, which
// looks like nothing at all: the write step still has its
// instruction, still has a model, and still produces a sentence --
// composed from a picture with a hole in it that the sentence will
// not mention.
func TestALookupThatFailedStopsTheSequence(t *testing.T) {
	s, h := inmemory.New(), &hands{
		failAt:  "event_recent",
		failure: fmt.Errorf("%w: event_recent: the database is away", job.ErrBlind),
		says:    "The battery is at four per cent, sir.",
	}
	put(t, s, "every 10m",
		job.Step{Do: job.Call, Tool: "event_recent"},
		job.Step{Do: job.Write, Ask: "say how the battery is"})

	pass := runner(s, h, noon).Once(context.Background())

	if got := h.did; len(got) != 1 {
		t.Errorf("went on after a lookup it needed failed: ran %v", got)
	}
	if pass.Stopped != 1 {
		t.Errorf("did not count the run as stopped: %+v", pass)
	}
	if pass.Failed != 1 {
		t.Errorf("did not count the failure: %+v", pass)
	}
}

// TestAnActionThatFailedDoesNotStopTheSequence : The older rule,
// and still right. A reminder whose phone alarm could not be set
// must still say its words aloud: nothing after it was relying on
// what it returned, and swallowing the sentence over it would lose
// the half that mattered.
func TestAnActionThatFailedDoesNotStopTheSequence(t *testing.T) {
	s, h := inmemory.New(), &hands{
		failAt:  "reminder_set",
		failure: errors.New("reminder_set: the phone did not answer"),
		says:    "You asked to be reminded, sir.",
	}
	put(t, s, "every 10m",
		job.Step{Do: job.Call, Tool: "reminder_set"},
		job.Step{Do: job.Write, Ask: "say the words"})

	pass := runner(s, h, noon).Once(context.Background())

	if got := h.did; len(got) != 2 {
		t.Errorf("an alarm that could not be set swallowed the words: ran %v", got)
	}
	if pass.Stopped != 0 {
		t.Errorf("stopped over something nothing was relying on: %+v", pass)
	}
	if pass.Failed != 1 {
		t.Errorf("did not count the failure: %+v", pass)
	}
}

// writes : A journal that keeps what it was given.
type writes struct{ got []job.Record }

func (w *writes) Wrote(_ context.Context, r job.Record) error {
	w.got = append(w.got, r)
	return nil
}

// TestARunWritesDownWhatEachStepDid : Sixteen rules are about to be
// converted to another shape, and the only way to say whether a
// converted one still does what it did is to have what it did.
//
// The id matters more than the name: a rule being renamed or
// rewritten is exactly the occasion this is for, and a name is the
// one thing that will not survive it.
func TestARunWritesDownWhatEachStepDid(t *testing.T) {
	s, h := inmemory.New(), &hands{
		withhold: "the condition battery.level >= 5 did not hold", says: "never said"}
	j := put(t, s, "every 10m",
		job.Step{Do: job.Call, Tool: "event_recent"},
		job.Step{Do: job.Check, When: "battery.level >= 5"},
		job.Step{Do: job.Write, Ask: "say how the battery is"})

	kept := &writes{}
	r := runner(s, h, noon)
	r.Journal = kept
	r.Once(context.Background())

	if len(kept.got) != 1 {
		t.Fatalf("wrote %d records, want 1", len(kept.got))
	}
	rec := kept.got[0]
	if rec.JobID != j.ID {
		t.Errorf("recorded job %q, want %q", rec.JobID, j.ID)
	}
	if rec.Schedule != "every 10m" {
		t.Errorf("did not keep the schedule as it was: %q", rec.Schedule)
	}

	// The step that ended it, and every step before it. A record
	// of only the last one could not say what had already been
	// done when it stopped.
	// The conditions are answered before any step, so the record
	// begins with them and the steps that never ran are not in it.
	if len(rec.Steps) != 1 {
		t.Fatalf("recorded %d steps, want only the conditions: %+v", len(rec.Steps), rec.Steps)
	}
	if rec.Steps[0].Do != "when" || rec.Steps[0].Came != job.Halted {
		t.Errorf("the conditions recorded as %+v", rec.Steps[0])
	}
	if !strings.Contains(rec.Steps[0].When, "battery.level >= 5") {
		t.Errorf("did not keep which condition stopped it: %q", rec.Steps[0].When)
	}
	if rec.Steps[0].Why == "" {
		t.Error("did not say why the conditions did not hold")
	}
	// Nothing was fetched, which is the point of asking first: the
	// old shape ran the lookup and then found it had nothing to
	// say, and one live rule spent 1255ms a time doing it.
	for _, st := range rec.Steps {
		if st.Do == job.Call || st.Do == job.Write {
			t.Errorf("recorded a step that should never have run: %+v", st)
		}
	}
}

// TestAFailedStepIsRecordedAsFailed : Told apart from a step that
// ran and from one that withheld, because the three are three
// different things to read on a page.
func TestAFailedStepIsRecordedAsFailed(t *testing.T) {
	s, h := inmemory.New(), &hands{
		failAt:  "reminder_set",
		failure: errors.New("the phone did not answer"),
		says:    "You asked to be reminded, sir.",
	}
	put(t, s, "every 10m",
		job.Step{Do: job.Call, Tool: "reminder_set"},
		job.Step{Do: job.Write, Ask: "say the words"})

	kept := &writes{}
	r := runner(s, h, noon)
	r.Journal = kept
	r.Once(context.Background())

	rec := kept.got[0]
	if len(rec.Steps) != 2 {
		t.Fatalf("recorded %d steps, want 2", len(rec.Steps))
	}
	if rec.Steps[0].Came != job.Errored || rec.Steps[0].Why == "" {
		t.Errorf("the failure recorded as %+v", rec.Steps[0])
	}
	// An act that failed does not stop the sequence, so the step
	// after it ran and the record has to show that it did.
	if rec.Steps[1].Came != job.Went {
		t.Errorf("the step after a failed act recorded as %+v", rec.Steps[1])
	}
}

// judges : A doer that can say what each condition answered.
type judges struct{ hands }

func (judges) Judge(_ context.Context, _ string, when job.Occasion) (job.Verdict, string, error) {
	return job.Verdict{Held: false, Parts: []job.Verdict{{Held: true}, {Held: false}}}, "the condition battery.level < 25 did not hold", nil
}

// TestATrialMarksEachCondition : Tried by a doer that can say, a rule's
// trial carries what every condition answered, and stops on them as a
// run would.
func TestATrialMarksEachCondition(t *testing.T) {
	j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 10m",
		[]job.Step{{Do: job.Announce, Say: "the battery is low"}}, noon)
	if err != nil {
		t.Fatal(err)
	}
	j.When.All = []job.Where{{Source: "hour", Op: "<", Value: 18}, {Source: "battery.level", Op: "<", Value: 25}}
	running := &job.Runner{Store: inmemory.New(), Doer: &judges{}, Where: india, Now: func() time.Time { return noon }}
	tried := running.Try(context.Background(), j)
	if tried.When == nil || tried.When.Held || len(tried.When.Parts) != 2 || !tried.When.Parts[0].Held {
		t.Fatalf("when = %+v", tried.When)
	}
	if !strings.Contains(tried.Why, "battery.level") || len(tried.Acts) != 0 {
		t.Errorf("why %q, %d acts: want it stopped on the battery before any step", tried.Why, len(tried.Acts))
	}
}

// A rule that waits for something and then ends.
//
// Owner, 21 October 2026, on using this to manage his spaces and
// terminals: *"here accuracy is what matters, not the latency .. the
// job has to be done and I should be reliable [on it]"*. Work that
// waits on a terminal is a rule that comes round every minute and
// stops at a check until the answer is there. That costs nothing
// while it waits, and before this it never ended.
//
// A finish past the check is reached only on the pass where the work
// happened. No branch: a step may stop the ones after it, and this
// one is simply the last.
func TestAFinishEndsARepeatingRule(t *testing.T) {
	// Waiting: the answer is not there, so the check stops the rest
	// and the finish is never reached.
	waiting, h := inmemory.New(), &hands{withhold: "no answer yet"}
	put(t, waiting, "every 1m",
		job.Step{Do: job.Check, When: "terminal.finished within 2m"},
		job.Step{Do: job.Write, Ask: "compare it with the notes"},
		job.Step{Do: job.Finish},
	)
	if got := runner(waiting, h, noon).Once(context.Background()); got.Stopped != 1 {
		t.Fatalf("pass = %+v, want it stopped at the check", got)
	}
	held, ok := waiting.Get("job_1")
	if !ok {
		t.Fatal("the rule is gone")
	}
	if held.State != job.Active {
		t.Errorf("state = %q, want it still waiting", held.State)
	}
	if held.Next.IsZero() {
		t.Error("a waiting rule has no next time, so it would never come round again")
	}

	// Answered: the check holds, the work runs, and the finish ends
	// it although its schedule says every minute.
	done, g := inmemory.New(), &hands{}
	put(t, done, "every 1m",
		job.Step{Do: job.Check, When: "terminal.finished within 2m"},
		job.Step{Do: job.Write, Ask: "compare it with the notes"},
		job.Step{Do: job.Finish},
	)
	if got := runner(done, g, noon).Once(context.Background()); got.Ran != 1 {
		t.Fatalf("pass = %+v, want it run", got)
	}
	over, ok := done.Get("job_1")
	if !ok {
		t.Fatal("the rule is gone")
	}
	if over.State != job.Done {
		t.Errorf("state = %q, want it over", over.State)
	}
	if !over.Next.IsZero() {
		t.Errorf("a finished rule is due again at %v", over.Next)
	}
	// The work happened before it ended. The harness records a step
	// by its kind, and the finish is not one it is asked to carry
	// out -- reaching it is the whole instruction.
	if len(g.did) != 1 || !strings.HasPrefix(g.did[0], "write") {
		t.Errorf("it ended without doing the work, or asked somebody to do the finish: %v", g.did)
	}
}

// A rule that stops and asks before it acts.
//
// Owner, 21 October 2026, asked which of the agent principles to build
// first: *"approval first"*. A rule that types into his terminals can
// do real work unwatched, and nothing in the engine could stop and ask
// him before it did.
//
// No timeout. A rule that goes ahead when nobody replied is one that
// did not ask, so it has no next time at all and the clock passes over
// it until he answers.
func TestARuleStopsToBeAllowed(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "every 5m",
		job.Step{Do: job.Call, Tool: "read_terminal"},
		job.Step{Do: job.Ask, Say: "Shall I tell the Laptop client to carry on, sir?"},
		job.Step{Do: job.Call, Tool: "type_into_terminal"},
	)

	runner(s, h, noon).Once(context.Background())

	// It read, it asked, and it did not type. The asking is said --
	// he has to hear what he is allowing -- and the step after it is
	// the one that waits.
	if len(h.did) != 2 || h.did[0] != "tool:read_terminal" ||
		!strings.HasPrefix(h.did[1], "say:Shall I") {
		t.Fatalf("the sequence ran as %v, want it read, asked, and stopped", h.did)
	}

	held, ok := s.Get("job_1")
	if !ok {
		t.Fatal("the rule is gone")
	}
	if held.State != job.Asking {
		t.Errorf("state = %q, want it waiting to be allowed", held.State)
	}
	if !held.Next.IsZero() {
		t.Errorf("a rule waiting on him comes round on its own at %v", held.Next)
	}
	// Where to carry on from, once he says yes.
	if held.Resume != 2 {
		t.Errorf("resume = %d, want the step after the asking", held.Resume)
	}

	// And the clock passes over it, however long it waits.
	later := runner(s, h, noon.Add(time.Hour)).Once(context.Background())
	if later.Ran != 0 {
		t.Errorf("a rule waiting to be allowed ran anyway: %+v", later)
	}
	if len(h.did) != 2 {
		t.Errorf("it went on without being allowed: %v", h.did)
	}
}

// An approval with nothing after it asks about nothing.
func TestAnApprovalAsksAboutWhatFollows(t *testing.T) {
	if _, err := job.New("job_1", "usr_1", "a job", "rule", "every 5m",
		[]job.Step{
			job.Step{Do: job.Call, Tool: "read_terminal"},
			job.Step{Do: job.Ask, Say: "Shall I?"},
		}, noon); err == nil {
		t.Error("an approval with nothing after it was allowed")
	}
	if _, err := job.New("job_2", "usr_1", "a job", "rule", "every 5m",
		[]job.Step{
			job.Step{Do: job.Ask, Say: ""},
			job.Step{Do: job.Call, Tool: "type_into_terminal"},
		}, noon); err == nil {
		t.Error("an approval that does not say what it would do was allowed")
	}
}

// And carries on where it stopped, once he allows it.
//
// The whole point of asking: a rule that stops and can never be
// answered is worse than one that never stopped. The answer arrives
// as a resume rather than a clock -- he says yes, the rule is made due
// again, and the pass after that runs the steps it had not reached.
func TestAnAllowedRuleCarriesOnWhereItStopped(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "every 5m",
		job.Step{Do: job.Call, Tool: "read_terminal"},
		job.Step{Do: job.Ask, Say: "Shall I tell the Laptop client to carry on, sir?"},
		job.Step{Do: job.Call, Tool: "type_into_terminal"},
		job.Step{Do: job.Announce, Say: "Told it, sir."},
	)
	runner(s, h, noon).Once(context.Background())

	allowed, err := s.Allow(context.Background(), "usr_1", "job_1", noon.Add(time.Minute))
	if err != nil || !allowed {
		t.Fatalf("Allow = %v, %v", allowed, err)
	}

	// The pass after he allowed it runs what it had not reached, and
	// does not do again what it had.
	if got := runner(s, h, noon.Add(time.Minute)).Once(context.Background()); got.Ran != 1 {
		t.Fatalf("pass = %+v, want it run", got)
	}
	want := []string{
		"tool:read_terminal",
		"say:Shall I tell the Laptop client to carry on, sir?",
		"tool:type_into_terminal",
		"say:Told it, sir.",
	}
	if len(h.did) != len(want) {
		t.Fatalf("it did %v, want %v", h.did, want)
	}
	for i := range want {
		if h.did[i] != want[i] {
			t.Fatalf("it did %v, want %v", h.did, want)
		}
	}
}

// A rule nobody allowed can still be called off, which is how saying
// no is answered. Before this only a running rule could be cancelled.
func TestAWaitingRuleCanBeCalledOff(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	put(t, s, "every 5m",
		job.Step{Do: job.Ask, Say: "Shall I?"},
		job.Step{Do: job.Call, Tool: "type_into_terminal"},
	)
	runner(s, h, noon).Once(context.Background())

	stopped, err := s.Stop(context.Background(), "usr_1", "job_1")
	if err != nil || !stopped {
		t.Fatalf("Stop = %v, %v", stopped, err)
	}
	held, _ := s.Get("job_1")
	if held.State != job.Cancelled {
		t.Errorf("state = %q, want it called off", held.State)
	}
}
