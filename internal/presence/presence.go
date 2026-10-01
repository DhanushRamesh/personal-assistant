// Package presence answers whether the person is with the assistant.
//
// Not whether they are at home, which nothing here can know. The question
// worth answering is whether they are near the thing that listens: an
// assistant that will not speak to an empty room needs to know the room
// is empty, and a person who has left the building and a person who is
// in the next street are the same to it.
//
// A shared network says they are together. It is good evidence and it
// is free: the phone already reports which network it joined and the
// machine running the satellite reports the same about itself.
//
// A different network says nothing at all. A phone on mobile data lying
// beside the laptop is on a different network and plainly present, and
// an office with a corporate network and a guest network puts two
// devices in one room on two networks. So this answers present or it
// answers unable to tell; it never answers away from a network alone.
//
// Away has to come from somewhere that can actually see distance, which
// means location and geofences. Until those arrive this cannot say
// anybody has left, and so nothing built on it can welcome anybody back.
// That is the honest state of it rather than a gap to be filled with a
// guess.
//
// Derived from transitions rather than from a reading. A device reports
// joining and leaving, so the newest of those is the current state and
// an old one is not a stale one -- somebody who has not moved for four
// hours produces no events at all, and treating silence as doubt would
// doubt every quiet afternoon.
//
// Three answers and not two. Being unable to see is not the same as
// having seen nobody, and only the second is an absence. Everything that
// reads this is expected to treat unknown as present: a fault that
// silences the house is worse than a gap in a gate.
package presence

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// Where : Whether the person is with the assistant.
type Where string

const (
	// Present : Their phone and the assistant are on the same network.
	Present Where = "present"
	// Away : They are elsewhere.
	//
	// Nothing produces this yet. A network cannot establish it -- see
	// the package comment -- and it waits on location.
	Away Where = "away"
	// Unknown : It cannot be told. Never an absence.
	Unknown Where = "unknown"
)

const (
	// Joined, Left : The kinds this reads. Any device may invent a kind;
	// these two are the ones that mean a network.
	Joined = "network.joined"
	Left   = "network.left"

	// Phone, Satellite : Which source is which.
	Phone     = "tasker"
	Satellite = "laptop"

	// Quiet : How long a device may say nothing before its last word
	// stops being worth trusting.
	//
	// Only meaningful for a device that reports on a timer as well as on
	// a change. A device that speaks only when something happens is
	// silent all afternoon by design, and this would call that doubt.
	Quiet = 90 * time.Minute
)

// Answer : Where the person is, and why it says so.
type Answer struct {
	// Where : Present, away, or unable to tell.
	Where Where
	// Why : A sentence for a person reading a screen or a log. Not for
	// the assistant to say aloud.
	Why string
	// Since : When the newest thing it is reading happened. Zero when
	// there was nothing to read.
	Since time.Time
	// Stale : Whether the phone has been quiet longer than Quiet.
	//
	// Reported rather than acted on. Whether silence is a fault depends
	// on whether the phone was asked to speak up periodically, which
	// this cannot know.
	Stale bool
	// On : Which network each side was last on, for showing a person why.
	On map[string]string
}

// From : Where the person is, given what their devices have reported.
//
// Events in any order; the newest of each kind per source wins.
func From(events []event.Event, now time.Time) Answer {
	type state struct {
		network string
		at      time.Time
	}
	latest := map[string]state{}

	for _, e := range events {
		if e.Kind != Joined && e.Kind != Left {
			continue
		}
		src := strings.TrimSpace(e.Source)
		if was, ok := latest[src]; ok && !e.OccurredAt.After(was.at) {
			continue
		}
		network := ""
		if e.Kind == Joined {
			network = value(e.Payload)
		}
		latest[src] = state{network: network, at: e.OccurredAt}
	}

	phone, sawPhone := latest[Phone]
	sat, sawSatellite := latest[Satellite]

	out := Answer{Where: Unknown, On: map[string]string{}}
	for src, s := range latest {
		out.On[src] = s.network
		if s.at.After(out.Since) {
			out.Since = s.at
		}
	}
	if sawPhone && !phone.at.IsZero() {
		out.Stale = now.Sub(phone.at) > Quiet
	}

	switch {
	case !sawPhone && !sawSatellite:
		out.Why = "neither the phone nor the assistant has said which network it is on"
	case !sawPhone:
		out.Why = "the phone has not said which network it is on"
	case !sawSatellite:
		out.Why = "the assistant has not said which network it is on"
	case phone.network == "":
		out.Why = "the phone is not on any network"
	case sat.network == "":
		out.Why = "the assistant is not on any network"
	case phone.network == sat.network:
		out.Where = Present
		out.Why = fmt.Sprintf("both are on %s", phone.network)
	default:
		// Not away. Mobile data beside the laptop looks exactly like
		// this, and so does a guest network in the same building.
		out.Why = fmt.Sprintf(
			"the phone is on %s and the assistant is on %s, which says nothing about where they are",
			phone.network, sat.network)
	}
	return out
}

// Arrived : Whether this answer is a return rather than a continuation.
//
// A return is being present now, having been away before. Not simply
// being present: a network that drops and comes back is present, was
// present a second ago, and is not somebody walking through the door.
// One afternoon's wifi flapped eight times over lunch, and each of those
// would have been a welcome home.
func Arrived(before, after Answer) bool {
	return before.Where == Away && after.Where == Present
}

// value : What a network event says the network is.
//
// Parsed rather than searched for. A network name may contain a quote or
// a backslash -- it is whatever somebody called their router -- and
// finding the field by looking for its punctuation would read the wrong
// thing from the one name that needed care.
func value(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var into struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil {
		return ""
	}
	return into.Value
}
