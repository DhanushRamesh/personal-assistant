package persona

import (
	"strings"
	"testing"
)

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
	const after = "It is twenty past four, sir."
	for _, said := range []string{"Thanks, Nova.", "Okay, Nova.", "Wait.", "Not now, Nova.", "Nova.", "hey Nova good morning", "I was just checking in."} {
		if !InPassing(said, after, "Nova") {
			t.Errorf("%q is not taken as said in passing", said)
		}
	}
	for _, said := range []string{"Yes, do it.", "Open Spotify.", "What's the time?", "okay open the terminal", "go to machines", ""} {
		if InPassing(said, after, "Nova") {
			t.Errorf("%q is taken as said in passing", said)
		}
	}
}

// An offer is answered by doing the thing offered. "Okay" after a
// question is consent, so it goes back to look at the tools; the same
// word after a statement is a shrug and does not.
//
// The fault: "Claude Code in Server has finished the GLiNER run, sir --
// shall I ask it to say the numbers?" was answered "Okay." and then
// "Very well, sir.", with nothing done and type_into_terminal among the
// tools the laptop had offered.
func TestAYesToAnOfferIsNotInPassing(t *testing.T) {
	const offer = "Claude Code in Server has finished the GLiNER run, sir -- shall I ask it to say the numbers?"
	for _, said := range []string{"Okay.", "okay", "OK", "Alright.", "All right, Nova.", "Fine."} {
		if InPassing(said, offer, "Nova") {
			t.Errorf("%q answering an offer is taken as said in passing", said)
		}
		if !InPassing(said, "The run has finished, sir.", "Nova") {
			t.Errorf("%q after a statement is not taken as said in passing", said)
		}
	}

	// An acknowledgement is not consent, and neither is a refusal. Sending
	// those back to look costs a round and changes no answer.
	for _, said := range []string{"Thanks, Nova.", "Got it.", "Understood.", "No thanks.", "Never mind."} {
		if !InPassing(said, offer, "Nova") {
			t.Errorf("%q answering an offer is not taken as said in passing", said)
		}
	}

	// With nothing said before it, there is no offer to agree to.
	if !InPassing("Okay.", "", "Nova") {
		t.Error("\"Okay.\" with nothing before it is not taken as said in passing")
	}
}

func TestAgrees(t *testing.T) {
	for _, said := range []string{"yes", "Yes.", "Yes, do it.", "okay Nova", "Go ahead.", "yeah go ahead sir", "Sure, please.", "hey Nova yes please", "Tell it."} {
		if !Agrees(said, "Nova") {
			t.Errorf("%q is not taken as a bare yes", said)
		}
	}
	for _, said := range []string{"no", "yes but not now", "yes, tell Claude Code to commit", "what is step 4?", "", "Nova"} {
		if Agrees(said, "Nova") {
			t.Errorf("%q is taken as a bare yes", said)
		}
	}
}

func TestAsks(t *testing.T) {
	if !Asks(`Shall I tell it to move on to step 4?`) || !Asks(`Shall I? "`) {
		t.Error("a question at the end is not seen")
	}
	if Asks("Step 3 is in, sir.") || Asks("Is it? It is done.") {
		t.Error("a statement is taken for a question")
	}
}

// TestAWallOfProseIsCaught : He reads on a screen and does not read
// paragraphs. The manner says so already and lost.
func TestAWallOfProseIsCaught(t *testing.T) {
	// What it actually sent him, shortened: three paragraphs, the
	// first of five sentences.
	wall := "Claude Code rejected the proposed changes because most of them were rewording only. " +
		"Moving things from a check to a sentence does not sharpen them. " +
		"The three pieces it kept are worth lifting. " +
		"They add something new. " +
		"The four-way distinction is one of them."

	if !AWall(wall, Written) {
		t.Error("a paragraph of five sentences was not taken as a wall")
	}
	// Heard, not read. The sentence count is its rule instead.
	if AWall(wall, Spoken) {
		t.Error("a spoken answer was judged as though it were read")
	}

	// Shaped is fine, however long.
	shaped := "**It rejected most of it** — the changes were rewording only.\n\n" +
		"Three pieces are worth lifting:\n\n" +
		"- the four-way distinction\n- the recovery rules\n- the section structure\n\n" +
		"A better-written prompt is still an unmeasured one."
	if AWall(shaped, Written) {
		t.Error("a shaped answer was taken as a wall")
	}

	// A list first and a wall after it is the thing an answer-wide
	// check would pass.
	if !AWall(shaped+"\n\n"+wall, Written) {
		t.Error("a wall after a list went unnoticed")
	}

	// Three sentences is the manner's own line and is left alone.
	three := "It rejected most of it. The changes were rewording. Three pieces are worth lifting."
	if AWall(three, Written) {
		t.Errorf("three sentences were taken as a wall: %q", three)
	}

	// A table, a code block and a quote are not prose to count.
	for _, shape := range []string{
		"| a | b |\n| --- | --- |\n| 1 | 2 |\n| 3 | 4 |",
		"```\none. two. three. four. five.\n```",
		"> one. two. three. four. five.",
	} {
		if AWall(shape, Written) {
			t.Errorf("a marked block was counted as prose: %q", shape)
		}
	}
}

// TestAnAnswerWithNoShapeIsCaught : He reads on a screen and will not
// read a paragraph, however short it is.
//
// Asked when a film was out, it gave three countries and three dates in
// one paragraph -- a table written as a sentence. Three sentences, so
// the wall check passed it. Measured over his typed answers, 127 of 136
// carried no shape at all.
func TestAnAnswerWithNoShapeIsCaught(t *testing.T) {
	// What it actually sent him: 37 words, three sentences, no shape.
	flat := "Avengers: Doomsday releases December 18, 2026 in the United States — that is nearly " +
		"two months away. In India it releases December 17, a day earlier, and in the UK " +
		"December 16. Tickets are on sale now."
	if !AWall(flat, Written) {
		t.Error("an answer with three dates and no shape was not caught")
	}
	if AWall(flat, Spoken) {
		t.Error("a spoken answer was judged on how it looks")
	}

	// One bold phrase clears it. The bar is low on purpose.
	if AWall("**18 December 2026** in the US, a day later than India and two days after the UK. "+
		"Tickets are on sale now and it is nearly two months away from today.", Written) {
		t.Error("a bolded lead did not count as shape")
	}
	// So does a table or a list.
	for _, one := range []string{
		"| Country | Date |\n| --- | --- |\n| US | 18 Dec |\n| India | 17 Dec |\n| UK | 16 Dec |",
		"Released:\n\n- US, 18 December\n- India, 17 December\n- UK, 16 December",
		"## 18 December 2026\n\nThe United States gets it last of the three, sir.",
	} {
		if AWall(one, Written) {
			t.Errorf("a shaped answer was caught: %q", one)
		}
	}

	// Short answers are left alone, which is most of them.
	for _, one := range []string{
		"It is twenty past four, sir.",
		"Nothing in your diary tomorrow, sir.",
		"Thirteen, sir.",
	} {
		if AWall(one, Written) {
			t.Errorf("a short answer was caught: %q", one)
		}
	}
}

// TestALongAnswerNeedsMoreThanBold : One bolded word cleared the shape
// check and the answer still ran three paragraphs.
//
// Asked what falsifiability means, it replied in about a hundred and
// fifty words with "Falsifiability" bold at the front and nothing else
// to look at. No paragraph reached four sentences, so every check
// passed, and it was still three paragraphs of prose.
func TestALongAnswerNeedsMoreThanBold(t *testing.T) {
	long := "**Falsifiability** is the principle that a statement is scientifically meaningful " +
		"only if it can be proven false. A falsifiable claim makes a prediction specific enough " +
		"that reality could contradict it. It will rain tomorrow is falsifiable; if tomorrow " +
		"comes dry the claim is false. It might rain or it might not is not falsifiable because " +
		"no outcome can prove it wrong. Karl Popper developed this as a cornerstone of method: " +
		"a theory that cannot be falsified is not science. A claim that survives repeated " +
		"attempts becomes more credible, not because it is proven but because it withstood them."

	if !AWall(long, Written) {
		t.Error("a long answer with only a bold phrase was not caught")
	}
	// The same length with something to jump through is fine.
	if AWall(strings.Replace(long, "A falsifiable claim makes",
		"\n\n- A falsifiable claim makes", 1), Written) {
		t.Error("a long answer with a list was caught")
	}
	// And a short bolded answer is still fine, which is the common case.
	if AWall("**18 December 2026** in the US, a day after India and two after the UK, sir.", Written) {
		t.Error("a short bolded answer was caught")
	}
}

// TestADateLeadsOrItIsSentBack : The date is what was asked for, so it
// goes where he looks first.
//
// Asked when a song came out, it answered "**Something About Us** was
// released November 14, 2003, as a single from Discovery" -- the date
// behind the name he had just typed.
func TestADateLeadsOrItIsSentBack(t *testing.T) {
	buried := "**Something About Us** was released November 14, 2003, as a single from " +
		"*Discovery*. It was the sixth and final single off that album."
	if !AWall(buried, Written) {
		t.Error("a buried date was not caught")
	}
	if AWall(buried, Spoken) {
		t.Error("a spoken answer was judged on where its date sits")
	}

	// Leading with it, in any of the shapes that lead.
	for _, one := range []string{
		"## 14 November 2003\n\nThe sixth and final single from *Discovery*, sir.",
		"**14 November 2003** — the sixth and final single from *Discovery*, sir.",
		"- 14 November 2003, the sixth single\n- Used to promote *Interstella 5555*",
		"| Single | Date |\n| --- | --- |\n| Something About Us | 14 November 2003 |",
	} {
		if AWall(one, Written) {
			t.Errorf("an answer that leads with its date was caught: %q", one)
		}
	}

	// Not every number is a date. A year alone, a weekday, a time.
	for _, one := range []string{
		"It came out in 2003, sir.",
		"New episodes air on Wednesdays, sir.",
		"It is twenty past four, sir.",
	} {
		if AWall(one, Written) {
			t.Errorf("something that is not a date was caught: %q", one)
		}
	}
}

// TestAnOpeningThatAgreesComesOff : Sent back to shape an answer, it
// replied "You're right. Here it is shaped:" and then the answer.
//
// The reshaping is between the server and the model. He asked when his
// birthday was.
func TestAnOpeningThatAgreesComesOff(t *testing.T) {
	for _, c := range []struct{ said, want string }{
		{"You're right. Here it is shaped:\n\n## Thursday 22 October\n\nYour birthday.",
			"## Thursday 22 October\n\nYour birthday."},
		{"Understood, sir. The dentist is at nine on Tuesday.", "The dentist is at nine on Tuesday."},
		{"Quite right — it is twenty past four, sir.", "it is twenty past four, sir."},
		{"Here it is, shorter: **14 November 2003**.", "**14 November 2003**."},
	} {
		got, took := Preambled(c.said)
		if !took || got != c.want {
			t.Errorf("Preambled(%q) = %q, %v; want %q", c.said, got, took, c.want)
		}
	}

	// An agreement that is the whole answer is the answer.
	for _, said := range []string{
		"You are right, sir.",
		"Understood, sir.",
		"Noted, sir.",
		"It is twenty past four, sir.",
		"Here is your diary for tomorrow, sir: the dentist at nine.",
	} {
		if got, took := Preambled(said); took {
			t.Errorf("Preambled(%q) took something, leaving %q", said, got)
		}
	}
}
