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
	"strconv"
	"strings"
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

const (
	// Stuck : How long a charger may make no difference before it is
	// worth saying.
	//
	// Twenty minutes. A phone on a working charger gains a per cent
	// every few minutes; twenty with no change at all is a cable, a
	// socket, or a charger, and all three are mended in seconds by
	// somebody who knows.
	Stuck = 20 * time.Minute

	// Settling : How long the phone's own word that nothing is going
	// in has to stand before it is worth saying.
	//
	// Two minutes. Android reports not_charging for a few seconds
	// while a cable is being seated and again while a hot phone
	// pauses to cool, and neither of those is a fault. Waiting the
	// twenty below would throw away the whole advantage of having
	// been told outright.
	Settling = 2 * time.Minute

	// Full : The level past which standing still is the battery being
	// full rather than the charger being broken.
	Full = 95
)

// NotCharging : That it is plugged in and nothing is happening.
//
// The failure nobody notices until morning. Worth saying while they
// are in the room and the cable is an arm's length away, and worth
// saying nowhere else.
//
// It happens two ways and both are worth the same sentence. The
// phone can say not_charging, which is the cable not seated or a
// dead socket, and that needs no working out. Or it can say
// charging while the level sits where it was, which is a charger
// or a cable too weak to show -- or the phone spending it as fast
// as it arrives. Neither device can see how many amps are going
// in, so both start the same twenty minutes.
func NotCharging(evs []event.Event, now time.Time) (Found, bool) {
	var newest event.Event
	levels := make([]event.Event, 0, 16)
	for _, e := range evs {
		switch {
		case strings.HasPrefix(e.Kind, "power."):
			// The newest word on the charger wins, whichever it was.
			if e.OccurredAt.After(newest.OccurredAt) {
				newest = e
			}
		case strings.HasSuffix(e.Kind, ".level"):
			levels = append(levels, e)
		}
	}

	// Told outright, so there is nothing to work out and no twenty
	// minutes to wait for: the phone has said the cable is in and
	// nothing is coming through it.
	if strings.HasSuffix(newest.Kind, ".not_charging") {
		if now.Sub(newest.OccurredAt) < Settling {
			return Found{}, false
		}
		return Found{
			Key:  "notcharging:" + newest.OccurredAt.UTC().Format(time.RFC3339),
			Says: "Your phone is on charge" + standing(newest) + ", sir, but nothing is going in. The cable may not be in properly.",
		}, true
	}

	// On the cable, however it was put that way. Current that is
	// going in but never shows is as much a fault as none at all,
	// so charging earns the same scrutiny a bare socket does.
	var plugged time.Time
	flowing := strings.HasSuffix(newest.Kind, ".charging")
	if flowing || strings.HasSuffix(newest.Kind, ".plugged") {
		plugged = newest.OccurredAt
	}
	if plugged.IsZero() || now.Sub(plugged) < Stuck {
		return Found{}, false
	}

	// What it said when it went on, and what it says now. Equal, over
	// that long, is a charger doing nothing.
	at, atOK := reading(levels, plugged, true)
	last, lastOK := reading(levels, plugged, false)
	if !atOK || !lastOK || last > at || last >= Full {
		return Found{}, false
	}
	if now.Sub(lastAt(levels)) > Stuck {
		// Nothing reported lately, so nothing is known lately.
		return Found{}, false
	}
	// Which fault to name. Told it is charging, the cable is in and
	// seated and saying so would send them to check a thing that is
	// not wrong; what is wrong is how little is arriving.
	blame := " per cent, sir. The cable may not be in properly."
	if flowing {
		blame = " per cent, sir. It says it is charging, so the charger or the cable is giving very little."
	}
	return Found{
		Key: "notcharging:" + plugged.UTC().Format(time.RFC3339),
		Says: "Your phone has been on charge a while and is still at " +
			strconv.Itoa(last) + blame,
	}, true
}

// standing : Where the battery had got to, said as a clause, or
// nothing at all when the sender left the level out.
func standing(e event.Event) string {
	var into struct {
		Level string `json:"level"`
	}
	if unmarshal(e, &into) != nil {
		return ""
	}
	n, err := strconv.Atoi(strings.TrimSpace(into.Level))
	if err != nil {
		return ""
	}
	return " and still at " + strconv.Itoa(n) + " per cent"
}

// reading : The level at the moment the charger went on, or the
// newest one since.
func reading(levels []event.Event, since time.Time, first bool) (int, bool) {
	best, found := 0, false
	var when time.Time
	for _, e := range levels {
		if e.OccurredAt.Before(since) {
			continue
		}
		if found && ((first && e.OccurredAt.After(when)) || (!first && e.OccurredAt.Before(when))) {
			continue
		}
		var into struct {
			Value string `json:"value"`
		}
		if unmarshal(e, &into) != nil {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(into.Value))
		if err != nil {
			continue
		}
		best, when, found = n, e.OccurredAt, true
	}
	return best, found
}

// lastAt : When the newest level was reported.
func lastAt(levels []event.Event) time.Time {
	var newest time.Time
	for _, e := range levels {
		if e.OccurredAt.After(newest) {
			newest = e.OccurredAt
		}
	}
	return newest
}

// unmarshal : A payload into whatever was asked for.
func unmarshal(e event.Event, into any) error {
	return json.Unmarshal(e.Payload, into)
}
