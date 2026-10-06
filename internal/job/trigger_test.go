package job_test

import (
	"context"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
	"github.com/DhanushRamesh/personal-assistant/internal/job/inmemory"
)

// seen : Readings of one kind, newest first.
type seen map[string][]job.Reading

func (s seen) Latest(_ context.Context, _, kind string, n int) ([]job.Reading, error) {
	got := s[kind]
	if n > 0 && len(got) > n {
		got = got[:n]
	}
	return got, nil
}

// at : A reading taken a given time before noon.
func at(value string, old time.Duration) job.Reading {
	return job.Reading{Value: value, At: noon.Add(-old), Known: true}
}

// waiting : A rule that waits on a condition rather than a time.
func waiting(t *testing.T, s *inmemory.Store, when string) job.Job {
	t.Helper()
	j, err := job.New("job_1", "usr_1", "a rule", "rule", when,
		[]job.Step{{Do: job.Announce, Say: "your battery is low"}}, noon)
	if err != nil {
		t.Fatalf("job.New(%q): %v", when, err)
	}
	s.Add(j)
	return j
}

// watcher : A runner with readings to look at.
func watcher(s *inmemory.Store, h *hands, r seen) *job.Runner {
	run := runner(s, h, noon)
	run.Readings = r
	return run
}

// Owner: *"currently the trigger is time, we should have event
// trigger also."* A watching rule has no next time at all, so the
// clock never picks it up.
func TestAWatchingRuleIsNeverDueByTheClock(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j := waiting(t, s, "when battery.level < 20")
	if !j.Next.IsZero() {
		t.Errorf("a watching rule was given a next time of %s", j.Next)
	}

	if got := watcher(s, h, nil).Once(context.Background()); got.Ran != 0 {
		t.Fatalf("pass = %+v, want the clock to leave it alone", got)
	}
	if len(h.did) != 0 {
		t.Errorf("the clock ran a watching rule: %v", h.did)
	}
}

// It fires on the crossing, not on the condition being true. A
// phone sitting at fifteen per cent reports itself every few
// minutes, and a rule that ran each time would say the same thing
// all afternoon.
func TestARuleFiresOnTheCrossingOnly(t *testing.T) {
	for _, c := range []struct {
		why      string
		readings []job.Reading
		want     int
	}{
		{"it has just dropped below", []job.Reading{at("19", 0), at("21", time.Minute)}, 1},
		{"it was already below", []job.Reading{at("15", 0), at("17", time.Minute)}, 0},
		{"it is still above", []job.Reading{at("40", 0), at("45", time.Minute)}, 0},
		{"it has climbed back over", []job.Reading{at("30", 0), at("19", time.Minute)}, 0},
		{"there is nothing before it", []job.Reading{at("19", 0)}, 0},
		{"there is nothing at all", nil, 0},
	} {
		t.Run(c.why, func(t *testing.T) {
			s, h := inmemory.New(), &hands{}
			waiting(t, s, "when battery.level < 20")

			got := watcher(s, h, seen{"battery.level": c.readings}).
				Landed(context.Background(), "usr_1", []string{"battery.level"})
			if got.Ran != c.want {
				t.Fatalf("pass = %+v, want Ran %d", got, c.want)
			}
			if len(h.did) != c.want {
				t.Errorf("it did %v", h.did)
			}
		})
	}
}

// A rule only wakes for the kind it is waiting on.
func TestAnUnrelatedReadingWakesNothing(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	waiting(t, s, "when battery.level < 20")

	got := watcher(s, h, seen{"battery.level": {at("19", 0), at("21", time.Minute)}}).
		Landed(context.Background(), "usr_1", []string{"wifi.joined"})
	if got.Ran != 0 || len(h.did) != 0 {
		t.Errorf("an unrelated reading ran it: %+v %v", got, h.did)
	}
}

// Somebody else's readings are not theirs.
func TestAnotherPersonsReadingWakesNothing(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	waiting(t, s, "when battery.level < 20")

	got := watcher(s, h, seen{"battery.level": {at("19", 0), at("21", time.Minute)}}).
		Landed(context.Background(), "usr_2", []string{"battery.level"})
	if got.Ran != 0 {
		t.Errorf("it ran for the wrong person: %+v", got)
	}
}

// Having fired, it is still waiting: no next time, and not done.
func TestAWatchingRuleDoesNotEnd(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	waiting(t, s, "when battery.level < 20")

	watcher(s, h, seen{"battery.level": {at("19", 0), at("21", time.Minute)}}).
		Landed(context.Background(), "usr_1", []string{"battery.level"})

	after, _ := s.Get("job_1")
	if after.State != job.Active {
		t.Errorf("state = %q, want it still waiting", after.State)
	}
	if !after.Next.IsZero() {
		t.Errorf("it was given a next time of %s", after.Next)
	}
	if after.Last.IsZero() {
		t.Error("nothing records that it ran")
	}
}

// A trigger must name an event kind. Presence does not arrive, and
// a rule about how old a reading is could never be true at the
// moment one lands.
func TestATriggerMustBeSomethingThatArrives(t *testing.T) {
	for _, when := range []string{
		"when presence is away",
		"when battery.level older than 2h",
		"when battery.level within 5m",
		"when nonsense",
		"when",
	} {
		if _, err := job.Watched(when); err == nil {
			t.Errorf("%q was accepted as a trigger", when)
		}
		if _, err := job.New("j", "u", "n", "rule", when,
			[]job.Step{{Do: job.Announce, Say: "hello"}}, noon); err == nil {
			t.Errorf("a job on %q was accepted", when)
		}
	}
	if _, err := job.Watched("when battery.level < 20"); err != nil {
		t.Errorf("a good trigger was refused: %v", err)
	}
}

// A phone handing over its readings must never wait on a rule,
// still less on the model a rule might ask for a sentence.
func TestArrivedNeverBlocks(t *testing.T) {
	r := &job.Runner{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < job.Backlog*3; i++ {
			r.Arrived("usr_1", []string{"battery.level"})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Arrived blocked with nothing reading the other end")
	}
	if r.Dropped() == 0 {
		t.Error("nothing was counted as dropped, so the backlog is unbounded")
	}
}

// The record is of what the assistant considered saying. A rule
// that only ever runs a tool considered nothing, and writing it
// down as having held its tongue would be a lie.
func TestOnlyARuleThatMightSpeakIsWrittenDown(t *testing.T) {
	for _, c := range []struct {
		why  string
		step job.Step
		want int
	}{
		{"it speaks", job.Step{Do: job.Announce, Say: "hello"}, 1},
		{"it writes", job.Step{Do: job.Write, Ask: "say something"}, 1},
		{"it only runs a tool", job.Step{Do: job.Call, Tool: "task_lists"}, 0},
	} {
		t.Run(c.why, func(t *testing.T) {
			s, h := inmemory.New(), &hands{}
			kept := &written{}
			j, err := job.New("job_1", "usr_1", "a rule", "rule", "every 5m",
				[]job.Step{c.step}, noon)
			if err != nil {
				t.Fatalf("job.New: %v", err)
			}
			j.Next = noon
			s.Add(j)

			run := runner(s, h, noon)
			run.Journal = kept
			run.Once(context.Background())

			if len(kept.records) != c.want {
				t.Errorf("%d rows written, want %d", len(kept.records), c.want)
			}
		})
	}
}

// written : A journal that keeps what it was given.
type written struct{ records []job.Record }

func (w *written) Wrote(_ context.Context, r job.Record) error {
	w.records = append(w.records, r)
	return nil
}
