package vocabulary_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// Names are taken and ordinary words are not. Every reminder title begins
// with a verb -- Call, Check, Watch -- and priming those buys nothing.
func TestOnlyWhatLooksLikeAName(t *testing.T) {
	got := vocabulary.Found([]string{
		"Watch the movie Cars",
		"Look into GitLab Merge Requests",
		"Check the metrics",
		"Integrate smartwatch",
		"Call BMRs today",
	})

	for _, want := range []string{"Cars", "GitLab", "Merge", "Requests", "BMRs"} {
		if !has(got, want) {
			t.Errorf("%q was not taken, from %v", want, got)
		}
	}
	// First words, and anything uncapitalised.
	for _, unwanted := range []string{"Watch", "Look", "Check", "Call", "metrics", "smartwatch"} {
		if has(got, unwanted) {
			t.Errorf("%q was taken and should not have been", unwanted)
		}
	}
}

// A word in capitals throughout counts wherever it stands, because that is
// what an acronym looks like and the position rule would miss one at the
// front.
func TestCapitalsThroughoutCountAnywhere(t *testing.T) {
	got := vocabulary.Found([]string{"BMRs are due", "GitLab is down"})
	for _, want := range []string{"BMRs", "GitLab"} {
		if !has(got, want) {
			t.Errorf("%q was not taken, from %v", want, got)
		}
	}
}

// The hand-written core wins over something heard once.
//
// "Call Alekia" is "Call Alekhya" misheard, and Alekhya is in the core. A
// prompt carrying both teaches the mistake.
func TestAWordCloseToACoreWordIsLeftOut(t *testing.T) {
	got := vocabulary.Prompt("Alekhya, Chintada", []string{"Alekia", "GitLab"})

	if strings.Contains(got, "Alekia") {
		t.Errorf("a mishearing of a core word was primed: %q", got)
	}
	if !strings.Contains(got, "GitLab") {
		t.Errorf("an unrelated word was dropped: %q", got)
	}
}

// Short words are compared exactly. At four characters two edits is half
// the word, and honest differences would start merging.
func TestShortWordsAreNotTreatedAsNear(t *testing.T) {
	got := vocabulary.Prompt("Cars", []string{"Bars"})
	if !strings.Contains(got, "Bars") {
		t.Errorf("two short, genuinely different words were merged: %q", got)
	}
}

// Nothing is primed twice, whatever its capitals.
func TestTheCoreIsNotRepeated(t *testing.T) {
	got := vocabulary.Prompt("chat, timer", []string{"Chat", "Timer", "Cars"})
	if strings.Count(strings.ToLower(got), "chat") != 1 {
		t.Errorf("chat appears more than once: %q", got)
	}
	if strings.Count(strings.ToLower(got), "timer") != 1 {
		t.Errorf("timer appears more than once: %q", got)
	}
}

// The core survives the budget and what was merely observed is what goes.
// Whisper truncates from the front, so a prompt over the limit loses the
// deliberate words rather than the incidental ones.
func TestTheCoreIsNeverCrowdedOut(t *testing.T) {
	core := strings.Repeat("deliberate, ", 40) + "last"
	got := vocabulary.Prompt(core, []string{"Alpha", "Beta", "Gamma"})

	if !strings.HasPrefix(got, core) {
		t.Error("the core was cut")
	}
	if len(got) > vocabulary.Budget {
		t.Errorf("prompt is %d characters, over the %d budget", len(got), vocabulary.Budget)
	}
}

// A long term near the front does not shut out the short ones behind it.
func TestALongTermDoesNotEndTheList(t *testing.T) {
	core := strings.Repeat("x", vocabulary.Budget-40)
	got := vocabulary.Prompt(core, []string{strings.Repeat("L", 60), "Cars"})

	if !strings.Contains(got, "Cars") {
		t.Errorf("a short term behind a long one was lost: %q", got[len(got)-30:])
	}
}

// has : Whether the list contains the word.
func has(list []string, want string) bool {
	for _, w := range list {
		if w == want {
			return true
		}
	}
	return false
}
