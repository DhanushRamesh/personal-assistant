package profile

import (
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// Prompt : What the model is asked, to describe somebody from what they
// said.
//
// Exported so an eval builds the same thing the server does. That was a
// separate copy once elsewhere in this codebase, and the eval went on
// passing against wording the server had stopped using.
func Prompt(said []conversation.Message) string {
	return prompt.Block(
		prompt.Text(
			"Below is one person talking to their assistant over the past week, in the order they said it.",
			"Everything is theirs; none of it is the assistant's.",
			"Write a short description of them, for the assistant to read before it answers them.",
		),
		prompt.Text(
			"Cover three things.",
			"What they talk about, and which of it keeps coming back rather than having come up once.",
			"The people in their life, by name, and who those people are to them.",
			"How they behave: how they ask for things, what they want more of and less of, what they lose patience with.",
		),
		prompt.Text(
			"Write it as plain sentences in the third person, under two hundred words, with no headings and no lists.",
			"It is read aloud to nobody; it is read by a model, so write it to be used rather than admired.",
		),
		prompt.Text(
			"Only what is in front of you.",
			"Where you are guessing, say so in the sentence -- 'seems to', 'appears to' -- and where there is not enough to tell, leave it out entirely rather than filling the gap.",
			"Do not describe what they asked the assistant to do; describe them.",
			"Do not flatter them, and do not write anything you would not be able to point at a sentence for.",
		),
		"Here is what they said:",
		heard(said),
	)
}

// heard : What the person said, one line each.
//
// Timestamped by day rather than to the second. What matters is what
// recurs across days and what happened once, and a wall of clock times
// buries that in noise.
func heard(said []conversation.Message) string {
	var b strings.Builder
	day := ""
	for _, m := range said {
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if d := m.At.Format("Monday 2 January"); d != day {
			day = d
			b.WriteString("\n" + d + "\n")
		}
		b.WriteString("- " + collapse(text) + "\n")
	}
	return strings.TrimSpace(b.String())
}

// collapse : One line, and not an endless one.
//
// A pasted wall of text is one thing somebody said, and letting it run
// would have a single message crowd out a week of shorter ones.
func collapse(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	const most = 300
	if len(text) > most {
		return text[:most] + "…"
	}
	return text
}

// Since : The start of the window a rebuild reads, from a clock.
func Since(now time.Time) time.Time { return now.Add(-Window) }
