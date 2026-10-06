// Package notice decides when the assistant has something worth
// saying that nobody asked for.
//
// One thing is, and only one: an event whose sender marked it to be
// said at once. The phone knows its battery is at five per cent and
// says so in the payload; this reads that mark, checks there is
// somebody in the room, and says it.
//
// It used to judge as well -- more of a thing than usual, or a thing
// never seen before -- and the judging is gone. Owner, 5 October
// 2026, looking at *"Sir, your phone has been unlocked three times
// in the last few minutes"*: *"no need for these kind of unasked
// announcement."* The two reasons it could invent were the two it
// was wrong about, because an unusual event and an event worth
// interrupting somebody for are not the same thing, and nothing
// here can tell which it has. A sender can.
//
// So nothing here names a kind of event, and nothing may. The
// senders describe themselves and more of them are coming; a rule
// that knows what a call or a song is stops working the day a new
// one is added. A thing is whatever label it carries, and whether
// it is worth saying is whatever its sender said.
package notice

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// Reason : Why something is worth saying.
//
// One value, and kept as a type with one value on purpose: it is
// written down with every row the watcher considers, and a column
// that has only ever held one word is how the next person reading
// the page knows there was never a second kind.
type Reason string

const (
	// Immediate : The sender said this is to be said now.
	//
	// The only reason there is. Not a judgement, which is the
	// point: a battery at five per cent is not interesting because
	// it is rare, it is worth saying because of what it is, and the
	// device that measured it is the only thing here that knows
	// that.
	//
	// Owner, when this was one of three: *"it's not urgent, name it
	// immediate announcement. Event arrives, check if present,
	// announce. It's simple. This is separate and the other unasked
	// is separate."* The other unasked is now gone and this is what
	// is left.
	Immediate Reason = "immediate"
)

// Candidate : Something to be said.
//
// Not a maybe any more. The sender has already decided; what is
// left is whether there is anybody to hear it, and what words to
// put it in.
type Candidate struct {
	// Label : The thing, as its own sender described it.
	Label string
	// Reason : What makes it worth a look.
	Reason Reason
	// At : When the latest of them happened.
	At time.Time
	// Immediate : Whether the sender said to announce it at once.
	//
	// Always true, and kept rather than assumed. The gate reads it
	// to know that the quiet between remarks does not apply, and a
	// gate that inferred that from the absence of anything else
	// would start saying things the day something else appeared.
	Immediate bool
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

// Find : What among the things that just arrived is to be said.
//
// One candidate per label, newest wins. A burst of six events when
// somebody walks through the door is six events and at most one
// thing to say about each distinct thing in it; the watcher looks
// on a tick for the same reason.
//
// No history, and that is the change. Judging whether a thing was
// unusual needed four weeks of events read on every tick; reading
// a mark the sender already wrote needs the last few minutes.
func Find(rules []Rule, arrived []event.Event, now time.Time) []Candidate {
	latest := map[string]time.Time{}
	detail := map[string]string{}
	order := []string{}

	// Oldest first, so that "what did this kind say before" is a
	// question with an answer. The direction of a value is the
	// whole of what stops a phone charging through fifteen per
	// cent announcing fifteen per cent.
	inOrder := append([]event.Event(nil), arrived...)
	sort.SliceStable(inOrder, func(i, j int) bool {
		return inOrder[i].OccurredAt.Before(inOrder[j].OccurredAt)
	})
	before := map[string]string{}

	for _, e := range inOrder {
		was := before[e.Kind]
		before[e.Kind] = valueOf(e.Payload)
		if skip(e) || e.OccurredAt.After(now) || !worth(rules, e, was) {
			continue
		}
		label := Label(e)
		if _, had := latest[label]; !had {
			order = append(order, label)
		}
		if e.OccurredAt.After(latest[label]) || detail[label] == "" {
			latest[label] = e.OccurredAt
			detail[label] = Beside(e.Payload)
		}
	}

	out := make([]Candidate, 0, len(order))
	for _, label := range order {
		out = append(out, Candidate{
			Label: label, Reason: Immediate, Immediate: true,
			At: latest[label], Detail: detail[label],
		})
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
