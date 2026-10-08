package job_test

import (
	"context"
	"testing"
	"time"

	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/job/inmemory"
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

// TestAConnectionThatHasStoppedWorking : A permission can die with
// nobody doing anything -- Google expires a refresh token after seven
// days while an app is in testing -- and the server finds out only
// when the next call fails. That is not a rule finding nothing. It is
// a capability gone, and every rule standing on it quietly does
// nothing until somebody looks.
//
// The one that prompted this had been dead nine hours, had said so in
// the log a hundred and nine times, and nothing had said it aloud.
func TestAConnectionThatHasStoppedWorking(t *testing.T) {
	now := time.Date(2026, 10, 7, 20, 0, 0, 0, time.UTC)
	broke := now.Add(-9 * time.Hour)

	gate, err := job.ParseGate("link.google is broken")
	if err != nil {
		t.Fatal(err)
	}
	if gate.Needs() != "link.google" {
		t.Errorf("it reads %q", gate.Needs())
	}
	// Read when somebody asks, so it can never set a rule off.
	if gate.Arrives() {
		t.Error("a rule would be allowed to wait for it, and wait for ever")
	}
	if _, err := job.Watched("when link.google is broken"); err == nil {
		t.Error("it was accepted as a trigger")
	}

	down := job.Reading{Value: job.Broken, At: broke, Known: true}
	if !gate.Passes(down, false, now) {
		t.Error("a broken connection did not read as broken")
	}
	up := job.Reading{Value: job.Working, At: now, Known: true}
	if gate.Passes(up, false, now) {
		t.Error("a working connection read as broken")
	}

	// Its time is when it broke, so how long it has been down is
	// asked with the operators already there.
	since, err := job.ParseConditions("link.google older than 1h")
	if err != nil {
		t.Fatal(err)
	}
	if !since.Passes(map[string][]job.Reading{"link.google": {down}}, false, now) {
		t.Error("nine hours down did not read as older than an hour")
	}
}

// TestWhatAConnectionCanBe : Working or broken and nothing else. Why
// it broke is the connection's own business and is recorded beside
// it; a rule asking about the wording would break when the wording
// changed.
func TestWhatAConnectionCanBe(t *testing.T) {
	for _, written := range []string{
		"link.google is down", "link.google < 20", "link.google older than 1h ago",
		"link.google arrives", "link.google older than soon", "link.google older than soon",
	} {
		if _, err := job.ParseGate(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
	for _, written := range []string{
		"link.google is broken", "link.google is not working", "link.anything is broken",
	} {
		if _, err := job.ParseGate(written); err != nil {
			t.Errorf("%q was refused: %v", written, err)
		}
	}
}

// TestConditionsJoinedWithAnd : The engine ands across steps -- two
// checks in a row are two things that must both hold -- and a rule
// asked for aloud could only ever have one check. What the engine
// could do and what could be asked for had come apart.
func TestConditionsJoinedWithAnd(t *testing.T) {
	all, err := job.ParseAll("hour >= 22 and battery.level < 20")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("read %d conditions, want two", len(all))
	}
	if got := all[0].Written(); got != "hour >= 22" {
		t.Errorf("the first reads %q", got)
	}
	if got := all[1].Written(); got != "battery.level < 20" {
		t.Errorf("the second reads %q", got)
	}
}

// TestAndIsLooserThanOr : The one thing here that has to be learned
// rather than guessed. It falls out of the shape -- steps are the
// and, a step is the or -- rather than from the usual precedence.
func TestAndIsLooserThanOr(t *testing.T) {
	all, err := job.ParseAll("hour >= 22 and battery.level < 20 or presence is away")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("read %d conditions, want two: the hour, then either of the other two", len(all))
	}
	if len(all[1]) != 2 {
		t.Errorf("the second condition holds %d alternatives, want two", len(all[1]))
	}

	where := time.FixedZone("IST", 19800)
	late := time.Date(2026, 10, 7, 23, 0, 0, 0, where)
	early := time.Date(2026, 10, 7, 14, 0, 0, 0, where)
	full := map[string][]job.Reading{
		"battery.level": {{Value: "80", At: late, Known: true}},
	}

	// Late, battery full, and away: the hour holds and the second
	// condition is carried by presence.
	if !all[0].Passes(full, true, late) || !all[1].Passes(full, true, late) {
		t.Error("late and away did not satisfy both conditions")
	}
	// Early is early however the rest of it stands.
	if all[0].Passes(full, true, early) {
		t.Error("two in the afternoon passed an hour of ten at night")
	}
	// Late and here with a full battery satisfies neither
	// alternative of the second.
	if all[1].Passes(full, false, late) {
		t.Error("a full battery with somebody in the room satisfied the second condition")
	}
}

// TestOneBadConditionRefusesTheWhole : Caught in front of whoever
// wrote it, rather than a rule that half works at three in the
// morning.
func TestOneBadConditionRefusesTheWhole(t *testing.T) {
	for _, written := range []string{
		"hour >= 22 and nonsense",
		"hour >= 99 and battery.level < 20",
		"battery.level < 20 and hour is noon",
	} {
		if _, err := job.ParseAll(written); err == nil {
			t.Errorf("%q was accepted", written)
		}
	}
}

// TestAPhoneThatStoppedReportingIsNotAPhoneThatStoppedCharging :
// Owner, told the battery had sat at five percent for a quarter of an
// hour: *"what if the phone is stale, I did not intend to put the
// charge."*
//
// The two look identical from here -- a level that has not changed --
// and the difference is whether anything was reported at all. A phone
// asleep in a pocket sends nothing, so the newest reading is older
// than the one it would be compared against, and a value equals
// itself. Left alone, every sleeping phone reads as a dead cable.
func TestAPhoneThatStoppedReportingIsNotAPhoneThatStoppedCharging(t *testing.T) {
	now := time.Date(2026, 10, 8, 22, 0, 0, 0, time.UTC)

	not, err := job.ParseConditions("battery.level <= 15m ago")
	if err != nil {
		t.Fatal(err)
	}

	// Nothing has arrived for twenty minutes. The level may be
	// climbing happily; nobody here can tell.
	asleep := map[string][]job.Reading{"battery.level": {
		{Value: "5", At: now.Add(-20 * time.Minute), Known: true},
		{Value: "5", At: now.Add(-40 * time.Minute), Known: true},
	}}
	if not.Passes(asleep, false, now) {
		t.Error("a phone that stopped reporting read as a phone that stopped charging")
	}

	// One reading and nothing to compare it with is the same case.
	alone := map[string][]job.Reading{"battery.level": {
		{Value: "5", At: now.Add(-20 * time.Minute), Known: true},
	}}
	if not.Passes(alone, false, now) {
		t.Error("a single stale reading was compared with itself")
	}

	// And a phone that is reporting, with a level that genuinely is
	// not moving, still says so -- which is the thing the rule is for.
	reporting := map[string][]job.Reading{"battery.level": {
		{Value: "5", At: now, Known: true},
		{Value: "5", At: now.Add(-7 * time.Minute), Known: true},
		{Value: "5", At: now.Add(-16 * time.Minute), Known: true},
	}}
	if !not.Passes(reporting, false, now) {
		t.Error("a level reported three times and never moving did not read as stuck")
	}
}

// TestAConditionCanAskHomeAssistant : Everything else a rule reads
// is as old as whenever a device sent it. A rule that waits twenty
// minutes and then asks whether the phone is still charging must
// ask the phone, and this is the only source that answers now.
func TestAConditionCanAskHomeAssistant(t *testing.T) {
	g, err := job.ParseGate("ha.sensor.pixel_7_battery_state is charging")
	if err != nil {
		t.Fatalf("could not read a Home Assistant condition: %v", err)
	}
	if !job.Hassed(g.Source) {
		t.Errorf("did not recognise %q as a Home Assistant source", g.Source)
	}
	if got := job.Entity(g.Source); got != "sensor.pixel_7_battery_state" {
		t.Errorf("entity is %q, want the id as Home Assistant spells it", got)
	}
	if g.Needs() != g.Source {
		t.Errorf("a Home Assistant gate has to be fetched; Needs gave %q", g.Needs())
	}

	// Nothing here arrives. It is asked, and only while a rule is
	// already running, so it can never set one off.
	if g.Arrives() {
		t.Error("let a Home Assistant entity be a trigger")
	}

	// An event kind is still an event kind.
	ev, err := job.ParseGate("battery.level < 25")
	if err != nil {
		t.Fatalf("broke an ordinary condition: %v", err)
	}
	if job.Hassed(ev.Source) || job.Entity(ev.Source) != "" {
		t.Errorf("read the event kind %q as a Home Assistant entity", ev.Source)
	}
	if !ev.Arrives() {
		t.Error("stopped an event kind being a trigger")
	}
}

// TestWhatHomeAssistantSaysIsComparedLikeAnythingElse : The point of
// a namespace rather than a word per entity is that every operator
// already there goes on working. A state is a word, a level is a
// number, and last_changed is a moment.
func TestWhatHomeAssistantSaysIsComparedLikeAnythingElse(t *testing.T) {
	at := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	stood := job.Reading{Value: "charging", At: at.Add(-90 * time.Minute), Known: true}

	for _, c := range []struct {
		cond string
		want bool
	}{
		{"ha.sensor.pixel_7_battery_state is charging", true},
		{"ha.sensor.pixel_7_battery_state is not charging", false},
		{"ha.sensor.pixel_7_battery_state is discharging", false},
		{"ha.sensor.pixel_7_battery_state older than 1h", true},
		{"ha.sensor.pixel_7_battery_state older than 3h", false},
		{"ha.sensor.pixel_7_battery_state within 30m", false},
	} {
		g, err := job.ParseGate(c.cond)
		if err != nil {
			t.Fatalf("%q: %v", c.cond, err)
		}
		if got := g.Passes(stood, false, at); got != c.want {
			t.Errorf("%q gave %v, want %v", c.cond, got, c.want)
		}
	}

	// Numbers compare as numbers.
	level := job.Reading{Value: "41", At: at, Known: true}
	g, err := job.ParseGate("ha.sensor.pixel_7_battery_level < 95")
	if err != nil {
		t.Fatal(err)
	}
	if !g.Passes(level, false, at) {
		t.Error("would not compare a Home Assistant level as a number")
	}

	// Not knowing is not a fault, and never passes.
	if g.Passes(job.Reading{}, false, at) {
		t.Error("a condition held against an entity nothing is known about")
	}
}
