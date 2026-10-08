//go:build evals

// Asks a real model whether an answer carrying a list is one somebody
// could follow by ear.
//
// The failure it was written for, live on 3 October 2026. Asked "did
// anyone call me today", it gave seven calls, five clock times and five
// durations in one breath -- accurate, and unholdable. The one thing
// worth saying, an hour-and-a-half call at half past one in the
// morning, came last, after three six-second misses from the same
// person read out one by one.
//
// The rule against it was already in the persona and lost to its own
// escape hatch: name every item only when the list is what they asked
// for. "Did anyone call me today" was read as asking for the list.
package evals

import (
	"regexp"
	"strings"
	"testing"
)

// clock : A time of day in a spoken answer. One or two is orientation;
// five is a timetable.
var clock = regexp.MustCompile(`\b\d{1,2}[:.]\d{2}\s*(am|pm)?\b`)

// Spoken : How many clock times and how many words an answer carrying a
// list may have before nobody can follow it.
//
// Measured against the failure: the bad answer had five times and
// seventy-one words. A good one names the count, the shape, and the one
// thing worth hearing.
const (
	SpokenTimes = 2
	SpokenWords = 60
)

// calls : What the events tool actually returned that day, in the shape
// the runner hands over.
func calls() string {
	return "\n\nA tool has just run and returned this:\n" +
		"call.missed \"Amma\" at 10:42am, 10 seconds\n" +
		"call.missed \"Amma\" at 2:02pm, 9 seconds\n" +
		"call.missed \"Amma\" at 2:44pm, 6 seconds\n" +
		"call.received \"unknown\" at 12:13pm, 4 minutes\n" +
		"call.received \"unknown\" at 3:33pm, 9 minutes\n" +
		"call.received \"unknown\" at 3:45pm, 11 minutes\n" +
		"call.received \"unknown\" at 1:32am, 89 minutes\n"
}

// TestAnAnswerCarryingAListCanBeFollowedByEar : the count, the shape,
// and the one worth hearing -- not the table.
func TestAnAnswerCarryingAListCanBeFollowedByEar(t *testing.T) {
	env, system := setup(t)
	system += calls()

	var good int
	for _, ask := range []string{
		"did anyone call me today",
		"who called me today",
		"what calls did I get today",
	} {
		answer := say(t, env, system, ask)
		words := len(strings.Fields(answer))
		times := len(clock.FindAllString(strings.ToLower(answer), -1))

		switch {
		case times > SpokenTimes:
			t.Errorf("%-30q read out %d clock times, want at most %d:\n    %s",
				ask, times, SpokenTimes, answer)
		case words > SpokenWords:
			t.Errorf("%-30q ran to %d words, want at most %d:\n    %s",
				ask, words, SpokenWords, answer)
		case !strings.Contains(strings.ToLower(answer), "seven"):
			// The count is the one number that belongs in every
			// answer of this shape: it is what "did anyone" asks.
			t.Errorf("%-30q never said how many there were:\n    %s", ask, answer)
		default:
			good++
		}
	}

	t.Logf("answers a person could follow: %d of 3", good)
}
