package persona

import "testing"

// TestAnOfferIsToldFromAQuestion : The line the guard draws.
//
// An offer asks whether it should act. A question that is allowed asks
// what they meant, and the persona wants those: "Ask only where a wrong
// guess cannot be taken back, and then as one question they can answer
// yes or no."
//
// The offers here are real. The questions are the shapes that would be
// lost if the line were drawn at "ends with a question mark", which is
// why it is not drawn there.
func TestAnOfferIsToldFromAQuestion(t *testing.T) {
	offers := []string{
		"The Server terminal is done, sir. Shall I tell it to move on to step 4?",
		"The Jarvis Server terminal is done, sir — Claude Code shipped a clock fix, " +
			"but it broke on the first real question asked. Shall I tell it to keep going?",
		"The run has finished, sir -- shall I read the numbers out?",
		"That is everything from this morning. Would you like me to read the rest?",
		"Seven of them, sir. Do you want me to cancel the Tuesday one?",
		"Done, sir. Anything else?",
		"The report is written. Let me know if you would like it shortened.",
		"I have the numbers. Want me to go through them?",
	}
	for _, said := range offers {
		if !Offers(said) {
			t.Errorf("an offer went unseen: %q", said)
		}
	}

	questions := []string{
		"The diary has two of these, sir. Did you mean the Tuesday one?",
		"There is a Praveen and a Praveena, sir. Which did you mean?",
		"Nothing by that name, sir. Was it the one from the bank?",
		"Two cost the same, sir. Is the earlier one the one you want?",
		// Not a question at all, and the commonest shape of a good
		// answer: it says what it did.
		"Told it to keep going, sir.",
		"Seven events this week, sir, and the Tuesday one has moved.",
		"",
	}
	for _, said := range questions {
		if Offers(said) {
			t.Errorf("an honest question was taken for an offer: %q", said)
		}
	}
}

// TestAnOfferIsOnlyJudgedAtTheEnd : An offer made and then acted on in
// the same answer is not what this is for.
func TestAnOfferIsOnlyJudgedAtTheEnd(t *testing.T) {
	said := "You asked whether I should tell it to keep going. I have told it, sir."
	if Offers(said) {
		t.Errorf("an offer already acted on was counted: %q", said)
	}
}
