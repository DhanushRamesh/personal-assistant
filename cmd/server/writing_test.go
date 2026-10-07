package main

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
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
