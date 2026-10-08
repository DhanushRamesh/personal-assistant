package vocabulary

import (
	"strings"

	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/prompt"
)

// Prompt : What the model is asked, to find the names in what
// somebody said.
//
// Exported so an eval builds the same thing the server does.
func Prompt(said []conversation.Message, reported []string) string {
	return prompt.Block(
		prompt.Text(
			"Below is one person talking to their assistant over the past week.",
			"It was written down by speech recognition, so some of it is already wrong.",
			"List the proper nouns in it: the names of people, pets, places, streets, brands, products, films, songs and anything else that is a name rather than a word.",
		),
		prompt.Text(
			"This list is given to the speech recognition itself, to tell it what to expect next time.",
			"So the point is names it would otherwise get wrong.",
			"Include a name even where it has clearly been misheard, and write it as you believe it is spelt rather than as it was transcribed.",
		),
		prompt.Text(
			"Leave out ordinary words, however often they come up: the engine already knows them, and every one included takes the place of a name.",
			"Leave out a name that is also an everyday word -- a film called Cars, a band called Blur, a place called Reading.",
			"Priming one of those makes the everyday word worse, which is the opposite of the point: telling the engine to expect \"timer\" and not \"time\" is how \"what is the time\" came back as \"what is the thing\".",
			"Leave out the assistant's own name and the names of its own abilities.",
			"Leave out anything you are inventing to fill the list -- a short list is correct when there were few names.",
		),
		prompt.Text(
			"Give a person once, in the form they are usually called.",
			"Not the first name, the surname and both together as three entries: that is one person taking three of the hundred places there are.",
			"The same for anything else with a long and a short form -- choose the one that is actually said aloud.",
		),
		prompt.Text(
			"Answer with one name per line and nothing else.",
			"No numbering, no bullets, no heading, no explanation, and no brackets saying who somebody is.",
			"Write each name once, in its ordinary spelling, with no surrounding quotes.",
		),
		"Here is what they said:",
		heard(said),
		reportedBy(reported),
	)
}

// reportedBy : The names their own devices gave.
//
// Given apart from the transcript and said to be spelt correctly,
// because that is the whole of what they are for. Everything above
// has been through speech recognition and may already be wrong; a
// contact name, a place they named themselves, a track a player
// reported are the spellings as they actually are. They are exactly
// the words that keep being misheard, which is exactly what this
// list is for.
//
// Still offered rather than forced. The rules above about everyday
// words hold here too -- a network called Home and a song called
// Cars are as bad primed from a device as from a transcript.
func reportedBy(reported []string) string {
	if len(reported) == 0 {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"Their own devices also reported these, most-reported first.",
			"These have not been through speech recognition: they are contact names, names they gave places themselves, and what their players and devices call themselves, spelt as they really are.",
			"So where one of these is also in what they said above but spelt differently, this is the spelling and that is the mishearing.",
			"They are candidates on the same terms as the rest: include the ones that are names worth expecting, and leave out the ones that are everyday words.",
		),
		prompt.Lines(reported...),
	)
}

// heard : What the person said, one line each.
//
// Undated, unlike the description's version. What matters here is
// only which names occur, and the days would be noise the model has
// to read past.
func heard(said []conversation.Message) string {
	var b strings.Builder
	for _, m := range said {
		if text := strings.TrimSpace(m.Content); text != "" {
			b.WriteString("- " + collapse(text) + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// collapse : One line, and not an endless one.
func collapse(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	const most = 300
	if len(text) > most {
		return text[:most] + "…"
	}
	return text
}
