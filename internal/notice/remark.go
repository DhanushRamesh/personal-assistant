package notice

import (
	"fmt"
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

// Remark : What the model is asked, about one thing that might be
// worth mentioning.
//
// It is given the thing and what makes it unusual, and nothing else
// about what kind of thing it is -- because nothing here knows. The
// judgement asked for is the one a person in the room makes: is this
// worth saying out loud, and is it worth saying to somebody who was
// there when it happened.
func Remark(c Candidate, now time.Time) string {
	return prompt.Block(
		prompt.Text(
			"You are in the room with them and something has just happened that may be worth a word.",
			"Decide whether to say anything, and if so, say it in one sentence.",
		),
		prompt.Text(
			"Here it is, as their own device reported it, with what makes it stand out:",
		),
		prompt.Lines(append([]string{
			"- the thing: " + c.Label,
			"- " + because(c),
			"- when: " + c.At.Format("Monday 2 January at 3:04 pm"),
		}, beside(c)...)...),
		prompt.Text(
			"They were there when it happened, so telling them it happened is telling them nothing.",
			"Say the part they would not have noticed: that it is the third time, that it has not happened before, what it might mean.",
			"If there is no such part, there is nothing to say.",
		),
		prompt.Text(
			"Say nothing at all unless it is worth interrupting somebody for.",
			"Nothing is the ordinary answer and the right one most of the time -- most of what a device reports is not news to the person it is about.",
			"Do not fill the silence, do not ask them a question to make conversation, and do not offer to do anything.",
			"If you have nothing, answer with the single word "+Silence+" and no punctuation.",
		),
		prompt.Text(
			"Never guess what the thing is. If its name does not tell you what it was, that is not something to say a sentence about.",
			"One sentence, spoken aloud, in your own manner.",
		),
	)
}

// because : What made this one worth looking at, in words.
func because(c Candidate) string {
	switch c.Reason {
	case Often:
		if c.Before > 0 {
			return fmt.Sprintf("%d times in the last few minutes, against %d in the four weeks before that",
				c.Times, c.Before)
		}
		return fmt.Sprintf("%d times in the last few minutes, and never before that", c.Times)
	case Novel:
		return "nothing like it in the four weeks behind this: the first time it has been reported at all"
	default:
		return "unusual"
	}
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
