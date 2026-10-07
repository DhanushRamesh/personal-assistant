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

// TestLeavingTwiceIsLeavingTwice : A trigger asked whether the
// condition had changed since the reading before, which is right for
// a measurement and wrong for a thing that happened.
//
// This person's own week has two consecutive place.exited "home" --
// out in the morning, back, out again -- and the second was silent,
// because the last departure had also been from home.
func TestLeavingTwiceIsLeavingTwice(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "going out", "rule",
		"when place.exited is home",
		[]job.Step{{Do: job.Announce, Say: "you are heading out"}}, noon)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(j)

	got := watcher(s, h, seen{"place.exited": {
		at("home", 0), at("home", 3*time.Hour),
	}}).Landed(context.Background(), "usr_1", []string{"place.exited"})

	if got.Ran != 1 {
		t.Fatalf("leaving home a second time gave %+v, want Ran 1", got)
	}
}

// TestADischargingPhoneStillSpeaksOnce : The other half, and the
// reason the change test existed. A battery sliding past the line
// must not say something on every reading all the way down.
func TestADischargingPhoneStillSpeaksOnce(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	waiting(t, s, "when battery.level < 20")

	// Already under the line on the reading before.
	got := watcher(s, h, seen{"battery.level": {
		at("8", 0), at("9", time.Minute),
	}}).Landed(context.Background(), "usr_1", []string{"battery.level"})

	if got.Ran != 0 {
		t.Fatalf("a battery already under the line gave %+v, want Ran 0", got)
	}
	if len(h.did) != 0 {
		t.Errorf("it did %v", h.did)
	}
}

// TestAFullBatteryIsAMeasurement : "battery.level is 100" names a
// number and reads like naming a thing. Sorted as a thing that
// happened it would speak every time the phone reported a full
// battery while it sat on the charger -- which is why this is
// decided by the value and not by the operator.
func TestAFullBatteryIsAMeasurement(t *testing.T) {
	for _, c := range []struct {
		written  string
		measures bool
	}{
		{"battery.level is 100", true},
		{"battery.level < 10", true},
		{"place.exited is home", false},
		{"call.missed is not unknown", false},
		{"bluetooth.connected is bike helmet", false},
	} {
		gate, err := job.ParseGate(c.written)
		if err != nil {
			t.Fatalf("%q: %v", c.written, err)
		}
		if got := gate.Measures(); got != c.measures {
			t.Errorf("%q measures = %v, want %v", c.written, got, c.measures)
		}
	}
}

// TestATriggerThatWaitsArmsTheClock : Owner, 7 October 2026, on
// what the engine could not do: *"it can't wait."*
//
// Whether a phone is actually charging cannot be known when it is
// plugged in, only later, by whether the level moved. So a trigger
// may carry how long to wait: it does not run when it fires, it
// sets its own next time and the clock takes it from there.
func TestATriggerThatWaitsArmsTheClock(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "really charging", "rule",
		"when power.plugged is true then 15m",
		[]job.Step{{Do: job.Announce, Say: "it is not charging"}}, noon)
	if err != nil {
		t.Fatal(err)
	}
	s.Add(j)
	if !j.Next.IsZero() {
		t.Errorf("a waiting rule was given a next time up front: %s", j.Next)
	}

	got := watcher(s, h, seen{"power.plugged": {at("true", 0), at("false", time.Hour)}}).
		Landed(context.Background(), "usr_1", []string{"power.plugged"})

	if got.Armed != 1 || got.Ran != 0 {
		t.Fatalf("pass = %+v, want it armed and not run", got)
	}
	if len(h.did) != 0 {
		t.Errorf("it ran the sequence straight away: %v", h.did)
	}

	armed, _ := s.Get("job_1")
	if want := noon.Add(15 * time.Minute); !armed.Next.Equal(want) {
		t.Errorf("it runs at %s, want %s", armed.Next, want)
	}
	if armed.Last.IsZero() == false {
		t.Error("waiting was written down as a run")
	}

	// And then the clock, which was always there, picks it up.
	later := runner(s, h, noon.Add(16*time.Minute))
	later.Readings = seen{"power.plugged": {at("true", 16 * time.Minute)}}
	if ran := later.Once(context.Background()); ran.Ran != 1 {
		t.Fatalf("pass = %+v, want the clock to run it", ran)
	}
}

// TestATriggerWithNoWaitStillRunsAtOnce : The whole point of the
// others, and the thing a delay must not quietly change.
func TestATriggerWithNoWaitStillRunsAtOnce(t *testing.T) {
	s, h := inmemory.New(), &hands{}
	waiting(t, s, "when battery.level < 20")

	got := watcher(s, h, seen{"battery.level": {at("18", 0), at("22", time.Minute)}}).
		Landed(context.Background(), "usr_1", []string{"battery.level"})
	if got.Ran != 1 || got.Armed != 0 {
		t.Fatalf("pass = %+v, want it run and not armed", got)
	}
}

// TestAWaitThatIsNotALengthOfTime : Caught when the rule is written,
// in front of whoever wrote it.
func TestAWaitThatIsNotALengthOfTime(t *testing.T) {
	for _, schedule := range []string{
		"when power.plugged is true then soon",
		"when power.plugged is true then -5m",
		"when power.plugged is true then 0s",
	} {
		if _, err := job.New("job_1", "usr_1", "x", "rule", schedule,
			[]job.Step{{Do: job.Announce, Say: "hello"}}, noon); err == nil {
			t.Errorf("%q was accepted", schedule)
		}
	}
}

// TestARuleWithAClockAndATrigger : A rule had one mouth -- it came
// round on a clock or it waited for something, never both. Which is
// right for almost everything and wrong for the shape that keeps
// coming up: tell me when this happens, and look anyway now and then
// in case the moment passed while nothing was running.
func TestARuleWithAClockAndATrigger(t *testing.T) {
	const both = "every 30m also when battery.level < 20"

	if !job.Watches(both) {
		t.Error("it does not watch")
	}
	if !job.Ticks(both) {
		t.Error("it does not come round on a clock")
	}
	if got := job.Clock(both); got != "every 30m" {
		t.Errorf("its clock reads %q", got)
	}
	if got := job.Trigger(both); got != "when battery.level < 20" {
		t.Errorf("its trigger reads %q", got)
	}

	// Made with a next time, which a rule that only watches has not.
	s, h := inmemory.New(), &hands{}
	j, err := job.New("job_1", "usr_1", "a rule", "rule", both,
		[]job.Step{{Do: job.Announce, Say: "low"}}, noon)
	if err != nil {
		t.Fatal(err)
	}
	if j.Next.IsZero() {
		t.Fatal("a rule with a clock was given no next time")
	}
	j.Next = noon
	s.Add(j)

	// The clock sets it off.
	if got := runner(s, h, noon).Once(context.Background()); got.Ran != 1 {
		t.Fatalf("the clock gave %+v, want Ran 1", got)
	}
	// And it still has a next time afterwards, rather than being
	// left waiting for ever.
	after, _ := s.Get("job_1")
	if after.Next.IsZero() {
		t.Error("it was left with no next time, so the clock would never pick it up again")
	}

	// And so does a reading crossing its line.
	s2, h2 := inmemory.New(), &hands{}
	j2, _ := job.New("job_2", "usr_1", "a rule", "rule", both,
		[]job.Step{{Do: job.Announce, Say: "low"}}, noon)
	s2.Add(j2)
	got := watcher(s2, h2, seen{"battery.level": {at("18", 0), at("22", time.Minute)}}).
		Landed(context.Background(), "usr_1", []string{"battery.level"})
	if got.Ran != 1 {
		t.Fatalf("the trigger gave %+v, want Ran 1", got)
	}
}

// TestASchedulePartlyUnreadable : Both halves are read, and both must
// be readable. A rule half of which cannot be understood is a rule
// that would half work, at three in the morning.
func TestASchedulePartlyUnreadable(t *testing.T) {
	for _, schedule := range []string{
		"every 30m also when nonsense",
		"whenever also when battery.level < 20",
		"also when battery.level < 20",
		"every 30m also",
	} {
		if _, err := job.New("job_1", "usr_1", "x", "rule", schedule,
			[]job.Step{{Do: job.Announce, Say: "hello"}}, noon); err == nil {
			t.Errorf("%q was accepted", schedule)
		}
	}
}
