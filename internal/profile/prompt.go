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
func Prompt(said []conversation.Message, rhythm, asking, was string) string {
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
			"The counted part is evidence, not a list to repeat back. Work things out from it.",
			"Two kinds of event that keep happening together are telling you what one of them is:",
			"a thing that only ever appears alongside another has its meaning given to it by that other,",
			"and a thing that reliably comes a certain time after another is a duration they live by.",
			"A gap that repeats is worth saying as a length. A pairing that repeats is worth saying as what it means, not as two event names.",
			"Say the conclusion in their terms -- what the thing is, what it is for, what it says about their day -- and never in the vocabulary of the events themselves.",
		),
		prompt.Text(
			"Do this for kinds of event you have never seen before, and expect new ones: their devices change and nobody will explain them to you.",
			"What an unfamiliar kind means is what it coincides with, what it recurs beside, and when it happens.",
			"That is the whole method, and it does not need to be told again for each new kind.",
			"An event you cannot make sense of is left out rather than guessed at.",
		),
		prompt.Text(
			"Counts are what make a conclusion safe to draw. Two occurrences are a coincidence,",
			"and the same pairing a dozen times across a dozen days is a fact about them.",
			"Where the count is small, say the conclusion is tentative or do not say it.",
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
		previously(was),
		"Here is what they said:",
		heard(said),
		rhythm,
		asking,
	)
}

// previously : The description written last time, to be corrected.
//
// Given as a draft and not as a source, and the difference is the whole
// of it. This was deliberately left out until 2 October 2026 so that a
// rebuild could not inherit anything it had invented: a description
// written from the last description drifts, and after enough rounds it
// is about somebody made up on a Tuesday. The owner's reason for
// putting it back is the opposite failure -- a profile rebuilt from
// scratch every time cannot notice that something in it has stopped
// being true, because it never sees the claim to drop it.
//
// So the wording has to do the work the safeguard used to: everything
// in it is up for deletion, and nothing in it is evidence for itself.
func previously(was string) string {
	if strings.TrimSpace(was) == "" {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"Here is the description from last time. It is a draft to correct, not something you were told.",
			"Keep what the evidence below still supports. Change what it no longer supports.",
			"Delete anything nothing in front of you supports at all, including things that were true when they were written.",
			"A claim being already written is not a reason to keep it, and it is not evidence for itself.",
			"Something that was a routine and has stopped happening is the most useful thing you can remove.",
		),
		was,
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
