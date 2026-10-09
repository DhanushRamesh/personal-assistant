package conversation_test

import (
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/records/conversation"
)

// The assistant is told the time, the day and the date, because it knows
// none of them and will otherwise answer from whenever it was trained.
func TestNowCarriesTheWholeMoment(t *testing.T) {
	india := time.FixedZone("IST", 5*3600+1800)
	got := conversation.Now(time.Date(2026, 9, 26, 15, 42, 0, 0, india))

	for _, want := range []string{"3:42 pm", "Saturday", "26 September 2026", "IST+05:30"} {
		if !strings.Contains(got, want) {
			t.Errorf("Now is missing %q:\n%s", want, got)
		}
	}
}

// Told the time, it is also told to use it rather than guess.
func TestNowForbidsGuessing(t *testing.T) {
	got := conversation.Now(time.Now())

	if !strings.Contains(got, "never guess the date") {
		t.Errorf("Now does not forbid guessing:\n%s", got)
	}
}

// Midnight and noon are the two a twelve-hour clock gets wrong.
func TestNowGetsMidnightAndNoonRight(t *testing.T) {
	midnight := conversation.Now(time.Date(2026, 9, 26, 0, 5, 0, 0, time.UTC))
	if !strings.Contains(midnight, "12:05 am") {
		t.Errorf("midnight read as %q", midnight)
	}

	noon := conversation.Now(time.Date(2026, 9, 26, 12, 5, 0, 0, time.UTC))
	if !strings.Contains(noon, "12:05 pm") {
		t.Errorf("noon read as %q", noon)
	}
}

// TestTheClockSaysItBeatsTheConversation : A long conversation
// accumulates times the assistant once said, each right when it said
// it, and they sit nearer the end of the window than this line does.
//
// Measured in a chat of 638 messages: one answer went out seventeen
// minutes slow and every answer after it copied the one before and
// added a minute, one per question, while two hours went past. Asked
// whether it was sure, it said "that is what I was told" -- and it
// had been told the right time.
func TestTheClockSaysItBeatsTheConversation(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	said := conversation.Now(time.Date(2026, 10, 9, 15, 48, 0, 0, ist))

	for _, want := range []string{
		"3:48 pm on Friday 9 October 2026",
		"IST+05:30",
		// The three things that stop the drift, each of which the
		// old wording left to be inferred.
		"only clock you have",
		"stale",
		"never carry one forward",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the clock line never says %q:\n  %s", want, said)
		}
	}
}
