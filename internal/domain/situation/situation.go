// Package situation speaks when several facts together mean
// something that none of them means alone.
//
// The third kind of thing said unasked, and the one the owner asked
// for in his own words: *"Tasker detects facts; Go understands
// situations; Jarvis speaks only when the situation changes what you
// should do."*
//
// It is deliberately not the machinery deleted on 5 October. That
// one invented its own reasons to speak -- three of a thing inside
// fifteen minutes, a thing never seen before -- and was wrong about
// every one of them, because an unusual event and an event worth
// interrupting somebody for are not the same thing. These are rules
// the owner stated, worked out by arithmetic, with no judgement in
// them at all. A check either holds or it does not.
//
// Each check is a pure function over what the devices reported. The
// watcher around them does the fetching, the room, and saying each
// thing once. Adding one is a function and a line, which is the
// point: the next situation worth knowing about should not need a
// new loop.
package situation

import (
	"encoding/json"
	"time"

	"personal-assistant/internal/base/world/event"
)

// Found : Something worth saying, and what makes it this thing
// rather than another.
type Found struct {
	// Key : What this is, so it is said once. Two different
	// situations never share one, and the same situation returning
	// on the next tick always does.
	Key string
	// Says : The words, already a sentence.
	Says string
}

// Check : One situation, worked out from what was reported.
//
// Given the window and the clock and nothing else: a check that
// needed to fetch something would be a check nobody could test.
type Check func(evs []event.Event, now time.Time) (Found, bool)

// Insisting : How many times somebody has to try before it is worth
// saying while they are sitting there.
//
// Two, not one. One unanswered call is ordinary and they will see
// it; the same person trying again is a person who needs something,
// and that is the difference between a notification and a reason to
// pick the phone up.
const Insisting = 2

// Trying : That somebody has tried more than once and not got
// through.
//
// The greeting already says this when they walk in. This says it
// while they are sitting in the room, which is the half that lets
// them do something about it.
func Trying(evs []event.Event, now time.Time) (Found, bool) {
	open, _ := event.Reaching(evs)
	for _, r := range open {
		if r.Tries < Insisting {
			continue
		}
		return Found{
			Key:  "trying:" + r.Who,
			Says: r.Named() + " has tried to reach you more than once, sir, and has not got through.",
		}, true
	}
	return Found{}, false
}

// unmarshal : A payload into whatever was asked for.
func unmarshal(e event.Event, into any) error {
	return json.Unmarshal(e.Payload, into)
}
