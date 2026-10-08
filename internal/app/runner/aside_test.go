package runner

import (
	"testing"
	"time"
)

// TestTheGapIsAMinimumNotAPause : Rounds are about three seconds
// apart on their own, so most of the time the spacing costs nothing.
//
// It exists for the round that comes back fast, where two sentences
// would otherwise arrive as one run-on and sound like a fault.
func TestTheGapIsAMinimumNotAPause(t *testing.T) {
	var s speech

	// Nothing said yet: the first sentence waits for nothing.
	if wait := s.after(AsideGap); wait > 0 {
		t.Errorf("the first aside was made to wait %v", wait)
	}

	// One said three seconds ago, which is the usual spacing of
	// rounds. The gap has already passed, so it adds nothing.
	s.done = time.Now().Add(-3 * time.Second)
	if wait := s.after(AsideGap); wait > 0 {
		t.Errorf("a round three seconds later was made to wait %v", wait)
	}

	// One said a moment ago, which is the case this is for.
	s.done = time.Now().Add(-100 * time.Millisecond)
	wait := s.after(AsideGap)
	if wait <= 0 {
		t.Fatal("two sentences a tenth of a second apart were not spaced")
	}
	if wait > AsideGap {
		t.Errorf("waiting %v, longer than the gap itself", wait)
	}
}

// The answer gets a longer beat than the narration in front of it, so
// it is audibly a different kind of thing.
func TestTheAnswerWaitsLongerThanAnAside(t *testing.T) {
	if AnswerGap <= AsideGap {
		t.Errorf("AnswerGap %v is not longer than AsideGap %v; the answer "+
			"would run on from the narration", AnswerGap, AsideGap)
	}

	var s speech
	s.done = time.Now()
	aside, answer := s.after(AsideGap), s.after(AnswerGap)
	if answer <= aside {
		t.Errorf("answer waits %v, aside waits %v", answer, aside)
	}
}
