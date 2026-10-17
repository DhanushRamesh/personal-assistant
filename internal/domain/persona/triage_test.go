package persona

import "testing"

// TestTheClassificationComesOff : both of the real ones, and the rest
// of each answer kept whole.
func TestTheClassificationComesOff(t *testing.T) {
	for _, c := range []struct {
		what   string
		said   string
		expect string
	}{
		{
			what: "the comparison, spoken five minutes after the ban went live",
			said: "This is a question about the world rather than about you — a comparison " +
				"of two countries' wealth. I can answer it from what I know.\n\n" +
				"Syria's economy has been severely damaged by years of civil war. India is wealthier, sir.",
			expect: "Syria's economy has been severely damaged by years of civil war. India is wealthier, sir.",
		},
		{
			what: "the backups answer",
			said: "This is a question about how to think about something — backups, in this case — " +
				"not a request to do anything with your own backups or devices. It is a question " +
				"about the world, and I can answer it.\n\n" +
				"Backups serve one purpose: to let you recover from loss.",
			expect: "Backups serve one purpose: to let you recover from loss.",
		},
		{
			what:   "one paragraph, so the first sentence goes instead",
			said:   "This is a question about the world. Damascus, sir.",
			expect: "Damascus, sir.",
		},
	} {
		got, took := Triaged(c.said)
		if !took {
			t.Errorf("%s: nothing was taken off", c.what)
			continue
		}
		if got != c.expect {
			t.Errorf("%s:\n  got  %q\n  want %q", c.what, got, c.expect)
		}
	}
}

// An answer that simply begins with "This is" is not a classification,
// and neither is one that mentions a question later on. Taking a
// sentence off a real answer is the only way this can do harm.
func TestARealAnswerIsLeftAlone(t *testing.T) {
	for _, said := range []string{
		"This is your third late night this week, sir.",
		"That is the Vandalur road, sir.",
		"Damascus is the capital of Syria, sir.",
		"I cannot answer that question, sir.",
		"Yes, sir.",
		"",
		"   ",
	} {
		if got, took := Triaged(said); took {
			t.Errorf("took the opening off a real answer %q, leaving %q", said, got)
		}
	}
}

// And an answer that is nothing but the classification keeps what it
// has, because there is no answer underneath to keep instead.
func TestNothingUnderneathIsLeftAlone(t *testing.T) {
	only := "This is a question about the world."
	if got, took := Triaged(only); took {
		t.Errorf("emptied an answer that had nothing else in it, leaving %q", got)
	}
}

// Thanks, a hold or a hello is said in passing; a command, a question or
// a yes never is, however short.
func TestInPassing(t *testing.T) {
	for _, said := range []string{"Thanks, Nova.", "Okay, Nova.", "Wait.", "Not now, Nova.", "Nova.", "hey Nova good morning", "I was just checking in."} {
		if !InPassing(said, "Nova") {
			t.Errorf("%q is not taken as said in passing", said)
		}
	}
	for _, said := range []string{"Yes, do it.", "Open Spotify.", "What's the time?", "okay open the terminal", "go to machines", ""} {
		if InPassing(said, "Nova") {
			t.Errorf("%q is taken as said in passing", said)
		}
	}
}
