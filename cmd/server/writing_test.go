package main

import (
	"strings"
	"testing"

	"personal-assistant/internal/domain/job"
)

// TestARuleIsToldWhatAnOrdinaryDayLooksLike : A rule decides whether
// to interrupt somebody, and the same thing is worth saying at one
// hour and not at another. Nothing else in a write step knows that.
func TestARuleIsToldWhatAnOrdinaryDayLooksLike(t *testing.T) {
	const usually = "Returns home around nine in the evening. Usually."

	with := writing("say if anything is amiss", []job.Ran{}, "", usually)
	if !strings.Contains(with, usually) {
		t.Error("the rule was not given their ordinary day")
	}
	if !strings.Contains(with, "never say they are late by it") {
		t.Error("nothing stops the rule reading the routine back to them")
	}

	// Without one -- the first week, or a description that has not
	// been written -- a rule writes what it would have written
	// before any of this existed.
	without := writing("say if anything is amiss", []job.Ran{}, "", "")
	if strings.Contains(without, "ordinary day looks like") {
		t.Error("a rule with no description was told about one anyway")
	}
}

// TestARuleAsksWhetherItIsWorthSaying : owner -- *"if the model is
// going to comment, or say something, it should ask itself a
// question, whether it is actually useful, or does it make sense.
// Same goes for rule writing too."*
//
// The bar a rule clears to exist and the bar one firing clears to be
// heard are the same two questions, and they have to be asked in
// both places or the careful one is wasted.
func TestARuleAsksWhetherItIsWorthSaying(t *testing.T) {
	got := writing("say what was found", []job.Ran{}, "", "")

	for _, want := range []string{"Is it worth their attention?", "Does it make sense?"} {
		if !strings.Contains(got, want) {
			t.Errorf("a rule about to speak was not asked %q:\n%s", want, got)
		}
	}
	// And the deciding comes before the phrasing. A model told at
	// length how to word something will word something.
	if deciding, wording := strings.Index(got, "deciding whether to interrupt"),
		strings.Index(got, "must not be the first thing in the sentence"); deciding > wording {
		t.Error("it was told how to phrase a sentence before it was told it might not write one")
	}
}

// TestARemarkIsNotHeldToTheUsefulBar : the reason noticing.Purpose
// was split in the first place. "A thing they can do nothing about
// is not worth an interruption" is what keeps warnings scarce, and
// it forbids every remark there is -- so a single bar at the moment
// of speaking would have undone the companion the day it was added.
func TestARemarkIsNotHeldToTheUsefulBar(t *testing.T) {
	got := writing("say something about what they have been listening to", []job.Ran{}, "", "")
	if !strings.Contains(got, "useful is the wrong test") {
		t.Errorf("a rule written to remark was held to the bar for warnings:\n%s", got)
	}
	if !strings.Contains(got, "true and particular to them") {
		t.Errorf("nothing says what a remark earns its place by:\n%s", got)
	}
}

// TestSilenceIsNotRationed : an interruption nobody needed is not
// improved by being rare, and a thing worth saying is not spoiled by
// having said something an hour ago.
func TestSilenceIsNotRationed(t *testing.T) {
	got := writing("say what was found", []job.Ran{}, "", "")
	if !strings.Contains(got, "Do not weigh how long it is since you last spoke") {
		t.Errorf("nothing stops a rule treating its own quiet as a reason to speak:\n%s", got)
	}
}
