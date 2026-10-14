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
