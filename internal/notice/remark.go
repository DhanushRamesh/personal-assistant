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
const Silence = prompt.Silence

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

// Tries : How many times a thing that must be said is put to the
// model before it is said plainly instead.
//
// Three, which at a look every thirty seconds is a minute and a
// half. Long enough that a provider busy for a moment still gets to
// write the sentence properly, short enough that a warning is out
// while it is still worth having.
const Tries = 3

// Plain : What is said when the model cannot be reached at all.
//
// A warning that cannot be worded is still a warning, and silence
// is the one wrong answer here. At five past midnight on 6 October
// a battery at twenty per cent was found, allowed, and lost to a
// provider busy for a few seconds; retrying covers that, and this
// covers the provider being down for the evening.
//
// Built from the label, which is the sender's own words for the
// thing and the only description that exists. Nothing here knows
// what it means -- the dot becomes a space and that is the whole of
// the rendering -- so a kind nobody has thought of yet comes out
// just as well as a battery does.
//
// It says why it is terse. Somebody who hears "battery level 20"
// with no sentence around it would reasonably wonder what is wrong,
// and the answer is that something is.
func Plain(c Candidate) string {
	said := strings.TrimSpace(strings.ReplaceAll(c.Label, ".", " "))
	if said == "" {
		return ""
	}
	return "This was marked to be said at once, and I cannot word it just now: " + said + "."
}
