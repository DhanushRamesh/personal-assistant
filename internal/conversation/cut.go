package conversation

import (
	"fmt"

	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// CutOff : That the recording stopped while the person was still speaking.
//
// Home Assistant stops listening after a fixed number of seconds regardless
// of whether anything has been finished. What arrives is therefore the
// beginning of a sentence presented as though it were all of one, and
// nothing in the words themselves says so -- which is exactly the situation
// an assistant answers confidently and wrongly.
//
// The seconds are named because the person cannot see the limit and will
// otherwise think they were interrupted for some reason of their own.
func CutOff(seconds float64) string {
	return prompt.Text(
		fmt.Sprintf(
			"The recording stopped after %.0f seconds because that is as long as it is allowed to run, not because the person stopped speaking.",
			seconds),
		"What you have been given is the beginning of what they were saying and not the whole of it, and the end of it is missing even though it reads as though it is complete.",
		"Do not act on it as an instruction and do not answer it as a question.",
		"The last item in a list that was being read out is the most likely thing to be wrong, because it is the one that was still being said.",
		"Say what you have so far, briefly, so they can see where it stopped, and ask them to go on from there.",
		"Keep it short: they are mid-sentence and waiting.",
		"This is a limit in the equipment and not a mistake they made, so do not apologise for them or ask them to speak faster or more briefly.",
		"If what arrived does happen to be complete in itself, answer it as normal and say nothing about the recording.",
	)
}
