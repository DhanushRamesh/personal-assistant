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

// TestAConditionMayHaveAlternatives : Owner, 7 October 2026 -- "I
// should be able to add like and and or operator." And was already
// the sequence: two checks in a row are two things that must both
// hold. This is the or, and it lives inside one check.
func TestAConditionMayHaveAlternatives(t *testing.T) {
	where := time.FixedZone("IST", 19800)
	saturday := time.Date(2026, 10, 10, 14, 0, 0, 0, where)

	got, err := job.ParseConditions("hour >= 22 or day is weekend")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d alternatives, want two", len(got))
	}
	// Neither reads anything a device reported, so neither asks for
	// a kind and the check needs no lookup at all.
	if kinds := got.Needs(); len(kinds) != 0 {
		t.Errorf("asked for readings it cannot use: %v", kinds)
	}
	// Two in the afternoon fails the first and the second carries it.
	if !got.Passes(nil, false, saturday) {
		t.Error("a Saturday afternoon satisfied neither alternative")
	}

	wednesday := time.Date(2026, 10, 7, 14, 0, 0, 0, where)
	if got.Passes(nil, false, wednesday) {
		t.Error("a Wednesday afternoon satisfied one of them")
	}
	if !got.Passes(nil, false, wednesday.Add(9*time.Hour)) {
		t.Error("eleven at night did not satisfy the hour")
	}
}

// TestAlternativesReadEveryKindTheyNeed : A single gate reads at most
// one kind. A condition satisfied by the battery or by where somebody
// is has to read both before it knows which.
func TestAlternativesReadEveryKindTheyNeed(t *testing.T) {
	got, err := job.ParseConditions(
		"battery.level < 20 or place.entered is office or battery.level is 100")
	if err != nil {
		t.Fatal(err)
	}
	kinds := got.Needs()
	if len(kinds) != 2 {
		t.Fatalf("asks for %v, want the two distinct kinds", kinds)
	}

	now := time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)
	at := map[string][]job.Reading{
		"battery.level": {{Value: "64", At: now, Known: true}},
		"place.entered": {{Value: "office", At: now, Known: true}},
	}
	if !got.Passes(at, false, now) {
		t.Error("at the office with a full battery satisfied nothing")
	}
	delete(at, "place.entered")
	if got.Passes(at, false, now) {
		t.Error("a kind that was never read was treated as holding")
	}
}

// TestTheHourAndTheDayAreNotTriggers : A trigger fires when a reading
// lands. The clock does not land, and a rule waiting for ten in the
// morning would wait for ever.
func TestTheHourAndTheDayAreNotTriggers(t *testing.T) {
	for _, written := range []string{"when hour >= 10", "when day is saturday"} {
		if _, err := job.Watched(written); err == nil {
			t.Errorf("%q was allowed as a trigger", written)
		}
	}
	// A schedule is how a clock starts a rule, and that already works.
	if _, err := job.ParseConditions("hour >= 10"); err != nil {
		t.Errorf("the hour could not be used as a condition: %v", err)
	}
}

// TestAnHourThatIsNotAnHour : Caught when the rule is written rather
// than at three in the morning.
func TestAnHourThatIsNotAnHour(t *testing.T) {
	for _, written := range []string{
		"hour >= 24", "hour is noon", "hour older than 2h", "day is somedayday",
	} {
		if _, err := job.ParseConditions(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
}

// TestAReadingAgainstItsOwnPast : Owner, 7 October 2026, on the
// engine's limitations -- "it can't compare two things." It could
// only compare a reading with a number somebody wrote down, so
// "lower than it was an hour ago" had nowhere to go. That is the
// whole of knowing whether a phone on charge is actually charging.
func TestAReadingAgainstItsOwnPast(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)

	// Plugged in a quarter of an hour ago and nothing has moved.
	stuck := map[string][]job.Reading{"battery.level": {
		{Value: "18", At: now, Known: true},
		{Value: "18", At: now.Add(-5 * time.Minute), Known: true},
		{Value: "18", At: now.Add(-16 * time.Minute), Known: true},
	}}
	// And the same quarter of an hour spent charging.
	rising := map[string][]job.Reading{"battery.level": {
		{Value: "31", At: now, Known: true},
		{Value: "24", At: now.Add(-5 * time.Minute), Known: true},
		{Value: "18", At: now.Add(-16 * time.Minute), Known: true},
	}}

	not, err := job.ParseConditions("battery.level <= 15m ago")
	if err != nil {
		t.Fatal(err)
	}
	if !not.Passes(stuck, false, now) {
		t.Error("a level that has not moved in a quarter of an hour read as rising")
	}
	if not.Passes(rising, false, now) {
		t.Error("eighteen to thirty-one read as not rising")
	}

	up, err := job.ParseConditions("battery.level > 15m ago")
	if err != nil {
		t.Fatal(err)
	}
	if !up.Passes(rising, false, now) {
		t.Error("a level that climbed thirteen points did not read as higher")
	}
	if up.Passes(stuck, false, now) {
		t.Error("a level that did not move read as higher")
	}
}

// TestNothingGoesBackThatFar : The comparison cannot be made, so it
// does not hold -- the same answer a gate gives about a kind nobody
// has reported. A rule that fired because the past was unknown would
// fire hardest on the day a device started reporting.
func TestNothingGoesBackThatFar(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	young := map[string][]job.Reading{"battery.level": {
		{Value: "18", At: now, Known: true},
		{Value: "18", At: now.Add(-2 * time.Minute), Known: true},
	}}

	for _, written := range []string{
		"battery.level <= 15m ago", "battery.level > 15m ago",
	} {
		got, err := job.ParseConditions(written)
		if err != nil {
			t.Fatal(err)
		}
		if got.Passes(young, false, now) {
			t.Errorf("%q held on two minutes of history", written)
		}
	}
}

// TestHowDeepAConditionHasToRead : Asked of the conditions and not
// of each gate, because the check fetches once per kind.
func TestHowDeepAConditionHasToRead(t *testing.T) {
	for _, c := range []struct {
		written string
		deep    time.Duration
	}{
		{"battery.level < 20", 0},
		{"battery.level <= 15m ago", 15 * time.Minute},
		{"battery.level < 20 or battery.level <= 2h ago", 2 * time.Hour},
		{"hour >= 10 or day is weekend", 0},
	} {
		got, err := job.ParseConditions(c.written)
		if err != nil {
			t.Fatalf("%q: %v", c.written, err)
		}
		if deep := got.Deep(); deep != c.deep {
			t.Errorf("%q reads back %s, want %s", c.written, deep, c.deep)
		}
	}
}

// TestWhatHasNoPast : The clock, presence and a routine are answered
// from the moment or from a number somebody worked out, and none of
// them has readings to look back through.
func TestWhatHasNoPast(t *testing.T) {
	for _, written := range []string{
		"hour >= 2h ago",
		"day is 1h ago",
		"presence is 5m ago",
		"usually.home.leaves <= 1h ago",
		"battery.level older than 5m ago",
		"battery.level < soon ago",
	} {
		if _, err := job.ParseConditions(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
}

// TestAGateThatAsksOnlyWhetherSomethingArrived : Every other gate
// asks about a value, and for a measurement that is the whole
// question. For a thing that happened the value is often beside the
// point and sometimes absent: a phone cannot always tell what it has
// been plugged into and reports "unknown", so a rule wanting every
// plug-in had to say `is not` against a string that could never
// occur. It worked and read like a trick.
func TestAGateThatAsksOnlyWhetherSomethingArrived(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)

	gate, err := job.ParseGate("power.plugged arrives")
	if err != nil {
		t.Fatal(err)
	}
	if gate.Needs() != "power.plugged" {
		t.Errorf("it reads %q", gate.Needs())
	}
	// Whatever the value, including the one the phone actually sends.
	for _, value := range []string{"unknown", "ac", "", "anything"} {
		if !gate.Passes(job.Reading{Value: value, At: now, Known: true}, false, now) {
			t.Errorf("a reading saying %q did not count as having arrived", value)
		}
	}
	// And nothing at all is still nothing.
	if gate.Passes(job.Reading{}, false, now) {
		t.Error("a kind nobody has reported counted as having arrived")
	}
	// It asks nothing about the value, so there is no line to cross
	// and every arrival is its own occasion.
	if gate.Measures() {
		t.Error("it was taken for a measurement, so a second plug-in would be silent")
	}
}

// TestATypoIsStillRefused : The word is required rather than taking
// the kind alone, because "when nonsense" would be a rule that never
// fired and never said why.
func TestATypoIsStillRefused(t *testing.T) {
	for _, written := range []string{"battery.level", "nonsense", "power.plugged arrived"} {
		if _, err := job.ParseGate(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
}

// TestWhatDoesNotArrive : The clock and presence are answered from
// the moment, and a routine is worked out and kept. Nothing of any
// of them ever lands.
func TestWhatDoesNotArrive(t *testing.T) {
	for _, written := range []string{
		"hour arrives", "day arrives", "presence arrives", "usually.home.leaves arrives",
	} {
		if _, err := job.ParseGate(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
}
