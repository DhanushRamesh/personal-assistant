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
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
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
// Five, so a slow one still lands. The ceiling is not Home Assistant --
// it gives up on the request at ten seconds and the greeting outlives
// that on purpose -- it is somebody standing in a doorway waiting to be
// spoken to, and the reminders still have to be read out after this.
const Within = 5 * time.Second

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
	// Fallback : The fixed greeting, said when there is no time to
	// think of a better one.
	Fallback string
	// Reminders : Anything held back while they were out, and anything
	// that was never said at all, in the words they would be said in.
	// The model is told to say these and not to summarise them away.
	Reminders []string
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

// Prompt : What the model is asked.
//
// Exported so an eval builds the same thing the server does.
func Prompt(t Told) string {
	return prompt.Block(
		prompt.Text(
			"Somebody has just walked in to the room their assistant is in.",
			"You are that assistant, speaking aloud to them, and you have known them for a while.",
			"Write what you say. One or two sentences, out loud, the way a person greets somebody they know.",
		),
		when(t),
		prompt.Text(
			"Below is everything their own devices reported while the two of you were not talking.",
			"Read it the way somebody who knows them would, and work out whether anything in it is worth mentioning.",
			"Something that happened far more often than usual, or lasted far longer than usual,",
			"or involved the same person again and again, or is simply out of keeping with their ordinary day, is worth remarking on.",
			"Where it is, say so and ask about it, the way somebody would: a short question, not a report.",
			"Where nothing stands out -- and most of the time nothing will -- just greet them, and say nothing about the events at all.",
		),
		prompt.Text(
			"Never list what the devices reported, never read out a count, and never use the names the events are written under.",
			"You noticed something; you did not read a log.",
			"One thing at most. Somebody who comes in and is asked three questions has been interrogated, not greeted.",
		),
		reminders(t),
		prompt.Text(
			"It is said out loud in a room that may have other people in it.",
			"Where mentioning something would mean saying aloud who called them or what they were doing, keep it to what they would not mind overheard,",
			"and let them be the one to say the rest.",
		),
		prompt.Text(
			"Only what is in front of you. Do not invent an event, a person or a reason,",
			"and do not guess at what something meant -- ask them instead, which is the point of asking.",
			"If there is nothing here at all, greet them and stop.",
		),
		prompt.Text(
			"Plain spoken sentences. No lists, no headings, no stage directions, and nothing they would have to read rather than hear.",
			"Do not say you are an assistant and do not offer to help: they know, and they will ask.",
		),
		profile(t),
		happened(t),
	)
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

	evs := t.Events
	if len(evs) > Most {
		evs = evs[len(evs)-Most:]
	}

	lines := make([]string, 0, len(evs))
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
			"The number in brackets is how often that same thing has happened over the past four weeks.",
			"It is there to tell you what is ordinary for them. Something that happens dozens of times a month",
			"is the texture of their life and not news, however much of it is in this window;",
			"what is worth remarking on is what is rare, or far more of something than there usually is.",
			"A thing with no number beside it has not happened before, which is itself worth a look.",
		),
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
	if v := value(e); v != "" {
		return e.Kind + " " + event.Readable(v)
	}
	return e.Kind
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
		switch v := into[name].(type) {
		case string:
			if v = strings.TrimSpace(v); v != "" {
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
