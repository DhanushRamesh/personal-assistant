package tool_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// A change carries both ends, because the person is listening and has
// nothing to compare against.
func TestAChangeCarriesBothEnds(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "10:30 am"})

	for _, want := range []string{"9:00 am", "10:30 am", "was", "is now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// Only what actually moved is reported. A field given the same value it
// already held is not a change and must not be announced as one.
func TestOnlyWhatMovedIsReported(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "10:30 am"},
		tool.Change{What: "the name", From: "Tablets", To: "Tablets"})

	if strings.Contains(got.Content, "the name") {
		t.Errorf("content = %q, want the unchanged field left out", got.Content)
	}
}

// A write where nothing moved says so, rather than claiming a change.
func TestNothingMovedIsSaidPlainly(t *testing.T) {
	got := tool.Changed("Changed the reminder",
		tool.Change{What: "the time", From: "9:00 am", To: "9:00 am"})

	if !strings.Contains(got.Content, "nothing had actually moved") {
		t.Errorf("content = %q", got.Content)
	}
}

// A deletion reports both counts, which is the listener's only check
// that one thing went and not all of them.
func TestADeletionReportsBothCounts(t *testing.T) {
	got := tool.Removed("Cancelled the reminder", 4, 3, "reminder")

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	for _, want := range []string{"were 4 reminders before", "there are 3 reminders now"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// Losing more than one is not a success. It is the failure this exists
// to catch, and it is called out rather than buried.
func TestLosingMoreThanOneIsNotQuietlyFine(t *testing.T) {
	got := tool.Removed("Cancelled the reminder", 4, 1, "reminder")

	if got.Outcome != "partial" {
		t.Errorf("outcome = %q, want partial", got.Outcome)
	}
	if !strings.Contains(got.Content, "3 fewer") {
		t.Errorf("content = %q, want it to say how many went", got.Content)
	}
}

// Singular and plural read correctly, since this is spoken aloud.
func TestCountsReadCorrectly(t *testing.T) {
	got := tool.Removed("Cancelled it", 1, 0, "reminder")
	if !strings.Contains(got.Content, "was 1 reminder before") {
		t.Errorf("content = %q, want the singular", got.Content)
	}
	if !strings.Contains(got.Content, "there are none now") {
		t.Errorf("content = %q, want none rather than zero", got.Content)
	}

	many := tool.Removed("Cancelled it", 3, 2, "memory")
	if !strings.Contains(many.Content, "were 3 memories before") {
		t.Errorf("content = %q, want the irregular plural", many.Content)
	}
}

// A write that did not survive being read back is a failure, whatever
// the write itself reported.
func TestAWriteThatDidNotTakeIsAFailure(t *testing.T) {
	got := tool.Unverified("Changing the reminder", "it still says 9:00 am")

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want failed", got.Outcome)
	}
	if !strings.Contains(got.Content, "Do not say it is done") {
		t.Errorf("content = %q", got.Content)
	}
}

// The answer is left alone when it already said what it owed.
func TestAnAnswerThatSaidItIsUntouched(t *testing.T) {
	owed := tool.Owing([]tool.Result{
		tool.Changed("Changed it", tool.Change{What: "the time", From: "9:00 pm", To: "10:00 pm"}),
	})
	said := "Moved it from 9:00 pm to 10:00 pm, sir."

	if got := tool.Ensure(said, owed); got != said {
		t.Errorf("answer was changed to %q", got)
	}
}

// And gets the missing fact added when it did not.
//
// Measured: handed "the time was 9:00 pm, is now 10:00 pm", the model
// said "Moved to 10:00 pm, sir." The before is the half the person
// cannot reconstruct, so it is the half enforced.
func TestADroppedFactIsAddedBack(t *testing.T) {
	owed := tool.Owing([]tool.Result{
		tool.Changed("Changed it", tool.Change{What: "the time", From: "9:00 pm", To: "10:00 pm"}),
	})

	got := tool.Ensure("Moved to 10:00 pm, sir.", owed)
	if !strings.Contains(got, "9:00 pm") {
		t.Errorf("answer = %q, want the before added back", got)
	}
	if !strings.HasPrefix(got, "Moved to 10:00 pm, sir.") {
		t.Errorf("answer = %q, want what the model said kept", got)
	}
}

// Counts are enforced the same way, since they are the listener's only
// check that one thing went and not several.
func TestADroppedCountIsAddedBack(t *testing.T) {
	owed := tool.Owing([]tool.Result{tool.Removed("Cancelled it", 4, 3, "reminder")})

	got := tool.Ensure("Cancelled, sir.", owed)
	if !strings.Contains(got, "4") || !strings.Contains(got, "3") {
		t.Errorf("answer = %q, want both counts", got)
	}
}

// A tool that promised nothing adds nothing.
func TestNothingOwedAddsNothing(t *testing.T) {
	owed := tool.Owing([]tool.Result{tool.OK("Listed them.")})
	if got := tool.Ensure("Here they are.", owed); got != "Here they are." {
		t.Errorf("answer = %q", got)
	}
}
