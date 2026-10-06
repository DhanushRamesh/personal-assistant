package job_test

import (
	"context"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
	"github.com/DhanushRamesh/personal-assistant/internal/job/inmemory"
)

// reading : A known reading of a given age.
func reading(value string, old time.Duration) job.Reading {
	return job.Reading{Value: value, At: noon.Add(-old), Known: true}
}

// A gate reads a number, a word, or how long ago the reading came,
// and presence answers for the room instead of an event.
func TestAConditionHolds(t *testing.T) {
	for _, c := range []struct {
		when string
		r    job.Reading
		away bool
		want bool
	}{
		{"battery.level < 20", reading("15", time.Minute), false, true},
		{"battery.level < 20", reading("45", time.Minute), false, false},
		{"battery.level < 20", reading("20", time.Minute), false, false},
		{"battery.level >= 20", reading("20", time.Minute), false, true},
		{"battery.level > 90", reading("100", time.Minute), false, true},
		{"battery.level is 100", reading("100", time.Minute), false, true},
		{"battery.level is not 100", reading("99", time.Minute), false, true},
		{"call.missed is an unknown number", reading("an unknown number", time.Minute), false, true},
		{"call.missed is AN UNKNOWN NUMBER", reading("an unknown number", time.Minute), false, true},

		// How long ago, which is how a rule says a device has
		// stopped talking.
		{"battery.level older than 2h", reading("50", 3*time.Hour), false, true},
		{"battery.level older than 2h", reading("50", time.Hour), false, false},
		{"battery.level within 10m", reading("50", time.Minute), false, true},
		{"battery.level within 10m", reading("50", time.Hour), false, false},

		{"presence is here", job.Reading{}, false, true},
		{"presence is here", job.Reading{}, true, false},
		{"presence is away", job.Reading{}, true, true},
		{"presence is not away", job.Reading{}, false, true},
	} {
		g, err := job.ParseGate(c.when)
		if err != nil {
			t.Fatalf("ParseGate(%q): %v", c.when, err)
		}
		if got := g.Passes(c.r, c.away, noon); got != c.want {
			t.Errorf("%q against %q = %v, want %v", c.when, c.r.Value, got, c.want)
		}
	}
}

// Nothing is known, so nothing is proved. The same answer notice
// gives when it cannot tell which way a value was moving: the
// cheaper mistake is the sequence that does nothing.
func TestAnUnknownReadingNeverPasses(t *testing.T) {
	for _, when := range []string{
		"battery.level < 20",
		"battery.level > 20",
		"battery.level is 20",
		"battery.level is not 20",
		"battery.level older than 2h",
	} {
		g, err := job.ParseGate(when)
		if err != nil {
			t.Fatalf("ParseGate(%q): %v", when, err)
		}
		if g.Passes(job.Reading{}, false, noon) {
			t.Errorf("%q passed with nothing to read", when)
		}
	}
}

// A sender reporting a word where the rule expected a number is
// not an error; it is simply something this gate cannot order.
func TestAWordIsNotOrdered(t *testing.T) {
	g, _ := job.ParseGate("battery.level < 20")
	if g.Passes(reading("unknown", time.Minute), false, noon) {
		t.Error("a word compared as a number")
	}
}

// A condition that cannot work should fail in front of whoever
// wrote it, not silently at three in the morning.
func TestAnUnreadableConditionIsRefused(t *testing.T) {
	for _, when := range []string{
		"",
		"battery.level",
		"battery.level < ",
		"battery.level 20",              // no comparison
		"battery.level < twenty",        // not a number
		"battery.level older than ages", // not a length of time
		"presence is probably",          // presence is here or away
		"presence < 20",                 // and never ordered
	} {
		if g, err := job.ParseGate(when); err == nil {
			t.Errorf("%q was accepted as %+v", when, g)
		}
	}
	if err := (job.Step{Do: job.Check}).Valid(); err == nil {
		t.Error("a check with no condition was accepted")
	}
	if err := (job.Step{Do: job.Check, When: "nonsense"}).Valid(); err == nil {
		t.Error("a check nobody can read was accepted")
	}
	if _, err := job.New("j", "u", "n", "rule", "every 5m",
		[]job.Step{{Do: job.Check, When: "nonsense"}}, noon); err == nil {
		t.Error("a job carrying an unreadable check was accepted")
	}
}

// "is not" must never be read as "is".
func TestTheLongerComparisonWins(t *testing.T) {
	g, err := job.ParseGate("battery.level is not 100")
	if err != nil {
		t.Fatalf("ParseGate: %v", err)
	}
	if g.Op != job.IsNot || g.Value != "100" {
		t.Errorf("read as op %q value %q", g.Op, g.Value)
	}
}

// Which event kind the server has to look up, and nothing to look
// up for presence.
func TestAGateSaysWhatItNeeds(t *testing.T) {
	g, _ := job.ParseGate("battery.level < 20")
	if g.Needs() != "battery.level" {
		t.Errorf("needs %q", g.Needs())
	}
	g, _ = job.ParseGate("presence is away")
	if g.Needs() != "" {
		t.Errorf("presence asked for the event %q", g.Needs())
	}
}

// A check as the first step is a condition on the whole job: it
// does not run, it is not a failure, and it comes round again.
func TestACheckAtTheFrontGatesTheWholeJob(t *testing.T) {
	s, h := inmemory.New(), &hands{stopAt: "battery.level"}
	put(t, s, "every 5m",
		job.Step{Do: job.Check, When: "battery.level < 20"},
		job.Step{Do: job.Announce, Say: "you are low"},
		job.Step{Do: job.Call, Tool: "alarm_set"})

	got := runner(s, h, noon).Once(context.Background())
	if got.Stopped != 1 || got.Failed != 0 {
		t.Fatalf("pass = %+v, want stopped and not failed", got)
	}
	if len(h.did) != 1 {
		t.Errorf("it carried on past the check: %v", h.did)
	}
	after, _ := s.Get("job_1")
	if after.State != job.Active || !after.Next.Equal(noon.Add(5*time.Minute)) {
		t.Errorf("a gated job did not come round again: %q at %s", after.State, after.Next)
	}
	if after.Why == "" {
		t.Error("nothing records which check stopped it")
	}
}

// A sequence is a transcript, not variables: a write step is given
// what the tools before it returned, so the sentence can be about
// whatever was just found.
func TestAWriteStepIsGivenWhatTheToolsFound(t *testing.T) {
	s := inmemory.New()
	h := &hands{got: map[string]string{"calendar_events": "Dentist at 4pm"}}
	put(t, s, "at 08:00",
		job.Step{Do: job.Call, Tool: "calendar_events"},
		job.Step{Do: job.Write, Ask: "say what is coming up"})

	runner(s, h, noon).Once(context.Background())
	if len(h.saw) != 2 {
		t.Fatalf("ran %d steps", len(h.saw))
	}
	if len(h.carried) != 2 || len(h.carried[1]) != 1 {
		t.Fatalf("the write step was given %v", h.carried)
	}
	if h.carried[1][0].Got != "Dentist at 4pm" {
		t.Errorf("it was given %q", h.carried[1][0].Got)
	}
}

// Silence is an answer. A rule that had to say something every
// time it ran would be worse than no rule.
func TestAWriteWithNothingToSayStopsTheRest(t *testing.T) {
	s, h := inmemory.New(), &hands{stopAt: "write"}
	put(t, s, "every 5m",
		job.Step{Do: job.Write, Ask: "anything worth saying?"},
		job.Step{Do: job.Call, Tool: "alarm_set"})

	got := runner(s, h, noon).Once(context.Background())
	if got.Stopped != 1 || got.Failed != 0 {
		t.Fatalf("pass = %+v, want stopped and not failed", got)
	}
	if len(h.did) != 1 {
		t.Errorf("it carried on past the silence: %v", h.did)
	}
}

// A write with no instruction cannot work, and should fail in
// front of whoever wrote it.
func TestAWriteNeedsSomethingToWriteAbout(t *testing.T) {
	if err := (job.Step{Do: job.Write}).Valid(); err == nil {
		t.Error("a write with nothing to write about was accepted")
	}
	if _, err := job.New("j", "u", "n", "rule", "every 5m",
		[]job.Step{{Do: job.Write}}, noon); err == nil {
		t.Error("a job carrying an empty write was accepted")
	}
}
