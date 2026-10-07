package profile

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// Part : One section of the description.
//
// Written one call at a time. Asked for all three at once the
// gateway gave up: forty six calls came back empty after between
// fifty four and sixty seven seconds each -- a minute of
// generation cut off rather than an answer. Our own timeout is
// two minutes and never came into it.
//
// Three shorter calls each finish well inside that, and the parts
// were already independent: the headings are fixed and the
// instructions for each never mentioned the others.
type Part string

const (
	// SoFar : What is so, with a count on every line.
	SoFar Part = "WHAT IS SO"
	// Day : The shape of an ordinary one.
	Day Part = "AN ORDINARY DAY"
	// Like : What the counts add up to, in prose.
	Like Part = "WHAT THEY ARE LIKE"
)

// Parts : The sections, in the order they are written and read.
//
// [Like] is last and not by accident: it is given what [SoFar]
// found, because its own instructions say it is what the first
// part adds up to.
var Parts = []Part{SoFar, Day, Like}

// Evidence : Everything the three calls are written from.
//
// A struct rather than the seven parameters this had grown to.
// The same evidence goes to every part: it is the writing that is
// split, not the reading.
type Evidence struct {
	// Said : What they said, folded.
	Said []conversation.Message
	// Rhythm : What their devices reported, counted.
	Rhythm string
	// Asking : What they had the assistant do.
	Asking string
	// Planned : What was in their diary.
	Planned string
	// Replies : How they answered what it said unasked.
	Replies []conversation.Reply
	// Was : The description written last time, as a draft.
	Was string
	// SoFar : What the first part said this time, for the last
	// part. Empty for the others.
	SoFar string
}

// Prompt : What the model is asked, for one part of a description.
//
// Exported so an eval builds the same thing the server does. That was a
// separate copy once elsewhere in this codebase, and the eval went on
// passing against wording the server had stopped using.
func Prompt(part Part, e Evidence) string {
	e = only(part, e)
	return prompt.Block(
		prompt.Text(
			"Below is one person talking to their assistant over the past week, in the order they said it,",
			"what their own devices reported them doing over the past four weeks, and what they had the assistant do over the same four weeks.",
			"The words are a week because what somebody is talking about is this week's business; the rest is four, because one Tuesday is an anecdote.",
			"Everything is theirs; none of it is the assistant's.",
		),
		prompt.Text(
			"Your job is to work out what is true of this person and keeps being true.",
			"Not a summary. Miss nothing that recurs, however ordinary it looks.",
			"The assistant reads this before it answers them, so a thing you leave out is a thing it will not know.",
		),
		prompt.Text(
			"Write one part of it and only that part:", string(part)+".",
			"Do not write the heading, and do not write any of the others.",
			"The rest are being written separately and the three are put together afterwards.",
		),
		section(part),
		adding(part, e.SoFar),
		prompt.Text(
			"How to read the evidence. These are the whole method and they do not need repeating for each sort of thing you find.",
			"Anything that repeats is a pattern: say when, how often, and how long it lasts.",
			"Two things that always appear together: one of them is explaining the other, and the pair means more than either.",
			"A gap that repeats is a duration they live by.",
			"The first and last thing on each day is the shape of that day.",
			"Something that used to repeat and has stopped is worth more than anything still happening.",
			"Something missing that you would expect to be there is also a fact.",
			"A long stretch where nothing is reported at all, falling at the same hour again and again, is the largest thing they do that day and nothing will ever report it directly.",
			"Read it from when it starts, how long it lasts, and what it is they were doing either side of it.",
			"Expect sorts of thing you have never seen before, and do not ask what they are: what an unfamiliar one means is what it coincides with, what it recurs beside, and when it happens.",
			"One you still cannot make sense of is reported as unexplained, not guessed at and not dropped.",
		),
		prompt.Text(
			"Counts are what make a conclusion safe. Two occurrences are a coincidence; the same pairing a dozen times across a dozen days is a fact about them.",
			"Where the count is small, say the conclusion is tentative or do not say it.",
			"Where something is regular enough to expect again, say so as an expectation -- \"usually\", \"tends to\", \"most weekday mornings\" -- because that is what lets the assistant have it ready before it is asked for.",
			"Where it is not regular, say nothing: a routine invented from two coincidences is acted on exactly as a real one is.",
		),
		prompt.Text(
			"Describe them, not their use of the assistant.",
			"What they keep asking about says what they care about, and that belongs here; how many times they asked, how they phrased it, and whether they repeated themselves do not.",
			"Those are facts about a tool being used and they are not facts about a person.",
		),
		prompt.Text(
			"Leave out anything that is looked up at the moment it matters.",
			"Anything dated that has not happened yet, and anything still outstanding, is read when it is needed and is right then.",
			"Written here it is a snapshot instead, and this is read before every answer -- so one that has since been called off is believed in place of going and looking.",
			"What belongs here is what is true of them over weeks, not what is on this Thursday.",
			"That somebody's birthday falls in March is a standing fact and stays. That they have something booked on the thirtieth is not, however firmly it is written down somewhere.",
		),
		prompt.Text(
			"Only what is in front of you.",
			"Where you are guessing, say so in the sentence -- 'seems to', 'appears to' -- and where there is not enough to tell, leave it out entirely rather than filling the gap.",
			"Do not flatter them, and do not write anything you would not be able to point at a line of evidence for.",
		),
		prompt.Text(
			"Two things are left out however strongly they seem to follow.",
			"Where they live, where they are from, their nationality, their age: say none of it unless they said it themselves, and do not reason towards it from somebody else's address or from the languages and films that come up.",
			"Their health: leave out symptoms, conditions and medicines entirely unless they asked for something to be remembered, which is a different thing from having mentioned it in passing.",
			"This is about being wrong, not about being discreet. Both were written confidently from evidence that did not support them, and a wrong line here is read before every answer and acted on.",
		),
		about(e.Rhythm,
			"The places below are different, and may be used.",
			"They are named by the person themselves, on their own devices, and a place they called home or the office is something they have told you rather than something you worked out.",
			"Write about where they go, how often and at what hour, by the names they gave those places.",
			"That is not the same as saying where they live: a name they gave a place is not an address, a city or a country, and none of those follow from it.",
		),
		about(e.Planned,
			"Their diary is below as well, and it is a different kind of thing from the rest.",
			"An entry says they meant to be somewhere. It is not evidence they were, and it never becomes one however ordinary it looks.",
			"What says whether they went is the readings, if anything does.",
			"Where a plan and the readings agree, that is worth saying and worth saying as agreement: they had a dentist at four and were somewhere else from ten until five.",
			"Where a plan has nothing beside it, say nothing about it at all rather than assuming it happened or assuming it did not.",
			"An entry marked invited is somebody else's plan that they were asked to, which is a weaker thing than one they made.",
			"What a diary is good for here is the pattern of it -- what recurs, what hour it tends to be, how much of it is theirs -- and that is a fact about them whether or not they turned up.",
		),
		prompt.Text(
			"Hedging is not a way round either of those.",
			"'Almost certainly' and 'likely' are still claims, and a hedged guess in a description that is read before every answer is acted on exactly as a plain one is.",
		),
		answering(e.Replies),
		previously(e.Was),
		"Here is what they said:",
		heard(e.Said),
		e.Rhythm,
		e.Planned,
		e.Asking,
		spoke(e.Replies),
	)
}

// only : The evidence one part is written from.
//
// All three were given everything, and splitting the writing into
// three calls then tripled the reading without making any one call
// smaller. It also had each part write the same findings again in
// its own register -- the commute appeared three times, counted,
// then as an hour of the day, then as a remark about his weeks --
// which is the repetition that is hardest to read and the easiest
// way for a description to end up contradicting itself.
//
// The parts are about different things, so they are not given the
// same things. An ordinary day is hours and places: a week of
// conversation cannot say what time somebody got up. What they are
// like is the opposite, written from their words, and the event
// counts reach it through what the first part made of them, which
// it is handed.
func only(part Part, e Evidence) Evidence {
	// Its own section of the description from last time, and not
	// the whole of it. A part is being asked to correct what it
	// wrote, and the other two sections are the other two calls'
	// business.
	was := Split(e.Was)[part]
	switch part {
	case Day:
		return Evidence{Rhythm: e.Rhythm, Planned: e.Planned, Was: was}
	case Like:
		return Evidence{Said: e.Said, Replies: e.Replies, SoFar: e.SoFar, Was: was}
	}
	e.Was = was
	return e
}

// about : Instructions for evidence, left out when there is none.
//
// A paragraph explaining how to read a diary, in front of a part
// that was given no diary, is an instruction to go and find one.
func about(evidence string, lines ...string) string {
	if strings.TrimSpace(evidence) == "" {
		return ""
	}
	return prompt.Text(lines...)
}

// section : What this part is, and how it is written.
func section(part Part) string {
	switch part {
	case SoFar:
		return prompt.Text(
			"WHAT IS SO",
			"One line per thing, as many lines as the evidence supports.",
			"Every line carries how many times and over what stretch of days, because that is what makes it arguable.",
			"A line you cannot put a count on does not belong in this part.",
			"Count what was recorded and say that is what you are counting. Recorded on four of seven days is what you have; at the office on four of seven days is a claim that the other three were days he did not go, and nothing in front of you says that -- a reading that was never taken looks exactly like a thing that never happened.",
			"Order them by how well established they are, not by how interesting.",
		)
	case Day:
		return prompt.Text(
			"AN ORDINARY DAY",
			"The shape of one, in the order it happens, from waking to sleeping. Ten lines at most.",
			"Each line is an hour or a stretch of hours and what is usually true of it -- when they are first about, when they leave if they leave, where they go, how long they are gone, when they are back, when they settle, when they stop.",
			"Hours as they would be said, and a range where it is a range: around nine, between seven and ten, not 09:00.",
			"Say which days it is true of. If weekdays and the weekend are different days, say so and give whichever you have the evidence for; if you cannot tell them apart, say that instead of averaging them into a day nobody has.",
			"Mark every line with how sure it is -- most days, usually, sometimes, once or twice -- and leave out anything you would have to invent to fill the gap. An hour nothing is known about is said to be unknown, which is worth more than a guess at it.",
			"How sure is not how many were recorded. Four days out of seven with a reading is not most days unless you know the other three had none to take, so where the count is of things recorded, say what was recorded and leave the frequency alone.",
			"The point of this part is to be the thing an unusual day is measured against, so a line that is nearly always true is worth more than an interesting one.",
		)
	case Like:
		return prompt.Text(
			"WHAT THEY ARE LIKE",
			"Plain sentences, third person, under a hundred and fifty words, no headings and no lists.",
			"What the first part adds up to.",
			"What they talk about, and which of it keeps coming back rather than having come up once.",
			"The people in their life, by name, and who those people are to them.",
			"How they behave: how they ask for things, what they want more of and less of, what they lose patience with.",
			"Their weeks rather than their days -- which days differ from the others and how -- since the day itself is the part above.",
			"This part may say things the counts only imply, and it is the only part that may.",
			"What it may not do is sum somebody up. Write what they do; do not write what kind of person that makes them.",
			"Spelling a name out letter by letter is worth having. Being organised is a verdict passed on somebody from a handful of things they did, and this is read before every answer as though it were a fact about them.",
			"A disposition drawn from two occasions is the easiest thing here to get wrong and the hardest to notice afterwards, because nothing in a later week will contradict it -- it will simply go on being read as true of them.",
		)
	}
	return ""
}

// adding : What the last part is told the first one found.
//
// Its instructions say it is what the first part adds up to, so
// it has to be given it. The other two are written from the
// evidence alone and are told nothing about each other.
func adding(part Part, sofar string) string {
	if part != Like || strings.TrimSpace(sofar) == "" {
		return ""
	}
	return prompt.Block(
		prompt.Text("This is what you wrote for the first part, a moment ago.",
			"It is what this part has to add up to, and you may not contradict it."),
		strings.TrimSpace(sofar),
	)
}

// Split : A description taken back apart into its sections.
//
// For a rebuild where one part failed: the others are new and
// that one keeps what it said last time, which is worth more than
// no description at all. The headings are fixed and written by
// this package, so this is reading back what it wrote.
func Split(description string) map[Part]string {
	out := map[Part]string{}
	at := make(map[Part]int, len(Parts))
	for _, part := range Parts {
		at[part] = strings.Index(description, string(part))
	}
	for _, part := range Parts {
		from := at[part]
		if from < 0 {
			continue
		}
		from += len(part)
		to := len(description)
		for _, other := range Parts {
			if next := at[other]; next > at[part] && next < to {
				to = next
			}
		}
		if body := strings.TrimSpace(description[from:to]); body != "" {
			out[part] = body
		}
	}
	return out
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

// Folded : The same thing said over and over, counted once.
//
// The description is read before every answer, so anything in it is
// acted on. What went in on 4 October 2026 was "repeats himself
// heavily, sometimes cycling through the same request a dozen times
// in a row", "loses patience quickly", "checks both repeatedly within
// a single session without anything having changed". None of it was
// about the person. It was an afternoon of saying the same five
// questions into the microphone to measure how long they took.
//
// The evidence cannot tell that apart from insistence, and neither
// can a rule in the prompt -- there is one already, and the
// description broke it. So the repetition is removed before the model
// sees it: a run of the same thing becomes one thing.
//
// Removed rather than counted. A count is still evidence, and the
// model drew a character from it; writing "said eleven times" invites
// exactly the sentence this exists to prevent. Insistence that is real
// survives anyway, because a question asked again tomorrow is outside
// Again and is kept.
//
// Matched on the words with their spacing, case and punctuation taken
// off, which is the same test in any language. Nothing is matched on
// what the words mean.
func Folded(said []conversation.Message) []conversation.Message {
	out := make([]conversation.Message, 0, len(said))
	last := make(map[string]time.Time, len(said))
	for _, m := range said {
		key := bare(m.Content)
		if key == "" {
			out = append(out, m)
			continue
		}
		if when, seen := last[key]; seen && m.At.Sub(when) < Again {
			last[key] = m.At
			continue
		}
		last[key] = m.At
		out = append(out, m)
	}
	return out
}

// bare : The words alone, for deciding whether two messages are the
// same one said twice.
func bare(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// answering : What to make of a reply to something nobody asked for.
//
// Owner: *"whenever it announces or greets, the acknowledgement or
// the respective response, if relevant, has to be taken more weight
// in profiling, greeting and when talking to me."*
//
// The reason it is worth more is not that it is recent or rare. It
// is that nobody asked for the sentence it answers. Everything else
// here is the person starting something; this is the person judging
// something the assistant chose to do, and there is no other
// evidence of that at all.
func answering(replies []conversation.Reply) string {
	if len(replies) == 0 {
		return ""
	}
	return prompt.Text(
		"One section below is different from the rest and is worth more than its size suggests.",
		"It is the times the assistant spoke first -- a greeting, a reminder, a warning, something it noticed, something coming up -- and what they said back within a few minutes.",
		"Nobody asked for any of those sentences. So what they said back is the only evidence there is about whether being spoken to unprompted is wanted, and about which sorts of it are.",
		"Read it for that and say what you find: what they take an interest in, what they answer flatly, what they ask you to stop, what they thank you for.",
		"A single clear one of those is worth a dozen ordinary messages, because there is nothing else that tells you.",
		"How quickly they answered is part of it: seconds is a reaction and minutes is a decision.",
		"And it is still evidence about them rather than about the assistant: write what it says about the person, not how well the assistant is doing.",
	)
}

// spoke : The pairs themselves.
func spoke(replies []conversation.Reply) string {
	if len(replies) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Here is what they said back when the assistant spoke first:\n")
	for _, r := range replies {
		fmt.Fprintf(&b, "\n%s -- the assistant said, unasked (%s): %s\n  they answered %s later: %s\n",
			r.At.Format("Mon 2 Jan 15:04"), r.Kind, strings.TrimSpace(r.Spoke),
			after(r.After), strings.TrimSpace(r.Said))
	}
	return strings.TrimRight(b.String(), "\n")
}

// after : How long they took, as somebody would say it.
func after(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	return fmt.Sprintf("%d minutes", int(d.Round(time.Minute).Minutes()))
}
