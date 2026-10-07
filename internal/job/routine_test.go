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
