package persona_test

import (
	"testing"

	"personal-assistant/internal/domain/persona"
)

// The sentences he was actually sent, and the ones that must not be
// mistaken for them.
func TestWhatCountsAsNamingItsMachinery(t *testing.T) {
	named := []string{
		"There is no tool I have that controls whether you shower.",
		"I have no tool that answers what you think about a character.",
		"I do not have a tool that answers what JARVIS stands for.",
		"The tools I have are for your life -- your calendar, reminders, messages.",
		"I have no tool for that, sir.",
	}
	for _, said := range named {
		if !persona.NamesMachinery(said) {
			t.Errorf("not caught: %q", said)
		}
	}

	// A round trip is the cost of a false positive, so the test is
	// narrow on purpose: a whole word, and nothing that merely
	// contains it.
	fine := []string{
		"I am afraid I cannot do that, sir.",
		"Your toolbar is on the left.",
		"The toolkit arrived this morning.",
		"Good morning, sir. You slept about eight hours.",
		"",
	}
	for _, said := range fine {
		if persona.NamesMachinery(said) {
			t.Errorf("wrongly caught: %q", said)
		}
	}
}

// TestHandingTheQuestionBackIsCaught : Asked how to see the points on
// his credit card, it told him to use the bank's app.
//
// The web answers "how do I" questions. Refusing to tell somebody
// something is never a limit; refusing to do something can be.
func TestHandingTheQuestionBackIsCaught(t *testing.T) {
	// What it actually sent him.
	for _, said := range []string{
		"I cannot see your credit card account or its balance from here, sir.",
		"That is something you would need to check through the HDFC Bank app or website.",
		"I cannot tell you how to check your points from here, sir.",
		"You will have to log in to their website, sir.",
		"I do not have access to that, sir.",
		"I don't know how to do that, sir.",
	} {
		if !persona.Unsearched(said) {
			t.Errorf("not caught: %q", said)
		}
	}

	// Saying you cannot do a thing is right, and the manner asks for
	// it. None of these is a refusal to tell him something.
	for _, said := range []string{
		"I cannot put a call on speaker from here, sir.",
		"I cannot send that from here, sir.",
		"There is no list called that, sir. Shall I make one?",
		"Your diary has nothing on Thursday, sir.",
		"It is twenty past four, sir.",
		"The reminder is set for nine, sir.",
	} {
		if persona.Unsearched(said) {
			t.Errorf("wrongly caught: %q", said)
		}
	}
}

// Sending somebody to a person is the one honest hand-back. What a
// doctor or a lawyer knows about this person is not written down, so
// searching instead would be worse than useless.
//
// A bank is not on that list and is not meant to be: its process is
// published, which is the whole of the credit card fault.
func TestSendingSomebodyToAPersonIsNotAHandBack(t *testing.T) {
	for _, said := range []string{
		"You would need to check with your doctor about that, sir.",
		"I cannot tell you whether that is safe; you would need to check with a pharmacist.",
		"That is something you would need to check with your accountant, sir.",
	} {
		if persona.Unsearched(said) {
			t.Errorf("sending him to a person was taken as a hand-back: %q", said)
		}
	}

	// And the fault itself still is one.
	for _, said := range []string{
		"That is something you would need to check through the HDFC Bank app or website.",
		"You would need to check with the airline, sir.",
	} {
		if !persona.Unsearched(said) {
			t.Errorf("a published process was taken as honest: %q", said)
		}
	}
}
