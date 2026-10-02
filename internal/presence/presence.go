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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// Heard : How long the phone may say nothing before it stops being
// able to say where anybody is.
//
// The phone reports a position every five minutes whether anything
// happened or not, so silence is a fault rather than a quiet
// afternoon. Four missed readings: enough that one lost signal or one
// slow flush does not blind the house, short enough that a phone left
// at the office does not keep claiming somebody is at home.
//
// This is the heartbeat the geofences do not have. A crossing fires on
// change and nothing else, so an old crossing is not a stale one -- but
// a phone that has stopped reporting positions cannot be trusted about
// either.
const Heard = 20 * time.Minute

// Here : Whether the person is at the place the assistant is in.
//
// From the geofence the person drew themselves, which is the only
// thing in this system that can see distance. The owner, 2 October
// 2026, having had six greetings from a watch that never left the
// room: "can we eliminate watch as my presence and change it to my
// phone, as phone will be always with me... the home assistant should
// announce only when my phone is near to it."
//
// What this gives up is the room. A watch at the desk could tell one
// room from the next; a geofence cannot tell the desk from the garden.
// That was the trade: the room was worth a decibel of margin, and a
// decibel of margin is why it was wrong six times in a day.
func Here(events []event.Event, place string, now time.Time) Answer {
	place = strings.TrimSpace(place)
	out := Answer{Where: Unknown, On: map[string]string{}}
	if place == "" {
		out.Why = "nowhere is configured as where the assistant is"
		return out
	}

	var crossings []event.Event
	for _, e := range events {
		switch e.Kind {
		case event.Entered, event.Exited:
			crossings = append(crossings, e)
		case event.Fixed:
			if e.OccurredAt.After(out.Since) {
				out.Since = e.OccurredAt
			}
		}
	}

	// Liveness first. Everything below is a claim about where somebody
	// is, and a phone that has gone quiet cannot support one.
	if out.Since.IsZero() {
		out.Why = "the phone has not reported where it is"
		return out
	}
	if now.Sub(out.Since) > Heard {
		out.Stale = true
		out.Why = fmt.Sprintf("the phone last reported %s ago", ago(now.Sub(out.Since)))
		return out
	}

	// The same pairing the stays use, so the two never disagree about
	// when somebody was somewhere.
	var holding *event.Fence
	for _, f := range event.Fences(crossings, now) {
		if !strings.EqualFold(f.Name, place) {
			continue
		}
		copied := f
		if holding == nil || f.From.After(holding.From) {
			holding = &copied
		}
	}
	if holding == nil {
		out.Why = "nothing has been reported about " + place
		return out
	}

	// Said as how long ago rather than at what time. This is read in a
	// log and on a screen, and a clock time here would be the server's
	// own, which is not the one the person keeps.
	switch {
	case !holding.Holds(now):
		out.Where = Away
		out.Why = fmt.Sprintf("they left %s %s ago", place, ago(now.Sub(holding.To)))
	case now.Sub(holding.From) > event.Lingering:
		// Open for longer than anybody stays without the departure
		// being missed. Android drops them, and a missed one must not
		// keep somebody at home for days.
		out.Stale = true
		out.Why = fmt.Sprintf("they entered %s %s ago and no leaving was ever reported",
			place, ago(now.Sub(holding.From)))
	default:
		out.Where = Present
		out.Why = fmt.Sprintf("they have been at %s for %s", place, ago(now.Sub(holding.From)))
	}
	return out
}

// ago : A stretch of time, roughly, in the words somebody would use.
func ago(d time.Duration) string {
	switch minutes := int(d.Round(time.Minute).Minutes()); {
	case minutes < 1:
		return "a moment"
	case minutes < 60:
		return fmt.Sprintf("%d minutes", minutes)
	case minutes < 120:
		return "an hour"
	case minutes < 48*60:
		return fmt.Sprintf("%d hours", minutes/60)
	default:
		return fmt.Sprintf("%d days", minutes/(24*60))
	}
}

// Reader : Where the events come from.
type Reader interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// OfPhone : Answers whether somebody is away, from their phone.
//
// Shaped to the one question the reminder loop asks, and answering it
// the safe way round: only a plain, current, confident absence holds a
// reminder back. Every doubt -- a failed read, a quiet phone, a
// geofence nobody has crossed -- says they are here, because speaking
// to an empty room wastes a sentence and holding a reminder from
// somebody sitting there loses it until they think to ask.
type OfPhone struct {
	// Events : Where to read from. Required.
	Events Reader
	// Place : What the person calls the place the assistant is in.
	Place string
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where a failed read goes. Nil is silent.
	Logger *slog.Logger
}

// Looking : How far back it reads. Long enough to find the crossing
// that put somebody where they are, which may have been last night.
const Looking = 48 * time.Hour

// Away : Whether the person is known to be elsewhere.
func (o OfPhone) Away(ctx context.Context, userID string) bool {
	if o.Events == nil || strings.TrimSpace(o.Place) == "" {
		return false
	}
	now := time.Now()
	if o.Now != nil {
		now = o.Now()
	}

	found, err := o.Events.Recent(ctx, userID, event.Query{Since: now.Add(-Looking)})
	if err != nil {
		if o.Logger != nil {
			o.Logger.WarnContext(ctx, "cannot tell whether they are here", slog.Any("error", err))
		}
		return false
	}
	return Here(found, o.Place, now).Where == Away
}

// Describe : What it is watching, for the log at startup.
func (o OfPhone) Describe() string { return "their phone, against the " + o.Place + " geofence" }
