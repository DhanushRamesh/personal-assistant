package job_test

import (
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
)

// TestAMomentStillToComeSatisfiesNothing : A device reading is always
// in the past, so "within ten minutes" could only ever mean the ten
// minutes behind us. What somebody usually does at nine in the
// evening is not in the past at breakfast, and the same comparison
// read as true from midnight onwards -- a morning rule that would
// have fired all night.
func TestAMomentStillToComeSatisfiesNothing(t *testing.T) {
	where := time.FixedZone("IST", 19800)
	breakfast := time.Date(2026, 10, 7, 8, 0, 0, 0, where)
	evening := time.Date(2026, 10, 7, 21, 0, 0, 0, where)
	ahead := job.Reading{Value: "21", At: evening, Known: true}

	for _, written := range []string{
		"usually.home.arrives within 30m",
		"usually.home.arrives older than 2h",
	} {
		gate, err := job.ParseGate(written)
		if err != nil {
			t.Fatalf("%q: %v", written, err)
		}
		if gate.Passes(ahead, false, breakfast) {
			t.Errorf("%q held thirteen hours before the moment it names", written)
		}
	}

	// And still works once the moment has passed.
	late := time.Date(2026, 10, 7, 23, 15, 0, 0, where)
	gate, err := job.ParseGate("usually.home.arrives older than 2h")
	if err != nil {
		t.Fatal(err)
	}
	if !gate.Passes(ahead, false, late) {
		t.Error("two and a quarter hours past nine is not older than two hours")
	}
}

// TestARuleCannotWaitForARoutine : A trigger fires when a reading
// lands. A routine is a time of day and nothing lands, so a rule
// written to wait for one would wait for ever.
func TestARuleCannotWaitForARoutine(t *testing.T) {
	if _, err := job.Watched("when usually.home.leaves is 10"); err == nil {
		t.Fatal("a rule was allowed to wait for a routine")
	}
	// It is still perfectly good as a check.
	if _, err := job.ParseGate("usually.home.leaves older than 90m"); err != nil {
		t.Errorf("a routine could not be used as a condition: %v", err)
	}
}

// TestARoutineIsFetchedEvenThoughItIsNoTrigger : Two different
// questions that were briefly one. "Nothing to look up" is true of
// the clock and of presence, which are answered from the moment;
// "cannot set a rule off" is true of those and of a routine as well.
//
// Conflating them made every condition on a routine silently fail:
// the check asked which kinds to read, was told none, and compared
// an hour it had never fetched.
func TestARoutineIsFetchedEvenThoughItIsNoTrigger(t *testing.T) {
	got, err := job.ParseConditions("usually.home.leaves older than 1h")
	if err != nil {
		t.Fatal(err)
	}
	if kinds := got.Needs(); len(kinds) != 1 || kinds[0] != "usually.home.leaves" {
		t.Errorf("the check would read %v, so it compares an hour it never fetched", kinds)
	}
	if _, err := job.Watched("when usually.home.leaves is 10"); err == nil {
		t.Error("a rule was allowed to wait for a routine")
	}

	for _, written := range []string{"hour >= 10", "day is saturday", "presence is here"} {
		one, err := job.ParseConditions(written)
		if err != nil {
			t.Fatal(err)
		}
		if kinds := one.Needs(); len(kinds) != 0 {
			t.Errorf("%q asked to read %v, and is answered from the moment", written, kinds)
		}
	}
}
