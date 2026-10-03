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
)

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
	order := []string{}
	for _, e := range arrived {
		if skip(e) {
			continue
		}
		label := Label(e)
		if _, had := latest[label]; !had {
			order = append(order, label)
		}
		if e.OccurredAt.After(latest[label]) {
			latest[label] = e.OccurredAt
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
		switch {
		case lately[label] >= Insistent:
			out = append(out, Candidate{
				Label: label, Reason: Often,
				Times: lately[label], Before: seen[label] - lately[label],
				At: latest[label],
			})
		case seen[label] <= lately[label]:
			// Nothing of it before this window, so this is the first
			// time it has ever been reported.
			out = append(out, Candidate{
				Label: label, Reason: Novel, Times: 1, Before: 0, At: latest[label],
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

// skip : Working material, which is not history and must not be
// remarked on.
func skip(e event.Event) bool {
	if event.Working(e.Kind) {
		return true
	}
	// A coordinate is never a thing to say something about, and every
	// one of them is distinct, so unchecked they would all look new.
	return event.Position(valueOf(e.Payload))
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
