// Package greet writes what the assistant says when somebody comes in.
//
// It used to be one of a handful of fixed sentences chosen by the hour.
// That is a doorbell: it knows somebody arrived and nothing else, and it
// says the same thing whether they have been gone five minutes or since
// Tuesday.
//
// The owner's description of what it should be instead: "treat Jarvis
// like a person -- I met him two hours back, he greeted me, after that
// things happened, Jarvis came to know them, and after two hours I see
// him, he greets again and asks me things that he knew after that, like
// how a human would ask."
//
// So the greeting is written each time, from what has happened since the
// two of them last had anything to do with each other. Which is not how
// long they were out, and not when they were last greeted: a
// conversation at the desk at four o'clock leaves nothing to catch up on
// at five.
package greet

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
	"github.com/DhanushRamesh/personal-assistant/internal/base/prompt"
)

// Within : How long the greeting may take before the fixed one is used
// instead.
//
// Measured on this gateway, 2 October 2026, with a real greeting prompt
// of about 2600 characters: sonnet 2.0, 2.1, 2.4 seconds, and haiku
// 1.2, 1.8, 4.0. Two and a half seconds was the first guess and it cut
// off most attempts, which is how this number came to be measured
// rather than chosen.
//
// Ten, which the owner set: *"no max 10 secs."*
//
// The only ceiling is somebody standing in a doorway waiting to be
// spoken to, and the reminders still have to be read out after
// this. Home Assistant is not one: an older comment here said it
// gave up at ten seconds, and the rest_command has said 180 for
// some time.
//
// Measured over the last six real greetings the model took 1.7 to
// 4.4 seconds, so five left almost nothing in hand at the slow end.
// None of them overran; the four failures in the log say "the
// provider said nothing", which is a different fault and is fixed.
//
// **It buys headroom and not reasoning.** Low effort was measured
// at 12.1, 16.7 and 2.1 seconds on the same three evenings, so even
// ten reaches it one time in three; high costs 29 to 46. Thinking
// cannot be paid for out of a doorway, and what it needs is to
// start before they are in it -- which is a different problem, and
// the measurement that it has no easy answer is in DEVELOPMENT.md.
const Within = 10 * time.Second

// Recent : How far back events are read when the assistant has never
// spoken to them before.
//
// A first greeting has no last-spoken-to time to work from, and reading
// everything ever reported would have it asking about a Tuesday three
// weeks ago as though it had just happened.
const Recent = 12 * time.Hour

// Most : The most events handed over for one greeting.
//
// Newest kept, because a day of a phone reporting is mostly the end of
// the day. A bound rather than a guess at the right number: this runs
// at the door and the prompt cannot be allowed to grow with however
// many kinds of event the phone learns to send.
const Most = 120

// Asked : How the model is asked. Returns what it answered.
type Asked func(ctx context.Context, prompt string) (string, error)

// Told : What the assistant knows at the moment somebody walks in.
type Told struct {
	// Now : When they came in, in their own zone.
	Now time.Time
	// Since : When the assistant last said anything to them, zero if
	// never.
	Since time.Time
	// Events : What their devices reported in between, oldest first.
	Events []event.Event
	// Profile : The standing description of them. May be empty.
	Profile string
	// Opening : The first words, written by the server rather than
	// by the model.
	//
	// A salutation for the hour and a welcome back. Said whatever
	// else happens: on its own when the model has nothing or cannot
	// be reached in time, and in front of whatever it wrote when it
	// can. The model is told it has already been said.
	//
	// Owner, 5 October 2026, after a greeting that opened "Kliffy
	// rang while you were out, sir": *"it should say the good
	// morning / afternoon / evening and welcome backs at the
	// beginning."* Asking the model for it would have worked most
	// of the time, which is the trouble: the failure is silent and
	// nobody is reading the greeting afterwards to catch it.
	Opening string
	// Reminders : Anything held back while they were out, and anything
	// that was never said at all, in the words they would be said in.
	// The model is told to say these and not to summarise them away.
	Reminders []string
	// Diary : What is in the diary between now and the end of
	// tomorrow, soonest first. Empty when there is nothing, or when
	// it could not be read in time.
	Diary []Appointment
	// Were : What was in the diary while they were away.
	//
	// The half this never had. Owner: *"it knows I go to a movie in
	// the calendar, then it knows my location. When I come back it
	// should cross-check whether I went there, and ask me how was
	// the movie."*
	//
	// It could not, and not for want of thinking: it was shown
	// where they had been and never what they had planned, so there
	// was nothing to check against. Nothing here does the checking
	// -- the stays carry their addresses now, the appointments
	// carry their places, and whether the two are the same thing is
	// a judgement rather than a rule.
	Were []Appointment
	// Chores : What is still to be done and is due by the end of
	// tomorrow or was due already, and how many others are open.
	Chores Chores

	// Usual : How many times each thing in Events has happened over
	// the past few weeks, keyed the same way the lines are labelled.
	//
	// Without it the window is read as though everything in it were
	// news. Measured: a phone moving between a router and its extender
	// inside one house produced "you have been moving about quite a
	// bit" to somebody who had not left the building, in a window that
	// also said they had been at home all day. The events were real
	// and the reading was wrong, and nothing in the window could have
	// told it otherwise -- what was missing was that this happens
	// every day.
	Usual map[string]int
}

// Appointment : One thing in the diary, as much of it as is worth
// saying out loud.
type Appointment struct {
	// What : Its title.
	What string
	// When : When it starts. Midnight for one that takes the day.
	When time.Time
	// AllDay : Whether it takes the whole day rather than a slot.
	AllDay bool
	// Where : The place, if one was given.
	Where string
}

// Chores : What is still to be done.
//
// Split into the ones that are due and a count of the rest, because
// the rest are the ones that must not be read out. Sixteen open tasks
// is a normal number to have and a terrible thing to be told at a
// door.
type Chores struct {
	// Due : Open tasks due by the end of tomorrow, or overdue,
	// soonest first.
	Due []Chore
	// Others : How many more are open and not yet due.
	Others int
}

// Chore : One task.
type Chore struct {
	// What : Its title.
	What string
	// By : The day it is due. Zero when none was set.
	By time.Time
	// Overdue : Whether that day has passed.
	Overdue bool
}

// Silence : What the model answers when there is nothing to add.
//
// A word rather than an empty reply, and the reason is measured. Of
// twelve answers to six windows the owner had ruled should produce
// nothing, not one came back empty -- and one of them came back as
// "nothing here is outstanding enough to raise ... *(Silence after
// the greeting.)*", which is a model that has made the right
// judgement and cannot express it. Asked for a sentence it writes a
// sentence; given a word that means no sentence, it has somewhere
// to put the judgement.
//
// The same device internal/notice uses, and it was in this package
// until the prompt was rewritten on 5 October and the refusal
// wording went with it.
const Silence = "NOTHING"

// Quiet : Whether the model declined.
//
// Generous about how it is written, because the cost of reading a
// refusal as a greeting is that the refusal is read out loud.
func Quiet(said string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(said))
	trimmed = strings.Trim(trimmed, ".!\"' ")
	return trimmed == Silence
}

// Anything : Whether there is anything at all to write a greeting
// from.
//
// Asked before the model is, and the model is not asked when the
// answer is no. A prompt with nothing under it is a page of
// instructions about finding something worth saying, handed to
// something obliging, and over twelve sampled arrivals the one with
// an empty window answered with an invented missed call.
//
// There is no judgement in it. Where every source is empty the
// greeting is the opening, which is what it would have been anyway,
// and nothing is spent finding that out.
func (t Told) Anything() bool {
	return len(t.Events) > 0 || len(t.Were) > 0 || len(t.Diary) > 0 ||
		len(t.Reminders) > 0 || len(t.Chores.Due) > 0
}

// Prompt : What the model is asked.
//
// Exported so an eval builds the same thing the server does.
func Prompt(t Told) string {
	return prompt.Block(
		prompt.Text(
			"Somebody has just walked in to the room their assistant is in.",
			"You are that assistant, speaking aloud to them, and you have known them for a while.",
			"Write what you say after the greeting. One or two sentences at most, out loud, the way a person speaks to somebody they know.",
		),
		opening(t),
		when(t),
		prompt.Text(
			"Below is everything their own devices reported while the two of you were not talking.",
			"Read it the way somebody who knows them would, and work out whether anything in it is worth mentioning.",
			"Something that happened far more often than usual, or lasted far longer than usual,",
			"or involved the same person again and again, or is simply out of keeping with their ordinary day, is worth remarking on.",
			"Most of what is below is not a thing to talk about at all; it is what lets you understand the one thing that is.",
			"The test is whether somebody would ever raise it with a person who had just walked in. Where they would not, it is evidence rather than a subject: use it to reach something worth saying, and leave it out of the saying.",
			"Where something is worth it, say so and ask about it, the way somebody would: a short question, not a report.",
			"Where nothing stands out -- and most of the time nothing will -- answer "+Silence+". The greeting has been said; silence after it is a greeting.",
		),
		nothing(),
		prompt.Text(
			"Never list what the devices reported, never read out a count, and never use the names the events are written under.",
			"You noticed something; you did not read a log.",
			"One thing at most. Somebody who comes in and is asked three questions has been interrogated, not greeted.",
		),
		sure(),
		settled(t),
		shown4(),
		reminders(t),
		reached(t),
		prompt.Text(
			"It is said out loud in a room that may have other people in it.",
			"Where mentioning something would mean saying aloud who called them or what they were doing, keep it to what they would not mind overheard,",
			"and let them be the one to say the rest.",
		),
		prompt.Text(
			"Only what is in front of you. Do not invent an event, a person or a reason,",
			"and do not guess at what something meant -- ask them instead, which is the point of asking.",
			"A name is worth no more than whatever handed it to you. One that came off a device may be a nickname, a mishearing or the wrong person altogether, and where it says it does not know, it does not know.",
			"Say somebody rather than a name you cannot vouch for. They will tell you who it was, and a wrong name is not a hedge -- it is a different person.",
			"If there is nothing here at all, answer "+Silence+": they have already been greeted.",
		),
		prompt.Text(
			"Plain spoken sentences. No lists, no headings, no stage directions, and nothing they would have to read rather than hear.",
			"Do not say you are an assistant and do not offer to help: they know, and they will ask.",
		),
		prompt.Text(
			"Say it the way it would be said, not the way it would be written.",
			"There is a shape that creeps in and it has to be resisted: a statement, a dash, and a question tacked on the end -- \"you were out a good while, sir -- was it a good dinner?\"",
			"Written down that reads well. Spoken, every greeting sounds the same, and somebody hearing it twice a day notices within a week.",
			"Vary it. Sometimes the question alone is the whole greeting and the fact behind it goes unsaid, because they already know where they were.",
			"Sometimes there is nothing to ask and a sentence is simply said to them.",
			"Where you have a name for the thing, use it rather than describing it: \"how was the film\" is worse than its title, and \"you were out a good while\" is worse than either.",
			"Short. Somebody at a door has their hands full.",
			"Every sentence must be one. \"There was a call came in\" is not, and neither is a clause hung on the end that cannot stand by itself.",
			"Whatever you write is spoken exactly as it stands. There is no second go they do not hear, so never work anything out in it and never correct yourself inside it: decide first, then write the one sentence.",
			"A length of time is a count like any other, and is no more said than a count is. A long call, or one that went on a while; never an hour and a half.",
		),
		planned(t),
		silent(t),
		profile(t),
		coming(t),
		happened(t),
	)
}

// opening : What has already been said, so the model does not say it
// again.
//
// The salutation and the welcome are the server's, and they are
// spoken in front of whatever comes back from here. A model that
// writes its own greets the person twice.
func opening(t Told) string {
	said := strings.TrimSpace(t.Opening)
	if said == "" {
		return ""
	}
	return prompt.Text(
		"You have already said this much, out loud, and they have heard it: \""+said+"\"",
		"Write only what comes after it. Do not greet them again, do not say good morning or good afternoon or good evening,",
		"and do not welcome them back: it has been done.",
		"What you write follows straight on, so it begins mid-greeting rather than at the start of one.",
	)
}

// Outstanding : Whoever tried to reach them and did not, with
// nothing since to settle it.
//
// Worked out here rather than asked for. Deciding whether a thing
// was left hanging is not a judgement about language, it is two
// comparisons, and a model asked to make it got it wrong on four
// arrivals out of four -- raising a call somebody returned eight
// minutes later, and raising calls that were answered when they
// came.
//
// Two payload conventions do the whole of it, and neither is a
// kind:
//
//   - number. A sender writing one has said this was somebody being
//     reached; it means the same whoever sends it, and no kind has
//     to be named for it to be read.
//   - seconds. A sender writing it has said how long something ran.
//     Some of it means the reaching worked. None of it, or nought
//     of it, means it did not.
//
// So: an attempt is an event carrying a number and no time. It is
// settled by anything later that carries a number, names the same
// person, and did take some time.
//
// Inferring the family from the window was tried first -- a member
// with no seconds, in a family where another member had some -- and
// it fails on the commonest case of all: a window holding one
// missed call and nothing else has nothing to compare it against.
//
// Measured against the owner's own rulings on twenty arrivals: it
// agrees with every one of them. A call returned eight minutes
// later is settled; two from a number with no name and nothing
// after are not; an evening of answered calls holds nothing
// outstanding at all.
func Outstanding(t Told) []string {
	open, _ := event.Reaching(t.Events)
	out := make([]string, 0, len(open))
	for _, r := range open {
		said := r.Named() + ", at " + r.When.In(t.Now.Location()).Format("3:04 pm")
		if r.Tries > 1 {
			said += ", and again after that"
		}
		out = append(out, said)
	}
	return out
}

// fields : A payload as a map, or an empty one.
func fields(e event.Event) map[string]any {
	var into map[string]any
	if json.Unmarshal(e.Payload, &into) != nil {
		return nil
	}
	return into
}

// reached : What was left hanging, and that it outranks the rest.
//
// Only when something was. The block used to be in every prompt,
// which made it a standing suggestion of the thing to look for, and
// a model with nothing else to say filled the slot with the highest
// value in it -- inventing a caller three times in twelve, once over
// a window holding nothing at all.
//
// Then it was conditional on the window mentioning that sort of
// thing, which stopped the inventions and not the rest: an answered
// call is still a call, and it went on raising those. Now the
// condition is the fact itself, worked out above, and the block
// says which one rather than asking it to find one.
func reached(t Told) string {
	open := Outstanding(t)
	if len(open) == 0 {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"Somebody tried to reach them while they were out and did not get through, and nothing since shows it was settled.",
			"That is the thing to say. It is told rather than asked about, it wins over anything else here, and it is a few words: no number, no count, and nothing about what they wanted.",
			"Say only that it happened and who, where there is a who. A message nobody left is the worst thing this could do: they will ring somebody back who never called.",
		),
		prompt.Lines(open...),
	)
}

// Settled : Everybody who was reached, and when.
//
// The other half of Outstanding and the half that was missing. Said
// one by one rather than as a sentence about the window, because a
// sentence about the window was not enough: told that nothing was
// owed, the model stopped asking whether somebody had got through
// and started asking how the call went instead. It had given up
// treating them as owed and not as news.
//
// They were there for every one of these. That is the whole
// argument, and it is easier to make about a named call at a named
// hour than about a window.
func Settled(t Told) []string {
	_, got := event.Reaching(t.Events)
	out := make([]string, 0, len(got))
	for _, r := range got {
		out = append(out, r.Named()+", last at "+r.When.In(t.Now.Location()).Format("3:04 pm"))
	}
	return out
}

// settled : What got through, and that none of it is a subject.
func settled(t Told) string {
	done := Settled(t)
	if len(done) == 0 {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"These got through. They spoke, and they were there for every one of them.",
			"So none of these is owed and none of them is news. Do not raise one, do not ask whether somebody reached them, and do not ask how it went: they know how it went.",
		),
		prompt.Lines(done...),
	)
}

// nothing : That saying nothing is the ordinary answer and not a
// failure to find one.
//
// The instruction was already there and was already ignored. Over
// twenty-two real arrivals sampled out of four weeks of a phone's
// reporting, the model spoke twenty-two times and returned nothing
// not once -- with "most of the time nothing will" sitting in the
// middle of a paragraph about finding something, which is not where
// a model looks for permission to stop.
//
// Having found nothing worth saying it reached for whatever was
// left: the shape of the day, the hour they came in, the phone
// itself. So those are named as what having nothing looks like,
// rather than left for it to discover that they do not count.
func nothing() string {
	return prompt.Text(
		"Most arrivals have nothing in them worth a word. Returning nothing is the commonest right answer here, and it is not a failure to have found one.",
		"You are not being asked to produce a sentence. You are being asked whether there is one worth saying, and usually there is not.",
		"If what you are left with is the shape of their day, the hour they came in, how long they were gone, or how their phone behaved, then you have nothing: they were there for all of it and none of it needs telling.",
		"Where that is what you have, answer with the single word "+Silence+" and nothing else -- no full stop, no explanation, and nothing about having nothing to say. It is never spoken; it is how you say there is nothing.",
	)
}

// shown4 : Four arrivals already judged, two of them silences.
//
// Telling it that saying nothing is the ordinary answer moved
// nothing: nought silences in twelve across six windows the owner
// had ruled should produce one. Twice. Showing is the thing left to
// try, and there was nothing to show until he ruled on ten real
// windows and six came back silent.
//
// Four of those ten, chosen for the two things they settle: that an
// answered call is not news, and that a thing which merely happened
// loses to a thing still outstanding. The other six are held back
// to measure against, because a prompt carrying the cases it is
// scored on is scored on nothing.
//
// Compressed on purpose. They are at a door inside ten seconds, and
// the point of each is its verdict rather than its detail.
func shown4() string {
	return prompt.Text(
		"Four arrivals somebody has already ruled on, with what was said and why. They are the measure; match them.",
		"-- Home all morning. Two short calls, one from his mother and one from a friend, both answered. Answered "+Silence+": a call that connected is not news to the person who took it.",
		"-- An afternoon at the office, with nothing in the window but the phone reporting on itself. Answered "+Silence+": a working afternoon, and a device's own housekeeping, are the mechanics of a day and he was there for both.",
		"-- Nothing reported at all, and the dentist and a haircut at eleven in the morning. Said: \"You have the dentist and the haircut at eleven, sir.\" Near enough that hearing it now beats finding it out.",
		"-- Out all evening, a meeting in the diary he plainly went to, and two calls from a number with no name that nobody answered and nothing since. Said: \"A couple of calls came in while you were out, sir, from a number the phone did not know.\" The meeting is over and nothing is owed on it; the calls are still outstanding, and outstanding beats finished.",
	)
}

// sure : How far the evidence goes, and what may be said at each
// distance.
//
// The judgement asked for here has not changed; what was missing was
// a scale to make it on. "Judge it as a person would" leaves two
// moves -- say it or do not -- and the useful third is to ask about a
// thing rather than assert it. A question is what a person does with
// evidence that points somewhere without proving it, and it costs
// almost nothing when it turns out to be wrong.
//
// Nothing here names a kind of event, because the distances are
// about how much the evidence establishes and not about what it is
// evidence of.
func sure() string {
	return prompt.Text(
		"How far the evidence goes decides how it may be said, and there are three distances.",
		"Where a thing is recorded outright, you may simply say it.",
		"Where several separate things point the same way and no one of them settles it -- the hour, how long it went on, a place that could be the same place -- you may ask about it, as somebody would who thought they already knew the answer. You may not state it.",
		"The whole of the difference is between asking how the film was and telling them they went to it: the first is still worth saying when it is wrong, and the second is not.",
		"Where it rests on a single thing that could be read two ways, say nothing -- and do not say that you are unsure either, because a doubt read out loud is worse than the silence it replaced.",
	)
}

// planned : What they had meant to do while they were out, and what
// to make of it.
//
// The instruction is to compare rather than to report. A place the
// phone recorded and a place the diary named are two accounts of the
// same afternoon, and which parts agree is the whole of what makes a
// greeting worth hearing: "how was the film" is a question only
// somebody who had both could ask.
//
// Deliberately not a rule about matching. The names will rarely be
// the same -- a cinema inside a mall is recorded as whatever shop
// the map had nearest, 127 metres away, on a road the address gives
// in full. Deciding that those are one place is the sort of thing a
// person does without effort and no amount of string comparison
// does at all.
func planned(t Told) string {
	if len(t.Were) == 0 {
		return ""
	}

	lines := make([]string, 0, len(t.Were)+8)
	lines = append(lines,
		"This was in their diary for the time they were away:")
	for _, a := range t.Were {
		when := a.When.In(t.Now.Location()).Format("3:04 pm")
		if a.AllDay {
			when = "all day"
		}
		line := "- " + strings.TrimSpace(a.What) + ", " + when
		if w := strings.TrimSpace(a.Where); w != "" {
			line += ", at " + w
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		"Compare it with where they actually were.",
		"The two will almost never use the same words. A cinema sits inside a mall and is recorded as whichever shop the map had nearest, sometimes a hundred metres away; the address beside the stay is what says whether that is the same place as the one in the diary -- the same road, the same part of town, at the same hour, for about the length of the thing.",
		"Judge it as a person would, on all of that together, and do not require the names to match.",
		"Where they plainly did it, that is the thing to say: ask how it was, by name, as somebody who knew they were going would.",
		"A thing being in the diary is not evidence that it happened; it is evidence that it was meant to. What makes it likely is the rest lining up -- the hour, the length, and a place that could be the same place.",
		"Where none of it lines up, be careful what you make of that. Nothing in front of you is a record of what did not happen: a device reports where it was and says nothing whatever about where it was not, and a quiet hour is as likely to be a quiet phone.",
		"So never conclude that they did not go, and never put it to them as though they had not. Being asked to account for a plan is not a greeting, and plans change for reasons that are nobody else's business.",
		"Where what is in front of you does cover those hours and plainly has them elsewhere, you may ask whether they got to it at all -- lightly, the way somebody asks who has no stake in the answer.",
		"This beats anything else here. Something they planned and did is the one thing in front of you that you know they care about, because they wrote it down.")
	return prompt.Text(lines...)
}

// coming : What is in the diary and what is still to be done.
//
// Given last among the facts because it is the least likely to be
// worth saying. The description of this greeting used to leave out
// everything still to come, on the grounds that a reminder waiting
// for four o'clock is not news at half past one and counting things
// at the door turns a greeting into a status report. That is still
// true of counting them; it was too strong about saying any of them.
// Somebody walking in at eleven at night with a meeting at nine is
// better off hearing it now than at nine.
//
// So: what is due before they would otherwise find out, and nothing
// else. The tasks that are merely open are a number, never a list.
func coming(t Told) string {
	lines := make([]string, 0, 8)
	for _, a := range t.Diary {
		when := a.When.In(t.Now.Location())
		day := "today"
		if when.YearDay() != t.Now.YearDay() || when.Year() != t.Now.Year() {
			day = "tomorrow"
		}
		line := "- " + day + " "
		if a.AllDay {
			line += "all day: " + a.What
		} else {
			line += when.Format("3:04 pm") + ": " + a.What
		}
		if a.Where != "" {
			line += ", at " + a.Where
		}
		lines = append(lines, line)
	}
	for _, c := range t.Chores.Due {
		line := "- to do: " + c.What
		switch {
		case c.Overdue:
			line += " (was due " + c.By.In(t.Now.Location()).Format("Monday 2 January") + ", not done)"
		case !c.By.IsZero():
			line += " (due " + c.By.In(t.Now.Location()).Format("Monday 2 January") + ")"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 && t.Chores.Others == 0 {
		return ""
	}

	said := prompt.Text(
		"What is coming, and what is still to be done.",
		"Say at most one of these, and only when it is soon enough that hearing it now is better than finding it out later --",
		"something first thing tomorrow, or something that was due and was not done.",
		"Nothing here is news on its own: they wrote it down, so they know it exists.",
		"Never read the list, never say how many there are, and never raise a thing that is hours away and obvious.",
	)
	if t.Chores.Others > 0 {
		said = prompt.Block(said, prompt.Text(
			fmt.Sprintf("There are %d other things on their lists that are not due yet.", t.Chores.Others),
			"That number is for judging whether one of the above stands out, and is not to be said.",
		))
	}
	if len(lines) == 0 {
		return said
	}
	return prompt.Block(said, prompt.Lines(lines...))
}

// when : The clock at the door, and how long it has been.
//
// The gap is given in words rather than as two timestamps because it is
// the thing that decides the greeting: ten minutes is "hello again" and
// nine hours is somebody who has had a day.
func when(t Told) string {
	now := t.Now.Format("Monday 2 January, 3:04 pm")
	if t.Since.IsZero() {
		return prompt.Text(
			"It is "+now+".",
			"You have not spoken to them before.",
		)
	}
	return prompt.Text(
		"It is "+now+".",
		"The last time you said anything to them was "+spoken(t.Now.Sub(t.Since))+" ago, at "+
			t.Since.In(t.Now.Location()).Format("3:04 pm")+".",
		"Anything from before that, you have already talked about. Do not bring it up again.",
	)
}

// reminders : The things that must actually be said.
//
// In the greeting rather than appended to it as fixed text, which is
// what this replaced. The owner's reason: once a model is writing the
// sentence there is no call for a second voice behind it, and a person
// would say "you were going to do X at four" rather than reciting a
// row. What a model must not do is lose one, so the instruction is to
// say every one of them.
func reminders(t Told) string {
	if len(t.Reminders) == 0 {
		return ""
	}
	lines := make([]string, 0, len(t.Reminders))
	for _, r := range t.Reminders {
		lines = append(lines, "- "+strings.TrimSpace(r))
	}
	return prompt.Block(
		prompt.Text(
			"These were due while they were out and have not been said yet. Say all of them, after the greeting.",
			"Say them as somebody would -- that they were going to do this, or that it was for an hour that has gone --",
			"and not as a list read back. Keep every one: leaving one out is the whole of the harm here.",
		),
		prompt.Lines(lines...),
	)
}

// profile : What is already known about them.
func profile(t Told) string {
	if strings.TrimSpace(t.Profile) == "" {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"This is what you have come to know about them. It is for judging what is ordinary for them and what is not.",
			"Do not repeat it back to them and do not greet them with a fact about themselves.",
		),
		t.Profile,
	)
}

// happened : The events, as plainly as they can be put.
//
// Written out with their kinds rather than interpreted here. What a kind
// means is the model's to work out -- the same method the description is
// written with -- so that a phone reporting something nobody has
// written code for still arrives as something it can read.
func happened(t Told) string {
	if len(t.Events) == 0 {
		return prompt.Text("Their devices reported nothing in between.")
	}

	// What a distance beside a place means. The number is in the
	// line either way; without this it reads as detail, when it is
	// the difference between knowing where they were and guessing.
	about := prompt.Text(
		"Where a place has a distance beside it, that is how far the named thing was from where they actually stopped, and it is how sure the name is.",
		"Twenty metres is inside it and may be said plainly.",
		"A hundred is across a road, and the name is the nearest thing any map had rather than where they were -- a cinema inside a mall is recorded as whichever shop was closest.",
		"At that distance name the area and not the place, or say nothing about which it was and ask instead.",
		"Never repeat a name from that far away as though it were a fact: being told they were at a restaurant they have never been to is worse than being told nothing.")

	// The spans first, and taken out before the cap. A stay re-
	// reports itself every five minutes while it is open and music
	// reports every track, so left among the rest they crowd out
	// everything else and then lose to the cap anyway. Folded, each
	// is one line, and they are the two that say what somebody was
	// actually doing.
	spans, evs := spanned(t.Events, t.Now.Location())
	if len(evs) > Most {
		evs = evs[len(evs)-Most:]
	}

	lines := make([]string, 0, len(evs)+len(spans))
	lines = append(lines, spans...)
	day := ""
	for _, r := range folded(evs) {
		at := r.first.In(t.Now.Location())
		if d := at.Format("Monday 2 January"); d != day {
			day = d
			lines = append(lines, d)
		}
		line := at.Format("3:04 pm") + "  " + r.shown
		if r.more != "" {
			line += "  (" + r.more + ")"
		}
		if r.times > 1 {
			line += fmt.Sprintf("  (%d times, the last at %s)",
				r.times, r.last.In(t.Now.Location()).Format("3:04 pm"))
		}
		if n := t.Usual[r.label]; n > 0 {
			line += fmt.Sprintf("  [%d in the past four weeks]", n)
		}
		lines = append(lines, line)
	}
	return prompt.Block(
		prompt.Text(
			"What their devices reported in between.",
			"The same thing happening more than once is one line saying how many:",
			"how many times is the thing worth noticing, and reading it out ten times is not.",
			"A line is the hour, then what the device called it, then what it was about, then anything else it said, then how often it has happened before.",
			"What the device called it is two words: the family of thing, and what happened to it, written from this person's side -- so one of them made a call and another was made to them, and the second word is what tells them apart. It is the device's wording and is never yours: do not say it back.",
			"The number in brackets is how often that same thing has happened over the past four weeks.",
			"It is there to tell you what is ordinary for them. Something that happens dozens of times a month is the texture of their life and not news, however much of it is in this window.",
			"More of it is still it. A count higher than usual is the same thing more often, and the mechanics of a day -- what a device did because they were somewhere, or moving, or awake -- are never the subject whatever the number says. Nobody has ever been glad to be told about them.",
			"The exception is anything with another person at the other end of it. Somebody doing a thing again and again is somebody; a device doing a thing again and again is a device.",
			"What is worth remarking on is what is rare. A thing with no number beside it has not happened before, which is itself worth a look.",
		),
		about,
		prompt.Lines(lines...),
	)
}

// run : One thing that happened, and how many times.
type run struct {
	label       string
	shown       string
	more        string
	first, last time.Time
	times       int
}

// Apart : How long a silence ends a sitting of music.
//
// A ride out with music on and a ride back with music on are two
// sittings and not one three-hour one, and the difference decides
// whether somebody could have been sitting in a cinema in between.
const Apart = 20 * time.Minute

// spanned : The stays and the listening as one line each, and
// everything else unchanged.
//
// These two used to be left out of the greeting altogether, on the
// grounds that a day of them is longer than a day of everything
// else -- which is true of the rows and not of the facts. Owner,
// looking at a window that had neither: *"stay should reach and
// also the music, only then it can know whether I went to movie or
// not."*
//
// Quite right, and they are the two that answer it. The diary says
// a film at ten past six; where somebody sat for three hours says
// whether they went, and music playing through those same hours
// says they did not. Neither was in front of the model.
//
// A span rather than a count, because that is what they are. A stay
// is one place for a stretch, however many times the phone said so
// while it was open; a sitting of music is one sitting, however
// many tracks.
func spanned(evs []event.Event, where *time.Location) ([]string, []event.Event) {
	type span struct {
		what            string
		first, last     time.Time
		minutes, tracks int
		detail          string
	}
	stays := map[string]*span{}
	var sittings []*span
	kept := make([]event.Event, 0, len(evs))

	for _, e := range evs {
		switch {
		case e.Kind == event.Stayed:
			at := valued(e)
			s, had := stays[at]
			if !had {
				s = &span{what: at, first: e.OccurredAt, last: e.OccurredAt}
				stays[at] = s
			}
			if e.OccurredAt.After(s.last) {
				s.last = e.OccurredAt
			}
			if m := numbered(e, "minutes"); m > s.minutes {
				s.minutes = m
			}
			if s.detail == "" {
				s.detail = rest(e)
			}
		case strings.HasPrefix(e.Kind, "music."):
			// A new sitting when the last one stopped long enough
			// ago. Folding every track of an evening into one span
			// said "listening until 9:23 pm" over music that played
			// on the way out and again on the way back, which reads
			// as three hours of it and is the opposite of the truth:
			// somebody listening through a film did not watch one.
			var at *span
			if n := len(sittings); n > 0 && e.OccurredAt.Sub(sittings[n-1].last) < Apart {
				at = sittings[n-1]
			} else {
				at = &span{first: e.OccurredAt, last: e.OccurredAt}
				sittings = append(sittings, at)
			}
			if e.OccurredAt.After(at.last) {
				at.last = e.OccurredAt
			}
			at.tracks++
			if who := valued(e); who != "" && who != event.Unknown {
				at.what = who
				at.detail = rest(e)
			}
		default:
			kept = append(kept, e)
		}
	}

	out := make([]string, 0, len(stays)+1)
	order := make([]*span, 0, len(stays))
	for _, s := range stays {
		order = append(order, s)
	}
	sort.Slice(order, func(i, j int) bool { return order[i].first.Before(order[j].first) })
	for _, s := range order {
		// Reported, not worked out. The duration is the sender's own
		// and the row times are only when it said so; computing an
		// end from the two produced a stay at home of nearly
		// fourteen hours that ran straight through an evening out,
		// because stays sharing a name are rejoined elsewhere.
		line := s.first.In(where).Format("3:04 pm") + "  stayed at " + s.what
		if s.minutes > 0 {
			line += ", reported as " + spell(time.Duration(s.minutes)*time.Minute)
		}
		if s.last.After(s.first) {
			line += ", still being reported at " + s.last.In(where).Format("3:04 pm")
		}
		out = append(out, line)
	}
	for _, m := range sittings {
		line := m.first.In(where).Format("3:04 pm") + "  listening"
		if m.last.After(m.first) {
			line += " until " + m.last.In(where).Format("3:04 pm")
		}
		if m.what != "" {
			line += ", " + m.what
		}
		if m.detail != "" {
			line += "  (" + m.detail + ")"
		}
		out = append(out, line)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out, kept
}

// valued : A sender's own word for the thing.
func valued(e event.Event) string { return value(e) }

// numbered : A whole number out of the payload, or zero.
func numbered(e event.Event, field string) int {
	var fields map[string]any
	if json.Unmarshal(e.Payload, &fields) != nil {
		return 0
	}
	switch v := fields[field].(type) {
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// folded : The same thing happening repeatedly, as one line with a
// count.
//
// Without this the window is whatever repeated most, and what repeats
// most is never what matters. Measured on real readings: three hours
// produced thirteen events, almost all of one network going and coming
// back, and the greeting written from them asked whether everything was
// all right with the connection -- true, useless, and the one thing in
// the window nobody wanted raised at a door.
//
// Folded rather than dropped, because the count is the signal. Somebody
// ringing three times in twenty minutes is exactly the thing worth
// asking about, and a rule that threw away repeats would throw that
// away first. Ordered by when each thing first happened, so a sequence
// still reads as a sequence.
func folded(evs []event.Event) []run {
	out := make([]run, 0, len(evs))
	at := map[string]int{}
	for _, e := range evs {
		label := Label(e)
		if i, seen := at[label]; seen {
			out[i].times++
			if e.OccurredAt.After(out[i].last) {
				out[i].last = e.OccurredAt
			}
			continue
		}
		at[label] = len(out)
		out = append(out, run{label: label, shown: shown(e), more: rest(e),
			first: e.OccurredAt, last: e.OccurredAt, times: 1})
	}
	return out
}

// Label : How an event is written on a line, and the key its count is
// kept under.
//
// Exported so that whatever counts what is ordinary labels things the
// same way this does. Two spellings of one event is a count that never
// matches a line, which fails silently and reads as everything being
// unprecedented.
func Label(e event.Event) string {
	if v := value(e); v != "" {
		return e.Kind + " " + v
	}
	return e.Kind
}

// shown : How a line names the thing, which is not always how it is
// counted.
//
// A caller with no name is labelled by their number so that one
// stranger can be told from another, and read out that is eleven
// digits nobody asked to hear. The count stays on the number; only
// the words change.
func shown(e event.Event) string {
	what := Readable(e.Kind)
	v := value(e)
	if v == "" || v == event.Unknown {
		// Nothing to name. The word "unknown" was being printed in
		// its place, which reads as an unknown something rather than
		// as a thing with nothing to name -- a screen locking does
		// not have a subject, and "screen.locked unknown" invited
		// the model to wonder what was unknown about it.
		return what
	}
	return what + ": " + event.Readable(v)
}

// Readable : A kind as words, without knowing what any kind is.
//
// Exported so a test can check that a count lands on the line it
// belongs to. Two spellings of one event is a count that never
// matches, which fails silently and reads as everything being
// unprecedented.
//
// The dot becomes a space and nothing else moves. Senders write a
// kind as a family and what happened to it, so "call missed" and
// "wifi joined" fall out for free.
//
// Putting the verb first was tried and is worse: it reads well for
// the kinds that end in one and badly for the kinds that do not,
// and "level battery: 44" is not an improvement on anything.
//
// Generic on purpose. A table turning call.made into "an outgoing
// call to" would read better today and would be wrong the morning a
// new sender appears -- three were added on the phone last week with
// no change here, and each read correctly the same afternoon.
func Readable(kind string) string {
	family, happened, ok := strings.Cut(kind, ".")
	if !ok || happened == "" {
		return kind
	}
	return family + " " + happened
}

// value : The one field every device writes, or nothing.
func value(e event.Event) string {
	var into struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(e.Payload, &into); err != nil {
		return ""
	}
	return strings.TrimSpace(into.Value)
}

// Seconds : The field a sender uses for how long something ran.
//
// Named in internal/event, beside the rest of what a payload may
// say about itself.
const Seconds = event.Seconds

// span : A number of seconds as somebody would say it.
//
// Nothing under a minute is worth a number: it is brief, and that is
// the whole of what anybody would say about it. A zero is not even
// that -- a call recorded as lasting no time is a call that did not
// happen, and saying so invited "it may not have connected
// properly", which nothing had reported.
func span(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d <= 0:
		return ""
	case d < time.Minute:
		return "brief"
	case d < time.Hour:
		return spell(d.Round(time.Minute))
	default:
		return spell(d.Round(time.Minute))
	}
}

// number : A JSON number however the sender wrote it.
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	}
	return 0, false
}

// unwrapped : A value that is itself a payload, as its own fields.
func unwrapped(v string) ([]string, bool) {
	if !strings.HasPrefix(v, "{") || !strings.HasSuffix(v, "}") {
		return nil, false
	}
	var into map[string]any
	if json.Unmarshal([]byte(v), &into) != nil {
		return nil, false
	}
	names := make([]string, 0, len(into))
	for name := range into {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s %v", name, into[name]))
	}
	return out, len(out) > 0
}

// rest : Everything else the payload carries, as "name value" pairs.
//
// The label is "value" alone, because that is what a count has to be
// keyed on: a thing whose label changed every time could never be
// counted, which is what made the raw position readings invisible. But
// everything else in the payload was being thrown away with it, and
// for some kinds that is the whole of the meaning -- a stay is a place
// and a number of minutes, and the model was handed the place and left
// to guess the rest from a timestamp.
//
// Sorted, so a line reads the same way twice. Objects and arrays are
// left out: they do not belong in a sentence said out loud, and
// anything that needs them is better off sending a second event.
func rest(e event.Event) string {
	var into map[string]any
	if err := json.Unmarshal(e.Payload, &into); err != nil {
		return ""
	}
	names := make([]string, 0, len(into))
	for name := range into {
		if name != "value" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	out := make([]string, 0, len(names))
	for _, name := range names {
		// How long a thing ran, in words. A sender writing a field
		// called seconds has said it is reporting a span, which is a
		// unit and not a kind of event, so naming it here breaks
		// nothing. Owner: "don't need to say exact 19 secs, 23 secs,
		// those are unwanted." internal/tool/events has done this
		// since that was said; the one renderer that is read aloud
		// had been left out of it.
		if name == Seconds {
			if n, ok := number(into[name]); ok {
				if said := span(n); said != "" {
					out = append(out, said)
				}
				continue
			}
		}
		switch v := into[name].(type) {
		case string:
			// Not a telephone number, in any field.
			//
			// The value is kept readable by Readable, which turns a
			// bare number into "an unknown number". This is the other
			// half: a call carries the number alongside the name, so
			// without this the digits reach the prompt anyway through
			// the field the masking does not look at, and get read
			// out in a room. Nothing is lost -- when the number is
			// the only identity there is, it is already the value.
			v = strings.TrimSpace(v)
			// A payload inside a payload. One sender double-encodes
			// its own, and the braces and quotes were reaching a
			// prompt that is read out loud.
			if inner, ok := unwrapped(v); ok {
				out = append(out, inner...)
				continue
			}
			// A field saying false is the absence of a thing, and
			// printing it says there is one. The boolean case below
			// has always dropped these; a sender that writes them as
			// words got "urgent false" and "playing false" onto a
			// line that is read aloud.
			if strings.EqualFold(v, "false") {
				continue
			}
			if v != "" && v != event.Unknown && !event.Number(v) && !event.Position(v) {
				out = append(out, name+" "+v)
			}
		case float64:
			out = append(out, fmt.Sprintf("%s %g", name, v))
		case bool:
			if v {
				out = append(out, name)
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, ", ")
}

// spoken : A duration as somebody would say it.
func spoken(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d < 48*time.Hour:
		return "a day"
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}

// Dark : How long nothing may be reported before the silence is
// itself worth knowing about.
//
// The phone reports a position every five minutes and something
// else most of the time. Twenty minutes of nothing is a lull;
// ninety is the phone off, flat, or out of signal, and that is
// usually the largest thing that happened.
const Dark = 90 * time.Minute

// silent : The stretches where nothing was reported at all.
//
// Absence is the one thing nothing else in this prompt can show.
// Every other line is something that happened; a gap is the shape
// of the time between, and without it a window with little in it
// reads as a day with little in it.
//
// Working material, and said so plainly, because it did not used to
// be. It offered the usual reasons for a gap -- off, flat, no signal
// -- and allowed the model to say the one that seemed plain. Over
// twenty-two sampled arrivals that produced "your phone went quiet
// just before six this morning, sir, did it go flat?" and "your
// phone was off the whole time, sir, was the walk long?", neither
// of which anything had reported. It was the block naming the
// inventions and then licensing them.
//
// Worked out here rather than given, because it is a reading of the
// same events and not another fact to fetch.
func silent(t Told) string {
	from := t.Since
	if from.IsZero() {
		if len(t.Events) == 0 {
			return ""
		}
		from = t.Events[0].OccurredAt
	}

	type gap struct{ from, to time.Time }
	var gaps []gap
	last := from
	for _, e := range t.Events {
		if e.OccurredAt.Sub(last) >= Dark {
			gaps = append(gaps, gap{last, e.OccurredAt})
		}
		if e.OccurredAt.After(last) {
			last = e.OccurredAt
		}
	}
	if t.Now.Sub(last) >= Dark {
		gaps = append(gaps, gap{last, t.Now})
	}
	if len(gaps) == 0 {
		return ""
	}

	lines := make([]string, 0, len(gaps)+5)
	lines = append(lines, "Their devices reported nothing at all for these stretches:")
	for _, g := range gaps {
		lines = append(lines, fmt.Sprintf("- %s to %s, which is %s of nothing",
			g.from.In(t.Now.Location()).Format("3:04 pm"),
			g.to.In(t.Now.Location()).Format("3:04 pm"),
			spell(g.to.Sub(g.from))))
	}
	lines = append(lines,
		"A gap is not an absence of anything happening; it is an absence of anything being reported, which is a different thing.",
		"It is here so that a thin window is not read as a quiet day. It is not itself a thing to say: they know what their phone was doing better than you do, and whatever it was, telling them is telling them nothing.",
		"Never offer a reason for it. A flat battery, a place with no signal, a phone somebody switched off -- none of those was reported, all of them are guesses, and a guess about why nothing was recorded is the easiest thing here to invent and the least worth inventing.",
		"Do not ask them to account for the time either.")
	return prompt.Text(lines...)
}

// spell : A stretch of time as somebody would say it.
func spell(d time.Duration) string {
	mins := int(d.Round(time.Minute).Minutes())
	switch {
	case mins < 120:
		return fmt.Sprintf("%d minutes", mins)
	case mins%60 == 0:
		return fmt.Sprintf("%d hours", mins/60)
	default:
		return fmt.Sprintf("%d hours %d minutes", mins/60, mins%60)
	}
}
