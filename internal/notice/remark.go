package notice

import (
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// Silence : What the model answers when there is nothing worth
// saying.
//
// A word rather than an empty reply, because a model asked for
// nothing tends to produce something anyway -- an apology, or a
// sentence about having nothing to say, which would then be spoken
// aloud. Given something to return it returns it.
const Silence = "NOTHING"

// Nothing : Whether the model declined.
//
// Generous about how it is written. The answer is thrown away either
// way, and the cost of reading a refusal as a remark is that the
// refusal is read out loud.
func Nothing(said string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(said))
	trimmed = strings.Trim(trimmed, ".!\"' ")
	return trimmed == Silence
}

// Remark : What the model is asked, about one thing that has to be
// said.
//
// It is given the thing and nothing else about what kind of thing
// it is -- because nothing here knows. The judgement it used to be
// asked for is gone with the remarks that needed it: a device marks
// something to be said because of what it is, and a model weighing
// whether a battery at five per cent is worth mentioning is a model
// that will one day decide it is not.
//
// What is left is the harder half, and the reason a model is asked
// at all rather than a sentence being assembled here: working out,
// from a label and whatever the sender put beside it, what about
// this matters to the person hearing it.
func Remark(c Candidate, now time.Time) string {
	return prompt.Block(
		prompt.Text(
			"You are in the room with them and their own device has reported something it marked as needing to be said now.",
			"Say it, in one sentence.",
		),
		prompt.Text(
			"Here it is, as the device reported it:",
		),
		prompt.Lines(append([]string{
			"- the thing: " + c.Label,
			"- when: " + c.At.Format("Monday 2 January at 3:04 pm"),
		}, beside(c)...)...),
		prompt.Text(
			"Whether this is worth saying is not a judgement you are being asked to review; it has been made.",
			"Say it plainly and say what they would want to do about it. Do not soften it into a remark, and do not answer with "+Silence+".",
		),
		prompt.Text(
			"Never guess what the thing is. If its name does not tell you what it was, say only what the device said.",
			"One sentence, spoken aloud, in your own manner.",
		),
	)
}

// beside : What else the sender said, as a line of its own.
//
// Kept apart from the thing itself because it is a different kind of
// evidence: the thing is what was counted, and this is what the
// device happened to say alongside it. A remark about music that
// cannot name the track is not worth making, and the track is here.
func beside(c Candidate) []string {
	if c.Detail == "" {
		return nil
	}
	return []string{"- also reported: " + c.Detail}
}
