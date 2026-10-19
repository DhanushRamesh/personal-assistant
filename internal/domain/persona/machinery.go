package persona

import (
	"regexp"
	"strings"
)

// inside : The words that belong in its head and not in their ears.
//
// A whole word, so "toolbar" and "Toolkit" are left alone, and the
// plural with it. Deliberately narrow: this decides whether an answer
// is sent back to be written again, and a false positive costs a
// round trip on an answer that was fine.
var inside = regexp.MustCompile(`(?i)\btools?\b`)

// NamesMachinery : Whether an answer tells them how it is built.
//
// [machinery] in the manner forbids this, and the manner forbade it
// three other ways before that was written. It still happens: the ban
// went live on 11 October 2026 at 23:25 and on the 12th at 22:27 the
// owner was told "There is no tool I have that controls whether you
// shower", with the rule in the running binary.
//
// The eval that passed the ban asked the model with no tools offered.
// A real turn carries sixty of them and the model is thinking about
// them, which is exactly when it reaches for the word. An instruction
// has to win an argument against everything else in the prompt; a
// check afterwards does not have to win anything.
//
// Owner: *"tool is not a conversational way -- it can say like 'sir I
// am afraid I cannot do'."*
func NamesMachinery(answer string) bool {
	return inside.MatchString(strings.TrimSpace(answer))
}

// unsearched : The shapes of an answer that pushed a question back
// instead of looking it up.
//
// Not "I cannot" in general. Saying you cannot do a thing is right and
// the manner asks for it -- *"I cannot put a call on speaker from
// here, sir"*. What is banned is refusing to **tell** somebody
// something, which is never a limit: the web is a tool like any other,
// and "how do I check my card's points" is a question with an answer
// on it.
//
// The fault, 19 October 2026. Asked how to see the points on his HDFC
// card, it answered "I cannot see your credit card account or its
// balance from here, sir. That is something you would need to check
// through the HDFC Bank app or website". It read a question about how
// to do something as a request to do it. The send-back had already
// fired and told it to read its tools; it answered again without
// calling one. Owner: *"jarvis has no limit, it should never say it
// doesn't know, it should do a websearch"*.
//
// Narrow on purpose, like the ban on the word "tool": every one of
// these is about handing the question back, and none of them is about
// an action it cannot take.
var unsearched = regexp.MustCompile(`(?i)\b(` +
	`i cannot tell you|i can't tell you|i am not able to tell you|` +
	`i cannot see your|i can't see your|i do not have access|i don't have access|` +
	`you would need to (check|look|find|visit|log)|` +
	`you will have to (check|look|find|visit|log)|` +
	`you'll have to (check|look|find|visit|log)|` +
	`that is something you would need|` +
	`i have no information|i do not have information|i don't have information|` +
	`i am not sure how to|i do not know how to|i don't know how to|` +
	// The same thing said as an instruction rather than as a limit.
	// "so check JioHotstar directly for the latest schedule" is a
	// hand-back with no "I cannot" in front of it, and the first
	// version of this missed it entirely.
	`check [^.]{0,40}? directly|` +
	`for the (latest|current|up-to-date|exact) |` +
	`on (their|its|the) (website|app|site|page)|` +
	`visit (their|its|the)` +
	`)\b`)

// Unsearched : Whether an answer handed the question back rather than
// looking it up.
//
// A check on the finished answer, not a rule in front of it, for the
// reason [NamesMachinery] is one: the rule was in lookFirst already
// -- *"if you were about to say you cannot do something, that is a
// claim to check"* -- it fired on this very turn, and the answer came
// back the same. A sentence the server will not send does not have to
// win an argument.
func Unsearched(answer string) bool {
	answer = strings.TrimSpace(answer)
	return unsearched.MatchString(answer) && !referred.MatchString(answer)
}

// referred : Sending somebody to a person, which is not a search.
//
// The one honest hand-back. What a doctor, a lawyer or an accountant
// knows about this person is not written down anywhere, so "you would
// need to check with your doctor" is the right answer and searching
// instead would be worse than useless.
//
// A bank, an airline or a shop is not on this list and is not meant to
// be: their process is published, which is the whole of the credit
// card fault.
var referred = regexp.MustCompile(`(?i)\b(doctor|physician|GP|surgeon|dentist|` +
	`lawyer|solicitor|advocate|accountant|auditor|therapist|counsellor|counselor|` +
	`pharmacist|landlord|employer|insurer)\b`)
