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
func Prompt(said []conversation.Message, rhythm, asking string) string {
	return prompt.Block(
		prompt.Text(
			"Below is one person talking to their assistant over the past week, in the order they said it,",
			"what their own devices reported them doing over the past four weeks, and what they had the assistant do over the same four weeks.",
			"The words are a week because what somebody is talking about is this week's business; the rest is four, because one Tuesday is an anecdote.",
			"Everything is theirs; none of it is the assistant's.",
			"Write a short description of them, for the assistant to read before it answers them.",
		),
		prompt.Text(
			"Cover four things.",
			"What they talk about, and which of it keeps coming back rather than having come up once.",
			"The people in their life, by name, and who those people are to them.",
			"How they behave: how they ask for things, what they want more of and less of, what they lose patience with.",
			"And their weeks: what they do most days, where they go and when, how long they stay, what tends to follow what, and what they keep asking the assistant for.",
		),
		prompt.Text(
			"Where the week shows something regular enough to expect again, say so as an expectation rather than a fact --",
			"\"usually\", \"tends to\", \"most weekday mornings\".",
			"That is what lets the assistant have the thing ready before it is asked for.",
			"Where it is not regular, say nothing: a routine invented from two coincidences is acted on exactly as a real one is.",
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
		prompt.Text(
			"Two things are left out however strongly they seem to follow.",
			"Where they live, where they are from, their nationality, their age: say none of it unless they said it themselves, and do not reason towards it from somebody else's address or from the languages and films that come up.",
			"Their health: leave out symptoms, conditions and medicines entirely unless they asked for something to be remembered, which is a different thing from having mentioned it in passing.",
		),
		prompt.Text(
			"The places below are different, and may be used.",
			"They are named by the person themselves, on their own devices, and a place they called home or the office is something they have told you rather than something you worked out.",
			"Write about where they go, how often and at what hour, by the names they gave those places.",
			"That is not the same as saying where they live: a name they chose for a geofence is not an address, a city or a country, and none of those follow from it.",
		),
		prompt.Text(
			"Hedging is not a way round either of those.",
			"'Almost certainly' and 'likely' are still claims, and a hedged guess in a description that is read before every answer is acted on exactly as a plain one is.",
		),
		"Here is what they said:",
		heard(said),
		rhythm,
		asking,
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
