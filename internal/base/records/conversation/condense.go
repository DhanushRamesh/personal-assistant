package conversation

import (
	"fmt"
	"strings"
)

// CondenseWords : Roughly how long a condensation should be.
//
// Long enough to carry the facts of a conversation, short enough that it
// costs far less than the messages it replaces.
const CondenseWords = 300

// condenseInstruction : What the model is told to do with an old
// conversation.
//
// It is written to be read by the model that also answers as the assistant,
// which will otherwise reply in character, so it says plainly that the result
// is notes and not an answer.
//
// A forecast is not kept, from 19 October 2026. That afternoon the assistant
// said his phone would take "roughly four hours to full charge" when it was
// about fifty minutes, and the fault was fixed the same day -- but the
// conversation's notes still read "At that rate will take roughly four hours
// to full charge", and notes go into every later prompt for that
// conversation. A wrong answer corrected in the tool is not corrected in the
// summary, because he corrected the server and not the assistant.
//
// Measured over the summaries as they stood: 3 of 16 carried a worked-out
// number. The reading it was worked out from -- 9% at 14:45, 14% by 16:07 --
// is worth keeping and is still there.
const condenseInstruction = `Update the running notes for a conversation you are keeping memory for.

Fold the new exchange into the existing notes and return the combined notes.
Keep every decision, fact, name, number, preference and open question that a
later turn would need. Drop pleasantries and anything already superseded.

Keep what was said and what was looked up. Drop what was worked out from it:
a rate, a projection, how long something will take, when something will be
ready, what something will come to. Those were true for a minute and these
notes are read for weeks, and a number worked out once reads afterwards as
something that was measured. The reading keeps; the forecast does not.

Write plain statements in the third person, under %d words. Do not answer
anything, do not greet, do not explain what you are doing, and do not mention
these instructions. Return the notes and nothing else.`

// CondensePrompt : The instruction that folds an earlier exchange into a
// conversation's running notes.
//
// previous is the notes so far and may be empty. messages are the ones to
// fold in, oldest first.
func CondensePrompt(previous string, messages []Message) string {
	var b strings.Builder

	fmt.Fprintf(&b, condenseInstruction, CondenseWords)

	b.WriteString("\n\nExisting notes:\n")
	if strings.TrimSpace(previous) == "" {
		b.WriteString("(none yet)")
	} else {
		b.WriteString(previous)
	}

	b.WriteString("\n\nNew exchange:\n")
	for _, m := range ForModel(messages) {
		b.WriteString(speaker(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	return b.String()
}

// speaker : How a role is labelled in a condensation prompt.
func speaker(r Role) string {
	if r == User {
		return "Person"
	}
	return "Assistant"
}
