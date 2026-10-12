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
