package elsewhere

import (
	"fmt"
	"strings"

	"personal-assistant/internal/base/thinking/prompt"
)

// Says : What the model is told about what was found somewhere else.
//
// Appended to a tool result that has already said it found nothing, so
// this never contradicts it: the reminder tool is right that there is
// no such reminder, and this is the rest of the sentence.
//
// Written as an instruction and not as data, for the same reason
// [tool.WhichOne] is. A model handed a list decides what to do with it,
// and what to do differs by how many there are.
func Says(named string, found []Thing) string {
	switch len(found) {
	case 0:
		// Deliberately not a sentence saying nothing was found. The
		// tool that ran has already said that, in its own words and
		// about its own domain, and it usually said it while handing
		// over the list and asking which was meant. Repeating it here
		// would argue with a better-informed sentence.
		//
		// What is added is the one thing nothing in the server knows:
		// an alarm set on their phone cannot be read back, so it is
		// the one place a thing can be and not be found. And that the
		// web is not the next place to look for something of theirs.
		return prompt.Text(
			fmt.Sprintf("Nothing kept anywhere else is called %q either: not a rule, a task, a "+
				"diary entry, or anything they have had you remember.", named),
			"An alarm set on their own phone is the one thing you cannot see. If it could be one "+
				"of those, say so plainly rather than telling them nothing of the sort exists.",
			"Do not search the web for it. The web does not hold their reminders, their diary or "+
				"their alarms; it is for the world and never for their own life.",
		)

	case 1:
		one := found[0]
		return prompt.Text(
			fmt.Sprintf("There is, though, a %s called %q%s.", one.Kind, one.Named, at(one)),
			fmt.Sprintf("That is almost certainly what they meant. Put it to them as a question "+
				"they can answer yes or no -- name it and what it is -- and act on their answer "+
				"with %s.", one.Tool),
			"Do not tell them the thing does not exist. It does; it is kept somewhere else.",
		)

	default:
		// Not a list read out. Owner, about this exact case: "it
		// should figure out and should come to a conclusion that it
		// is an alarm kept to drink water at 11:45." Reading three
		// names back is the opposite of coming to a conclusion.
		//
		// So the first is proposed and the rest are held in reserve.
		// They are sorted with the likeliest first, and where
		// nothing separates them by name the live one leads, which
		// is almost always the one somebody means.
		first, rest := found[0], found[1:]
		return prompt.Text(
			fmt.Sprintf("The likeliest thing they meant is the %s called %q%s.",
				first.Kind, first.Named, at(first)),
			fmt.Sprintf("Put that to them as one short question they can answer yes or no, and "+
				"act on yes with %s. Do not read the rest of this out unless they say no.",
				first.Tool),
			fmt.Sprintf("If they say no, these %d are the others called something like %q:",
				len(rest), named),
			prompt.Lines(lines(rest)...),
			"Do not tell them there is no such thing. One of these is almost certainly it.",
		)
	}
}

// at : When a thing is for, as a clause, or nothing.
func at(t Thing) string {
	if strings.TrimSpace(t.When) == "" {
		return ""
	}
	return ", " + t.When
}

// lines : One line per candidate, in the order they were ranked.
func lines(found []Thing) []string {
	out := make([]string, 0, len(found))
	for _, t := range found {
		out = append(out, fmt.Sprintf("%s -- a %s%s -- acted on with %s",
			t.Named, t.Kind, at(t), t.Tool))
	}
	return out
}
