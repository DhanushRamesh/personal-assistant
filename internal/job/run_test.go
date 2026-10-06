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
	got     map[string]string
	stopAt  string
	failAt  string
	failure error
}

func (h *hands) Do(_ context.Context, _ string, s job.Step, already []job.Ran) (job.Outcome, error) {
	h.saw = append(h.saw, len(already))
	h.heard = append(h.heard, job.Tools(already))
	h.carried = append(h.carried, already)
	what := string(s.Do) + ":" + s.Say + s.Tool + s.When
	h.did = append(h.did, what)
	if h.failAt != "" && strings.Contains(what, h.failAt) {
		return job.Outcome{}, h.failure
	}
	if h.stopAt != "" && strings.Contains(what, h.stopAt) {
		return job.Outcome{Stop: true, Why: "the check " + what + " did not pass"}, nil
	}
	return job.Outcome{Got: h.got[s.Tool]}, nil
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
