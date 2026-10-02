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

// Within : How close the phone has to be to the machine running the
// assistant to count as being with it, in metres.
//
// The same distance that makes two readings one place, so nothing in
// this server disagrees with anything else about what "here" means.
// Generous for a room and tight enough that the next street is not
// it; the phone's own scatter is about twenty metres, so a smaller
// radius would flicker.
const Within = 150.0

// Near : Whether the person's phone is with the machine that listens.
//
// The owner, 2 October 2026: "the distance between the phone and the
// home assistant server laptop is the factor." Not a geofence, not a
// named place, not a shared network -- those answer where somebody is
// rather than whether they are here, and a laptop carried to the
// office is still a laptop being spoken to.
//
// Two positions and the distance between them. The phone reports its
// own; the satellite's is remembered from the last time somebody spoke
// to it out loud, which is the one moment the two are certainly
// together.
func Near(events []event.Event, now time.Time) Answer {
	out := Answer{Where: Unknown, On: map[string]string{}}

	var phone, satellite *event.Fix
	for i := range events {
		e := events[i]
		switch e.Kind {
		case event.Fixed:
			if f, ok := event.ReadFix(e); ok && (phone == nil || f.At.After(phone.At)) {
				copied := f
				phone = &copied
			}
		case event.Standing:
			lat, lon, ok := event.LatLon(value(e.Payload))
			if !ok {
				continue
			}
			if satellite == nil || e.OccurredAt.After(satellite.At) {
				satellite = &event.Fix{At: e.OccurredAt, Lat: lat, Lon: lon}
			}
		}
	}

	switch {
	case phone == nil:
		out.Why = "the phone has not reported where it is"
		return out
	case satellite == nil:
		out.Since = phone.At
		out.Why = "the assistant does not know where it is; nobody has spoken to it out loud yet"
		return out
	}

	out.Since = phone.At
	// A phone that has stopped reporting cannot say where anybody is,
	// however recently it last did. The five-minute readings are the
	// only heartbeat there is.
	if now.Sub(phone.At) > Heard {
		out.Stale = true
		out.Why = fmt.Sprintf("the phone last reported %s ago", ago(now.Sub(phone.At)))
		return out
	}

	apart := event.Stay{Lat: satellite.Lat, Lon: satellite.Lon}.Apart(phone.Lat, phone.Lon)
	if apart <= Within {
		out.Where = Present
		out.Why = fmt.Sprintf("their phone is %d metres from the assistant", int(apart))
		return out
	}
	out.Where = Away
	out.Why = fmt.Sprintf("their phone is %s from the assistant", far(apart))
	return out
}

// Arriving : Whether this is the moment they came back.
//
// Worked out from the readings rather than remembered, so a restart
// does not greet somebody who never went anywhere and a missed batch
// does not swallow a homecoming. Present now, and away at the reading
// before this one.
func Arriving(events []event.Event, now time.Time) bool {
	if Near(events, now).Where != Present {
		return false
	}

	// Everything except the newest reading, judged at the moment that
	// reading replaced.
	var newest, previous time.Time
	for _, e := range events {
		if e.Kind != event.Fixed {
			continue
		}
		if e.OccurredAt.After(newest) {
			newest, previous = e.OccurredAt, newest
		} else if e.OccurredAt.After(previous) {
			previous = e.OccurredAt
		}
	}
	if previous.IsZero() {
		// One reading ever. Nothing to have arrived from.
		return false
	}

	before := make([]event.Event, 0, len(events))
	for _, e := range events {
		if e.Kind == event.Fixed && e.OccurredAt.Equal(newest) {
			continue
		}
		before = append(before, e)
	}
	return Near(before, previous).Where == Away
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

// far : A distance, roughly, in the words somebody would use.
func far(metres float64) string {
	if metres < 1000 {
		return fmt.Sprintf("%d metres", int(metres/10)*10)
	}
	return fmt.Sprintf("%.1f km", metres/1000)
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
	if o.Events == nil {
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
	return Near(found, now).Where == Away
}

// Describe : What it is watching, for the log at startup.
func (o OfPhone) Describe() string {
	return fmt.Sprintf("how far their phone is from this machine, over %d metres being away", int(Within))
}

// Moved : How far the assistant has to have moved before it is worth
// writing down again.
//
// The same radius that counts as being with it, so a position good
// enough to answer presence is not rewritten for a few metres of
// scatter.
const Moved = Within

// Stale : How old the assistant's own position may be before it is
// refreshed, even where nothing has moved.
//
// A laptop that was carried somewhere while the phone was elsewhere
// has a position nobody corrected. A day is long enough that an
// ordinary week writes seven of these, and short enough that a
// forgotten move is wrong for one day rather than for ever.
const Stale = 24 * time.Hour

// Writer : Where the assistant's own position is written.
type Writer interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
	Record(ctx context.Context, userID string, events []*event.Event) (stored, seen []string, err error)
}

// Locate : Notes where the machine running the assistant is, from the
// phone, because somebody has just spoken to it out loud.
//
// This is the whole of how a laptop learns its own position. Nothing
// tells it; it is told by the one thing that is certainly true when a
// voice arrives, which is that a person is standing in front of it,
// and their phone is where they are.
//
// Only voice. A typed message can come from the office, and a
// position learned from one would move the assistant to wherever
// somebody happened to be sitting.
//
// Quiet about everything. Nobody is waiting for this and nothing
// depends on any one of them landing: the next voice turn writes
// another.
func Locate(ctx context.Context, store Writer, userID string, now time.Time, logger *slog.Logger) {
	if store == nil || userID == "" {
		return
	}

	found, err := store.Recent(ctx, userID, event.Query{Since: now.Add(-Looking)})
	if err != nil {
		return
	}

	var phone *event.Fix
	var known *event.Fix
	for i := range found {
		e := found[i]
		switch e.Kind {
		case event.Fixed:
			if f, ok := event.ReadFix(e); ok && (phone == nil || f.At.After(phone.At)) {
				copied := f
				phone = &copied
			}
		case event.Standing:
			lat, lon, ok := event.LatLon(value(e.Payload))
			if ok && (known == nil || e.OccurredAt.After(known.At)) {
				known = &event.Fix{At: e.OccurredAt, Lat: lat, Lon: lon}
			}
		}
	}

	// A reading nobody has taken recently says nothing about where
	// anybody is standing.
	if phone == nil || now.Sub(phone.At) > Heard {
		return
	}
	if known != nil && now.Sub(known.At) < Stale {
		// A composite literal cannot sit bare in a condition.
		at := event.Stay{Lat: known.Lat, Lon: known.Lon}
		if at.Apart(phone.Lat, phone.Lon) < Moved {
			return
		}
	}

	where := event.Coordinates(phone.Lat, phone.Lon)
	payload, err := json.Marshal(map[string]string{"value": where})
	if err != nil {
		return
	}
	// Keyed by the hour, so a morning of voice turns writes one row
	// rather than thirty.
	key := event.Standing + ":" + now.UTC().Format("2006-01-02T15")
	e, err := event.New(userID, "server", "", event.Standing, now, now, payload, key)
	if err != nil {
		return
	}
	if _, _, err := store.Record(ctx, userID, []*event.Event{e}); err != nil {
		return
	}
	if logger != nil {
		logger.InfoContext(ctx, "the assistant learned where it is",
			slog.String("from", "a voice turn"), slog.String("at", where))
	}
}
