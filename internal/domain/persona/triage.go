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
