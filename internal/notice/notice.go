// Package notice decides when the assistant has something worth
// saying that nobody asked for.
//
// The want, in the owner's words: it should act like someone in the
// room. Somebody in your room does not read out what you just did --
// you were there -- and does not sit in silence either. They mention
// the thing you would not have noticed.
//
// So nothing here reacts to an event happening. Events happening is
// the normal state of the world and there are hundreds a day. What
// is worth a remark is an event being *different*: more of it than
// usual, or a thing never seen before.
//
// Nothing in this package names a kind of event, and nothing may.
// The senders describe themselves and more of them are coming; a
// rule that knows what a call or a song is stops working the day a
// new one is added. A thing is whatever label it carries.
//
// Two kinds are left out, and both are mechanism rather than
// judgement: the raw position readings, which arrive every few
// minutes and whose values are all distinct, so every one of them
// would look new; and the music churn the stays and states are
// worked out from. What they are turned into is here instead.
package notice

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// Burst : How close together repeats have to be to be worth
// mentioning.
//
// The owner's example is a third missed call from the same number.
// Three in a quarter of an hour is somebody trying to reach you;
// three across a week is just somebody who calls.
const Burst = 15 * time.Minute

// Insistent : How many times inside Burst before it is remarked on.
//
// Three rather than two, for the reason counts are used everywhere
// else here: two of anything is a coincidence, and a remark made on
// a coincidence is worse than one not made.
const Insistent = 3

// Reason : Why something is worth saying.
//
// Not what happened -- that is the label -- but what makes it
// different from the ordinary run of things. The model is given this
// so it can say the interesting half rather than reading the event
// out.
type Reason string

const (
	// Often : More of it, faster, than usual.
	Often Reason = "often"
	// Novel : Nothing like it in the record.
	Novel Reason = "novel"
	// Told : The sender said this must be said.
	//
	// Not a judgement at all, which is the point. A battery at five
	// per cent and a phone too hot to hold are not interesting
	// because they are unusual; they are worth saying because of
	// what they are, and a model weighing whether to mention them is
	// a model that will one day decide not to.
	Told Reason = "told"
)

// Urgent : The payload field a sender sets to say that something
// must be said, whatever else is going on.
//
// A convention rather than a list of kinds here, for the reason
// everything else is: the senders describe themselves. Which battery
// level is worth a warning is the person's own preference and it
// belongs on the device that knows it, not in a threshold compiled
// into this server. The phone decides it is urgent; this decides how
// to say it.
const Urgent = "urgent"

// Candidate : Something that might be worth saying.
//
// Might. This is what the cheap half found; whether it is worth
// interrupting somebody for is a judgement, and judgement is not
// made here.
type Candidate struct {
	// Label : The thing, as its own sender described it.
	Label string
	// Reason : What makes it worth a look.
	Reason Reason
	// Times : How many times inside Burst, for Often. One for Novel.
	Times int
	// Before : How many times it has happened in the whole history
	// behind this. Zero is what makes a thing Novel.
	Before int
	// At : When the latest of them happened.
	At time.Time
	// Urgent : Whether the sender said this must be said.
	//
	// Bypasses the quiet between remarks and the once-a-day rule,
	// and is never refused by the model. It does not bypass there
	// being nobody to tell: it goes wherever they are instead.
	Urgent bool
	// Detail : Whatever else the sender said about it, which the
	// label deliberately leaves out.
	//
	// A label is the kind and the value and nothing else, so that two
	// of a thing are counted as two of the same thing. But a player
	// reports the artist as the value and the track beside it, and a
	// remark about music that cannot name the song is not worth
	// making. So the counting uses the label and the sentence uses
	// this.
	Detail string
}

// Find : What among the things that just arrived might be worth
// saying, given everything behind them.
//
// Generous on purpose. Being quiet is decided twice after this --
// once by a model that may answer with nothing, and once by a gate
// that will not allow two remarks close together -- and a candidate
// that never gets made cannot be rescued by either.
//
// history is everything known before this moment, arrived included
// or not; a thing is counted Novel on what came before it, so
// passing the same slice twice does not make everything familiar.
func Find(arrived, history []event.Event, now time.Time) []Candidate {
	seen := map[string]int{}
	for _, e := range history {
		if skip(e) {
			continue
		}
		if e.OccurredAt.After(now) {
			continue
		}
		seen[Label(e)]++
	}

	// What turned up just now, newest last, so the moment reported is
	// the latest one rather than whichever came out of the store
	// first.
	lately := map[string]int{}
	latest := map[string]time.Time{}
	detail := map[string]string{}
	state := map[string]bool{}
	urgent := map[string]bool{}
	order := []string{}
	for _, e := range arrived {
		if skip(e) {
			continue
		}
		label := Label(e)
		if _, had := latest[label]; !had {
			order = append(order, label)
		}
		if Toggling(e.Payload) {
			state[label] = true
		}
		if told(e.Payload) {
			urgent[label] = true
		}
		if e.OccurredAt.After(latest[label]) || detail[label] == "" {
			latest[label] = e.OccurredAt
			detail[label] = Beside(e.Payload)
		}
	}

	// Repeats are counted over the window rather than over what
	// arrived, because a burst split across two ticks is still a
	// burst.
	from := now.Add(-Burst)
	for _, e := range history {
		if skip(e) || e.OccurredAt.Before(from) || e.OccurredAt.After(now) {
			continue
		}
		lately[Label(e)]++
	}

	out := make([]Candidate, 0, len(order))
	for _, label := range order {
		// Said because the sender said so, whether or not it is
		// unusual. A battery at five per cent has been at five per
		// cent before and is not news; it is still worth saying.
		if urgent[label] {
			out = append(out, Candidate{
				Label: label, Reason: Told, Urgent: true,
				Times: lately[label], Before: seen[label] - lately[label],
				At: latest[label], Detail: detail[label],
			})
			continue
		}
		switch {
		case lately[label] >= Insistent && !state[label]:
			out = append(out, Candidate{
				Label: label, Reason: Often,
				Times: lately[label], Before: seen[label] - lately[label],
				At: latest[label], Detail: detail[label],
			})
		case seen[label] <= lately[label]:
			// Nothing of it before this window, so this is the first
			// time it has ever been reported.
			out = append(out, Candidate{
				Label: label, Reason: Novel, Times: 1, Before: 0,
				At: latest[label], Detail: detail[label],
			})
		}
	}
	return out
}

// Label : A thing as its sender described it: what kind it is and
// which one.
//
// The same shape the counted half of the description uses, so a
// remark and a habit are talking about the same thing.
func Label(e event.Event) string {
	value := valueOf(e.Payload)
	if value == "" || value == event.Unknown {
		return e.Kind
	}
	return e.Kind + " " + value
}

// skip : What is never worth a remark whatever it says.
//
// A coordinate only. Every one of them is distinct, so unchecked
// every reading would look like something never seen before, and
// none of them is a thing to say a sentence about.
func skip(e event.Event) bool {
	return event.Position(valueOf(e.Payload))
}

// Toggling : Whether a sender is reporting a state rather than
// something that happened.
//
// A state is not an occurrence. A player reporting that it is
// playing, then paused, then playing is one song being listened to,
// not three of anything, and counting the toggles would have the
// assistant remark on somebody using a pause button. A screen
// reporting locked and unlocked fifty times a day is the same thing
// again. Such a sender can still report something new -- which is
// the half worth having -- but it can never be frequent.
//
// Read from the payload rather than from a list of kinds here. The
// senders describe themselves and more of them are coming; a list
// means every new toggle the person adds needs a line in this
// server before it stops being a nuisance, which is exactly the
// coupling the rest of this design avoids. A field that is true or
// false says what the event is, and the sender has already written
// it: playing, connected, locked.
//
// The value itself does not count. A thing whose whole value is
// "true" is a sender saying something happened, not a sender
// describing a state of something.
func Toggling(payload json.RawMessage) bool {
	if len(payload) == 0 {
		return false
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return false
	}
	for name, said := range fields {
		if name == "value" {
			continue
		}
		switch v := said.(type) {
		case bool:
			return true
		case string:
			if strings.EqualFold(strings.TrimSpace(v), "true") ||
				strings.EqualFold(strings.TrimSpace(v), "false") {
				return true
			}
		default:
			_ = v
		}
	}
	return false
}

// valueOf : The value a sender gave, if it gave one.
func valueOf(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil || into.Value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(into.Value))
}

// Beside : Whatever a sender said about a thing apart from its value,
// as one readable line.
//
// Every field but the one already in the label, in a fixed order so
// the same event always reads the same way. Nothing here knows what
// any of the names mean, and it does not need to: the model is being
// shown what the device said, not told what it is.
func Beside(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "value" || fields[k] == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		said := strings.TrimSpace(fmt.Sprint(fields[k]))
		if said == "" || len([]rune(said)) > 120 {
			continue
		}
		parts = append(parts, k+": "+said)
	}
	return strings.Join(parts, ", ")
}

// told : Whether a sender marked this as something that must be
// said.
//
// True or the word true, so a device that sends booleans and one
// that sends strings are both understood -- the same latitude
// Toggling allows, and for the same reason: nobody is going to read
// this server's source before writing a profile on a phone.
func told(payload json.RawMessage) bool {
	if len(payload) == 0 {
		return false
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return false
	}
	switch v := fields[Urgent].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}
