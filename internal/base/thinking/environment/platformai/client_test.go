package platformai

import (
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// TestOnlyJudgementReasons : Owner: *"use a high reasoning model
// for this"*, of the greeting. It already ran on the same model as
// everything else; what it did not have was permission to think.
//
// Given per purpose rather than globally, because the two are
// opposite requirements. A greeting reads a diary against a day of
// positions and decides what one sentence should be about; "what
// is the time" has one answer, and reasoning first spends seconds
// of a thirty-second turn to arrive at it.
func TestOnlyJudgementReasons(t *testing.T) {
	for _, c := range []struct {
		purpose environment.Purpose
		want    string
	}{
		// Not the profile pass either, from 19 October 2026: at
		// "low" it still failed 43 calls of 189, spending 1,535
		// tokens and 35 seconds each before returning nothing, and
		// the owner's description of himself stopped rebuilding.
		{environment.PurposeProfile, ""},
		// Not the greeting, though it wants it most: 29 to 46
		// seconds against a five-second budget means a better
		// greeting written every time and heard none of the time.
		{environment.PurposeGreeting, ""},
		{environment.PurposeRemark, ""},
		{environment.PurposeChat, ""},
		{environment.PurposeTitle, ""},
	} {
		if got := thinking("", c.purpose); got != c.want {
			t.Errorf("%q thinks %q, want %q", c.purpose, got, c.want)
		}
	}
}

// TestItNeverReasonsLessThanAsked : Somebody who set it globally
// meant it, and no purpose may quieten a server told to think hard
// about everything.
//
// No purpose asks for more any more, so the other half of this -- a
// purpose raising a quiet configuration -- is held by
// TestNoPurposeAsksForMoreThanIsConfigured below.
func TestItNeverReasonsLessThanAsked(t *testing.T) {
	if got := thinking("high", environment.PurposeRemark); got != "high" {
		t.Errorf("a global high was reduced to %q", got)
	}
	if got := thinking("medium", environment.PurposeChat); got != "medium" {
		t.Errorf("an ordinary turn lost its configured %q", got)
	}
	if got := thinking("high", environment.PurposeProfile); got != "high" {
		t.Errorf("the profile pass lost a global high, now %q", got)
	}
}

// TestNoPurposeAsksForMoreThanIsConfigured : The profile pass was the
// last one, and raising it is what stopped the description rebuilding.
//
// Reasoning and the answer come out of one three-thousand-token
// budget, and the gateway has no field to raise it. A purpose put
// back into Hard is a purpose that can spend the whole budget
// thinking and return nothing, so it has to be argued for here.
func TestNoPurposeAsksForMoreThanIsConfigured(t *testing.T) {
	for purpose, want := range Hard {
		t.Errorf("%q asks for %q more than the configuration; "+
			"reasoning and the answer share one budget, so say here why it can afford to",
			purpose, want)
	}
}
