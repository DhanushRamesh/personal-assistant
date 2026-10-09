package job_test

import (
	"testing"

	"personal-assistant/internal/domain/job"
)

// TestAPatternIsASourceLikeAnyOther : the whole claim of the design.
// If a pattern leaf lowers to an ordinary gate then the grouping,
// the operators, the describing and the fetching all work on it
// without knowing it exists, and none of them were changed.
func TestAPatternIsASourceLikeAnyOther(t *testing.T) {
	w := job.Where{Pattern: "reaching", With: map[string]any{"tries": 2}}
	g, err := w.Gate()
	if err != nil {
		t.Fatalf("a pattern should be a condition now: %v", err)
	}
	if g.Source != "pattern.reaching tries=2" {
		t.Errorf("source: %q", g.Source)
	}
	// Asked for and nothing said about it means it holds.
	if g.Op != job.Is || g.Value != job.Yes {
		t.Errorf("a bare pattern should mean yes, got %q %q", g.Op, g.Value)
	}
}

// TestTheBlanksAreOrderedTheSameEveryTime : a map has no order, and
// two rules asking the same thing must name the same source or each
// is worked out twice and kept as two.
func TestTheBlanksAreOrderedTheSameEveryTime(t *testing.T) {
	one, _ := job.Where{Pattern: "reaching",
		With: map[string]any{"tries": 2, "within": "2h", "among": "amma"}}.Gate()
	two, _ := job.Where{Pattern: "reaching",
		With: map[string]any{"among": "amma", "within": "2h", "tries": 2}}.Gate()
	if one.Source != two.Source {
		t.Errorf("the same pattern named two ways:\n  %q\n  %q", one.Source, two.Source)
	}
	if one.Source != "pattern.reaching among=amma tries=2 within=2h" {
		t.Errorf("source: %q", one.Source)
	}
}

// TestABlankWithASpaceInIt : the first rule written against real
// data broke on a bluetooth device called "bike helmet". A source
// is one string split on spaces, so a free-text blank has to be
// escaped or the second word is read as a blank of its own.
func TestABlankWithASpaceInIt(t *testing.T) {
	g, err := job.Where{Pattern: "still",
		With: map[string]any{"is": "bike helmet", "on": "bluetooth.connected"}}.Gate()
	if err != nil {
		t.Fatalf("lowering it: %v", err)
	}
	name, with, err := job.Pattern(g.Source)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if name != "still" {
		t.Errorf("name: %q", name)
	}
	if with["is"] != "bike helmet" {
		t.Errorf("the value came back as %q", with["is"])
	}
	if with["on"] != "bluetooth.connected" {
		t.Errorf("the other blank came back as %q", with["on"])
	}
}

// TestAPatternReadsBackBothWays : the source is the truth and is
// nobody's sentence.
func TestAPatternReadsBackBothWays(t *testing.T) {
	for _, c := range []struct {
		w    job.Where
		want string
	}{
		{job.Where{Pattern: "reaching"}, "reaching"},
		{job.Where{Pattern: "reaching", With: map[string]any{"tries": 2}}, "reaching tries=2"},
		{job.Where{Pattern: "reaching", Op: "==", Value: "no"}, "not reaching"},
	} {
		if got := c.w.Describe(); got != c.want {
			t.Errorf("described %q, wanted %q", got, c.want)
		}
	}
}

// TestAPatternCannotWakeARule : it is worked out from events that
// already landed, so a rule whose only condition was a pattern
// would be woken by nothing and wait for ever. Refused where it can
// be seen rather than left to never fire.
func TestAPatternCannotWakeARule(t *testing.T) {
	g, _ := job.Where{Pattern: "reaching"}.Gate()
	if g.Arrives() {
		t.Error("a pattern does not arrive, so it cannot be a hook")
	}
}

// TestThePatternIsReadBackFromItsSource : whatever works a shape out
// is handed the gate, and has to get the name and the blanks back.
func TestThePatternIsReadBackFromItsSource(t *testing.T) {
	name, with, err := job.Pattern("pattern.reaching among=amma tries=2")
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if name != "reaching" {
		t.Errorf("name: %q", name)
	}
	if with["tries"] != "2" || with["among"] != "amma" {
		t.Errorf("blanks: %v", with)
	}
	if _, _, err := job.Pattern("call.missed"); err == nil {
		t.Error("that is not a pattern and should not read as one")
	}
}
