package tool_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// TestFourDeletionsOweOneCount : Removing four things owes the net change,
// not four overlapping pairs of numbers.
//
// Unmerged they cascade. The answer said eight and four, which satisfied
// neither "8 and 7" nor "7 and 6"; appending the first supplied the seven
// that made the second match, and so on, and the person heard: the calendar
// had 8 events and now has 4. There were 8 before and 7 now. There were 7
// before and 6 now. There were 6 before and 5 now.
func TestFourDeletionsOweOneCount(t *testing.T) {
	var owed []tool.Owed
	for _, n := range [][2]int{{8, 7}, {7, 6}, {6, 5}, {5, 4}} {
		owed = append(owed, tool.Owing([]tool.Result{tool.Removed("Taken out", n[0], n[1], "event")})...)
	}
	if len(owed) != 4 {
		t.Fatalf("expected four obligations before merging, got %d", len(owed))
	}

	merged := tool.Merged(owed)
	if len(merged) != 1 {
		t.Fatalf("four deletions of one kind should owe one count, got %d", len(merged))
	}
	if merged[0].Tally.Before != 8 || merged[0].Tally.After != 4 {
		t.Errorf("net change = %d -> %d, want 8 -> 4",
			merged[0].Tally.Before, merged[0].Tally.After)
	}

	// An answer that already states the net change owes nothing further.
	said := "All four are removed, sir. The calendar had 8 events and now has 4."
	if got := tool.Ensure(said, "", merged); got != said {
		t.Errorf("a correct answer was added to:\n%s", got)
	}

	// One that states neither number is told both, once.
	bare := tool.Ensure("They are gone, sir.", "", merged)
	if strings.Count(bare, "before") != 1 {
		t.Errorf("expected exactly one count sentence:\n%s", bare)
	}
	if !strings.Contains(bare, "8") || !strings.Contains(bare, "4") {
		t.Errorf("the net change is missing:\n%s", bare)
	}
}

// TestCountsOfDifferentThingsStaySeparate : Reminders and events are not
// the same tally and must not be folded together.
func TestCountsOfDifferentThingsStaySeparate(t *testing.T) {
	owed := append(
		tool.Owing([]tool.Result{tool.Removed("Taken out", 8, 7, "event")}),
		tool.Owing([]tool.Result{tool.Removed("Taken out", 3, 2, "reminder")})...)

	if got := len(tool.Merged(owed)); got != 2 {
		t.Errorf("two kinds of thing owe two counts, got %d", got)
	}
}
