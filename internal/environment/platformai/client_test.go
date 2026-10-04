package platformai

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
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
		{environment.PurposeProfile, "high"},
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
// meant it, and a purpose asking for medium must not quieten a
// server told to think hard about everything.
func TestItNeverReasonsLessThanAsked(t *testing.T) {
	if got := thinking("high", environment.PurposeRemark); got != "high" {
		t.Errorf("a global high was reduced to %q", got)
	}
	if got := thinking("low", environment.PurposeProfile); got != "high" {
		t.Errorf("a purpose asking for more got %q", got)
	}
	if got := thinking("medium", environment.PurposeChat); got != "medium" {
		t.Errorf("an ordinary turn lost its configured %q", got)
	}
}
