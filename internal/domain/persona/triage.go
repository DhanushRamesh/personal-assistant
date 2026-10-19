package persona

import (
	"regexp"
	"strings"
)

// triage : An opening that says what sort of question was asked.
//
// Narrow on purpose, and anchored to the start: this removes text the
// model wrote, and the cost of a false positive is a sentence of a
// real answer going missing. Every form here is one that was actually
// said.
var triage = regexp.MustCompile(`(?is)^\s*(?:this|that)\s+is\s+(?:a|not a|an)\s+question\b` +
	`|^\s*(?:this|that)\s+is\s+about\b` +
	`|^\s*(?:this|that)\s+(?:is|sounds like)\s+(?:a|an)\s+(?:request|comparison|general)\b`)

// Triaged : The answer with an opening that classified the question
// taken off, and whether anything was taken.
//
// The manner forbids this outright -- never say what kind of question
// you were asked, begin with the answer -- and the ban was in the
// running binary when, five minutes after it started, a spoken answer
// opened: "This is a question about the world rather than about you
// -- a comparison of two countries' wealth. I can answer it from what
// I know." The sentence the ban names, nearly word for word.
//
// So it is taken off rather than argued about, for the reason the
// machinery check exists: an instruction has to win against
// everything else in the prompt and a check afterwards does not.
//
// Only the opening paragraph, and only when something is left. The
// classification is always the first thing said -- it is how the
// model gets to the answer -- and an answer that is nothing but a
// classification has no answer in it to keep.
func Triaged(answer string) (string, bool) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" || !triage.MatchString(trimmed) {
		return answer, false
	}
	// The paragraph it opens with, which is where it lives. Falling
	// back to the first sentence when the whole answer is one
	// paragraph.
	rest := strings.TrimSpace(firstParagraphOff(trimmed))
	if rest == "" {
		rest = strings.TrimSpace(firstSentenceOff(trimmed))
	}
	if rest == "" {
		return answer, false
	}
	return rest, true
}

// firstParagraphOff : Everything after the first blank line.
func firstParagraphOff(text string) string {
	if i := strings.Index(text, "\n\n"); i >= 0 {
		return text[i+2:]
	}
	return ""
}

// sentenceEnd : A stop that ends a sentence, which is one followed by
// a space. "9 a.m." and "4.5 hours" end nothing.
var sentenceEnd = regexp.MustCompile(`[.!?]+\s+`)

// firstSentenceOff : Everything after the first sentence.
func firstSentenceOff(text string) string {
	if where := sentenceEnd.FindStringIndex(text); where != nil {
		return text[where[1]:]
	}
	return ""
}

// Sentences : How many an answer runs to, counted the way a listener
// hears them: a stop followed by a space. "9 a.m." ends nothing and
// neither does "four and a half hours".
func Sentences(answer string) int {
	n := 0
	for _, part := range sentenceEnd.Split(strings.TrimSpace(answer), -1) {
		if strings.TrimSpace(part) != "" {
			n++
		}
	}
	return n
}

// Aloud : The most sentences a spoken answer may run to.
//
// The manner says it in words and the words do not hold. The count
// went in on 14 October and that evening a spoken answer ran to six
// sentences and ninety-five words, with the rule verbatim in a prompt
// of twenty thousand characters and thirty-six thousand tokens of
// conversation behind it. One line about counting does not win that
// argument.
//
// Owner, on the answer: *"it is too long. I know it cannot be said in
// one line but it can be short."*
const Aloud = 2

// TooLong : Whether a spoken answer runs past the count.
//
// Only spoken. A reader takes an answer at their own speed, and the
// written manner lifts every limit on length deliberately.
func TooLong(answer string, how Reading) bool {
	return how == Spoken && Sentences(answer) > Aloud
}

// inPassing : What is said in passing, the assistant's name and a
// "hey" around it aside -- thanks, an acknowledgement, a hold, a hello or
// a goodbye. Never a yes: a yes to something offered is answered by doing
// it.
var inPassing = regexp.MustCompile(`^(?:thanks|thank you|thanks a lot|thank you so much|cheers|okay|ok|okay then|alright|all right|got it|understood|cool|great|nice|good|fine|perfect|wait|hold on|one moment|one second|not now|never mind|nevermind|nothing|forget it|leave it|no|no thanks|hello|hi|hey|good morning|good afternoon|good evening|good night|bye|goodbye|see you|i was just checking|just checking|i was just checking in)$`)

// agreeing : The in-passing words that mean yes when they answer a
// question.
//
// "Okay" alone is a shrug; "okay" after "shall I read it out?" is
// consent, and the word is the same either way. Only the plain yesses
// belong here: an acknowledgement -- got it, understood, cool -- is not
// an instruction even after a question.
var agreeing = regexp.MustCompile(`^(?:okay|ok|okay then|alright|all right|fine)$`)

// asking : Whether the assistant's last words put a question to them.
var asking = regexp.MustCompile(`\?\s*$`)

// InPassing : Whether what was said is said in passing, with nothing to
// look up or do, so its answer is not sent back to look at the tools,
// and no round is run beside it for that.
//
// before is the assistant's own last words, whatever kind of message
// carried them. When those ended in a question, an [agreeing] answer to
// it is an instruction and not in passing: it was consent to an offer
// the assistant made, and the thing offered still has to be done.
//
// Owner, 17 October 2026, asking for a faster round trip: of two hundred
// answers sent back to look, the commonest thrown away were "Here, sir."
// (18), "Of course, sir." (6), "Understood, sir." (4) and "You are
// welcome, sir." (3) -- answers to being thanked or called, where looking
// changes nothing and the round run beside it costs up to a second, or
// five when it reaches for a tool anyway. A command or a question is not
// in passing, however short: "open Spotify" was answered "I cannot" until
// it was sent back, which is what the send-back is for.
func InPassing(said, before, name string) bool {
	s := strings.ToLower(said)
	s = regexp.MustCompile(`[^a-z' ]+`).ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimPrefix(s, "hey ")
	if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, n+" "), " "+n))
		if s == n {
			return true
		}
	}
	if s == "" || !inPassing.MatchString(s) {
		return false
	}
	return !(agreeing.MatchString(s) && asking.MatchString(before))
}

// consenting : A bare yes, and nothing else -- the words a person
// answers an offer with when the offer says what is to be done.
var consenting = regexp.MustCompile(`^(?:(?:yes|yeah|yep|yup|ya|sure|ok|okay|alright|all right|fine|absolutely|of course|please)` +
	`(?: (?:yes|sir|please|do it|do that|go ahead|go on|proceed|carry on|tell it|do so))*` +
	`|go ahead|do it|do that|go on|proceed|carry on|go for it|please do|sounds good|tell it)(?: sir| please)*$`)

// Agrees : Whether what was said is a bare yes, the assistant's name
// and a "hey" around it aside.
//
// Owner, 19 October 2026, typing "yes" under "The Server terminal is
// done, sir... Shall I tell it to move on to step 4?": nothing told the
// model what the yes was for, it worked out the wrong terminal from the
// conversation and did nothing. A yes on its own carries no subject; the
// question it answers is the subject, and the runner hands it over.
func Agrees(said, name string) bool {
	s := strings.ToLower(said)
	s = regexp.MustCompile(`[^a-z' ]+`).ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimPrefix(s, "hey ")
	if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, n+" "), " "+n))
	}
	return s != "" && consenting.MatchString(s)
}

// Asks : Whether the assistant's words end by putting a question to them.
func Asks(said string) bool {
	return asking.MatchString(strings.TrimRight(said, ` "'”’)]`))
}

// Wall : How many sentences in a row make a paragraph a wall.
//
// The written manner says it already -- *"Never a wall of prose. More
// than about three sentences in a row is a wall"* -- and on 19 October
// 2026 it answered him in three paragraphs of five, six and four. He
// had asked in the desktop app, which is the written form, and said:
// *"im a person who doesnt like reading long paragraphs, i prefer short
// pointers and compact ones"*.
//
// Four, so the manner's own "about three" is what it counts. A count
// rather than the sentence, for the reason [Aloud] is a count: the
// sentence was in the running binary and lost, which makes seven.
const Wall = 4

// marked : A block that is already a list, a heading, a table, a quote
// or code, and so is not prose to count.
var marked = regexp.MustCompile(`(?m)^\s*(?:[-*+]\s|\d+[.)]\s|#{1,6}\s|>\s|\||` + "```" + `)`)

// AWall : Whether a written answer has a paragraph of prose in it that
// should have been a list.
//
// Per paragraph, not over the whole answer: a reply that opens with a
// list and then runs six sentences together is the thing being
// stopped, and an answer-wide check would pass it.
//
// Spoken answers are not read, they are heard, and [TooLong] is their
// rule. This never applies to them.
func AWall(answer string, how Reading) bool {
	if how != Written {
		return false
	}
	if Unshaped(answer) || Buried(answer) {
		return true
	}
	for _, block := range strings.Split(strings.TrimSpace(answer), "\n\n") {
		if strings.TrimSpace(block) == "" || marked.MatchString(block) {
			continue
		}
		if Sentences(block) >= Wall {
			return true
		}
	}
	return false
}

// Shapeless : How many words a written answer may run to with no shape
// at all.
//
// Thirty, because the answer that prompted this is thirty-seven: asked
// when a film was out, it gave three countries and three dates in one
// paragraph -- *"December 18, 2026 in the United States... In India it
// releases December 17... and in the UK December 16"* -- which is a
// table written as a sentence. Owner: *"it can say the date in header
// like in big font, and tables are easy to read"*.
//
// Measured over his typed answers: 127 of 136 carried no shape at all,
// so the written manner was not being followed on text in any
// meaningful sense. 44 of them are this long or longer.
const Shapeless = 30

// Dense : How many words a written answer may run to before a bolded
// phrase stops being shape enough.
//
// Eighty. Asked what falsifiability means, it answered in about a
// hundred and fifty words: three paragraphs of three sentences each,
// with the one word "Falsifiability" in bold at the front. Every check
// passed -- no paragraph reached four sentences, and the bold cleared
// [Shapeless] -- and it was still three paragraphs of prose, which is
// the thing he said he will not read.
//
// So the shape has to grow with the answer. Past this, a bold run is
// not enough on its own: it takes a list, a table or a heading, which
// is what makes a long answer skimmable rather than merely punctuated.
//
// Only nine of his 137 typed answers are this long, so it asks for
// more work on the few that need it and changes nothing for the rest.
const Dense = 80

// shaped : Anything that gives an answer a shape -- a heading, a list,
// a table, a quote, code, or bold on the words that carry it.
var shaped = regexp.MustCompile("(?m)^\\s*(?:[-*+]\\s|\\d+[.)]\\s|#{1,6}\\s|>\\s|\\||```)|\\*\\*\\S")

// skimmable : Shape a reader can jump through -- a heading, a list, a
// table. Bold alone is not on this list, which is the point of it.
var skimmable = regexp.MustCompile("(?m)^\\s*(?:[-*+]\\s|\\d+[.)]\\s|#{1,6}\\s|\\||```)")

// Unshaped : Whether a written answer ran as prose for its length.
//
// Two bars, because one was gamed. Past [Shapeless] it needs any shape
// at all, and one bolded phrase clears that. Past [Dense] it needs
// shape a reader can jump through, and a bolded phrase no longer
// counts.
func Unshaped(answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false
	}
	words := len(strings.Fields(answer))
	switch {
	case words >= Dense:
		return !skimmable.MatchString(answer)
	case words >= Shapeless:
		return !shaped.MatchString(answer)
	}
	return false
}

// dated : A real date in an answer -- a day beside a month, either way
// round. Not a year on its own and not a weekday: "Wednesdays" is what
// he was given in place of a date, and "2003" is not one either.
var dated = regexp.MustCompile(`(?i)\b(\d{1,2}(st|nd|rd|th)? (January|February|March|April|May|June|July|` +
	`August|September|October|November|December)|(January|February|March|April|May|June|July|` +
	`August|September|October|November|December) \d{1,2})\b`)

// bolded : A run of bold text, for asking what the bold is actually on.
var bolded = regexp.MustCompile(`\*\*([^*]+)\*\*`)

// stands : A line that is itself something to look at -- a heading, a
// list item or a table row.
var stands = regexp.MustCompile("^\\s*(?:#{1,6}\\s|[-*+]\\s|\\d+[.)]\\s|\\|)")

// Buried : Whether a written answer carries a date and leaves it in
// the middle of a sentence.
//
// Asked when a song came out, it answered "**Something About Us** was
// released November 14, 2003, as a single from Discovery" -- the date
// he came for sitting in prose, behind the name he had just typed.
// Owner: *"i want the header"*.
//
// The test is where the date is, not whether the answer has a shape.
// The first version asked only whether the answer opened with
// something to look at, and this passed it: it opens with the song in
// bold. A date in a heading, in a bold run, in a list item or in a
// table row is where he looks; anywhere else it is buried.
//
// 17 of his 141 typed answers carry a date and all but one bury it.
func Buried(answer string) bool {
	answer = strings.TrimSpace(answer)
	if answer == "" || !dated.MatchString(answer) {
		return false
	}
	for _, line := range strings.Split(answer, "\n") {
		if stands.MatchString(line) && dated.MatchString(line) {
			return false
		}
	}
	for _, run := range bolded.FindAllStringSubmatch(answer, -1) {
		if dated.MatchString(run[1]) {
			return false
		}
	}
	return true
}

// preamble : An opening that acknowledges a note the server sent it,
// rather than answering.
//
// Sent back to shape an answer, it replied "You're right. Here it is
// shaped:" and then the answer. The reshaping is between the server
// and the model; he asked when his birthday was. The same fault as
// "Understood, sir. I will read both lists of tools", which was the
// look-first note read back to him as a promise.
//
// Narrow, and only where an answer follows: "You are right, sir" on
// its own is an answer, and so is "Noted, sir". What is taken off is a
// preamble with the real answer behind it.
var preamble = regexp.MustCompile(`(?is)^\s*(?:` +
	`you(?:'re| are)\s+right|quite\s+right|understood|apologies|my\s+apologies|` +
	`here\s+(?:it\s+is|you\s+are)|here(?:'s| is)\s+(?:it|that)` +
	// Stops at the first thing that ends a clause, never past it: a
	// greedy run swallowed "Here it is, shorter: **14 November 2003**"
	// whole and left nothing, which reads from outside as no match.
	`)\b[^.!?:\n` + "\u2014" + `]*(?:[.!?:]|` + "\u2014" + `|--)\s*`)

// Preambled : The answer with an opening that agreed with a note taken
// off, and whether anything was taken.
//
// Taken off rather than sent back, as [Triaged] is: the answer behind
// it is sound and a round trip to remove a clause is a round trip
// nobody is waiting through.
func Preambled(answer string) (string, bool) {
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" || !preamble.MatchString(trimmed) {
		return answer, false
	}
	// Twice over where there are two: it answered "You're right. Here
	// it is shaped:" and one pass left the second half standing.
	rest := trimmed
	for i := 0; i < 3 && preamble.MatchString(rest); i++ {
		rest = strings.TrimSpace(preamble.ReplaceAllString(rest, ""))
	}
	if rest == "" || rest == trimmed {
		// The whole answer was the agreement, so it is the answer.
		return answer, false
	}
	return rest, true
}
