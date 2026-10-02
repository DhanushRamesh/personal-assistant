// Package event holds what happened to the person, as their own devices
// saw it.
//
// Everything else the assistant knows, it knows because somebody told it.
// This is the one source that arrives unasked, and it is what lets the
// assistant say something first: that they are home later than usual, that
// they have not moved since lunch, that the same number has rung three
// times.
//
// Deliberately shapeless. A kind is a dotted name and a payload is
// whatever that kind carries, because the value of this depends on the
// person adding to it over months -- a parcel delivered, a medicine taken,
// a train missed -- and anything that makes adding one cost a schema
// change will stop them adding any.
//
// What is enforced instead is the envelope: who, what kind, when it
// happened, and a key that makes sending it twice harmless. Those are the
// four things nothing downstream can reconstruct if they are wrong.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// IDPrefix : Marks an identifier as belonging to an event.
	IDPrefix = "evt_"
	// idLen : The length of a prefixed event identifier.
	idLen = len(IDPrefix) + ulid.EncodedSize
)

const (
	// MaxSource : The longest a source name may be.
	MaxSource = 40
	// MaxDevice : The longest a device name may be.
	MaxDevice = 60
	// MaxKind : The longest a kind may be.
	MaxKind = 60
	// MaxDedupeKey : The longest a deduplication key may be.
	MaxDedupeKey = 80

	// MaxPayload : The largest an event's payload may be.
	//
	// Room for a notification with its text, and far short of anything
	// that should have been a file. A phone that has buffered for a day
	// sends hundreds of these at once, so the ceiling is what stops one
	// bad profile from posting a photograph four hundred times.
	MaxPayload = 16 << 10

	// MaxBatch : How many events one request may carry.
	//
	// The phone arrives with a backlog after every tunnel and every
	// flight, so a batch has to be worth making; it also has to be
	// bounded, because the person writing the phone side is the same
	// person who will one day flush ten thousand rows by accident.
	MaxBatch = 500
)

// Ahead : How far into the future an event may claim to have happened.
//
// Phone clocks drift and some are set by hand. A little slack keeps a
// correct event from being refused over a few seconds of skew; much more
// than this and a mis-set clock writes rows that sort after everything
// real and stay at the top of "what happened today" forever.
const Ahead = 5 * time.Minute

var (
	// ErrNoUser : An event belongs to somebody.
	ErrNoUser = errors.New("event: no user")
	// ErrNoSource : Nothing said where this came from.
	ErrNoSource = errors.New("event: no source")
	// ErrNoKind : Nothing said what happened.
	ErrNoKind = errors.New("event: no kind")
	// ErrBadKind : A kind that is not a dotted lowercase name.
	ErrBadKind = errors.New("event: kind must be lowercase words separated by dots")
	// ErrNoOccurredAt : Nothing said when it happened.
	ErrNoOccurredAt = errors.New("event: no time it happened")
	// ErrFromTheFuture : It claims to have happened later than now.
	ErrFromTheFuture = errors.New("event: happened in the future")
	// ErrNoDedupeKey : Nothing to recognise a resend by.
	ErrNoDedupeKey = errors.New("event: no dedupe key")
	// ErrPayloadNotObject : A payload that is not a JSON object.
	ErrPayloadNotObject = errors.New("event: payload must be a JSON object")
	// ErrTooLong : Something exceeded its ceiling.
	ErrTooLong = errors.New("event: too long")
)

// Event : One thing that happened.
type Event struct {
	// ID : This event's own identifier.
	ID string
	// UserID : Whose life this is.
	UserID string
	// Source : What kind of thing reported it -- tasker, watch, laptop.
	Source string
	// Device : Which particular one, when there is more than one of a
	// kind. Empty is allowed and means the only one.
	Device string
	// Kind : What happened, as a dotted name: location.arrived,
	// call.missed, battery.low.
	Kind string
	// OccurredAt : When it happened, by the reporting device's clock.
	OccurredAt time.Time
	// ReceivedAt : When this server was told, by its own clock.
	//
	// Both are kept because they disagree. A phone that spent the day
	// without a signal delivers the day at once, and with only one of
	// these that day happened at teatime.
	ReceivedAt time.Time
	// Payload : Whatever this kind of event carries, as a JSON object.
	Payload json.RawMessage
	// DedupeKey : What makes sending this twice harmless. Chosen by the
	// device and reused across its retries.
	DedupeKey string
}

// New : An event, validated, with its identifier and arrival time filled
// in.
//
// receivedAt is passed rather than read from the clock so that a batch
// arriving together is recorded as having arrived together, and so that
// tests are not racing a clock.
func New(userID, source, device, kind string, occurredAt, receivedAt time.Time,
	payload json.RawMessage, dedupeKey string) (*Event, error) {

	e := &Event{
		ID:         IDPrefix + ulid.Make().String(),
		UserID:     strings.TrimSpace(userID),
		Source:     strings.TrimSpace(source),
		Device:     strings.TrimSpace(device),
		Kind:       strings.ToLower(strings.TrimSpace(kind)),
		OccurredAt: occurredAt.UTC(),
		ReceivedAt: receivedAt.UTC(),
		Payload:    payload,
		DedupeKey:  strings.TrimSpace(dedupeKey),
	}
	if err := e.Valid(); err != nil {
		return nil, err
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("{}")
	}
	return e, nil
}

// Valid : Whether this event may be stored.
func (e *Event) Valid() error {
	switch {
	case e.UserID == "":
		return ErrNoUser
	case e.Source == "":
		return ErrNoSource
	case utf8.RuneCountInString(e.Source) > MaxSource:
		return fmt.Errorf("%w: source over %d", ErrTooLong, MaxSource)
	case utf8.RuneCountInString(e.Device) > MaxDevice:
		return fmt.Errorf("%w: device over %d", ErrTooLong, MaxDevice)
	case e.Kind == "":
		return ErrNoKind
	case utf8.RuneCountInString(e.Kind) > MaxKind:
		return fmt.Errorf("%w: kind over %d", ErrTooLong, MaxKind)
	case !dotted(e.Kind):
		return ErrBadKind
	case e.OccurredAt.IsZero():
		return ErrNoOccurredAt
	case e.OccurredAt.After(e.ReceivedAt.Add(Ahead)):
		return ErrFromTheFuture
	case e.DedupeKey == "":
		return ErrNoDedupeKey
	case utf8.RuneCountInString(e.DedupeKey) > MaxDedupeKey:
		return fmt.Errorf("%w: dedupe key over %d", ErrTooLong, MaxDedupeKey)
	case len(e.Payload) > MaxPayload:
		return fmt.Errorf("%w: payload over %d bytes", ErrTooLong, MaxPayload)
	}
	return object(e.Payload)
}

// dotted : Whether a kind is lowercase words separated by single dots.
//
// A shape, not a list. Nothing here knows which kinds exist, because the
// point is that the phone may invent one this evening -- but a kind is an
// identifier that will be grouped and counted for years, so "Location
// Arrived", "location-arrived" and "location.arrived" must not become
// three different things nobody notices.
func dotted(kind string) bool {
	if strings.HasPrefix(kind, ".") || strings.HasSuffix(kind, ".") {
		return false
	}
	for _, part := range strings.Split(kind, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
				return false
			}
		}
	}
	return true
}

// object : Whether a payload is a JSON object.
//
// An object and not an array or a bare number, so that a kind can grow a
// field later without what is already stored changing shape. Absent counts
// as an empty one: plenty of events are the whole story by their name.
func object(payload json.RawMessage) error {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return ErrPayloadNotObject
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil {
		return fmt.Errorf("%w: %s", ErrPayloadNotObject, err)
	}
	return nil
}

// ValidID : Whether s could be an event's identifier.
func ValidID(s string) bool {
	return len(s) == idLen && strings.HasPrefix(s, IDPrefix)
}

// Kind : One kind of event, and how much of it there is.
//
// What a listing needs before it can ask for anything: nothing knows which
// kinds exist, because any device may invent one, so the only way to offer
// a filter is to ask what has actually arrived.
type Kind struct {
	// Kind : The dotted name.
	Kind string
	// Count : How many have been stored.
	Count int64
	// First, Last : When the earliest and latest of them happened.
	First time.Time
	Last  time.Time
}

// Query : What to look for.
//
// One struct rather than a growing argument list, because the useful
// questions differ in which parts they fill in rather than in kind:
// "what did I do today", "when was I last on that network", "what
// happened on Tuesday" are all this with different fields set. A tool
// for each would be several tools that read one table.
type Query struct {
	// Since, Until : The window. Zero means unbounded at that end.
	Since time.Time
	Until time.Time
	// Kind : Exactly this kind. Ignored when empty.
	Kind string
	// Prefix : Any kind starting with this, which is how a family of
	// kinds is asked for -- "network." covers joining and leaving
	// without naming either, and without the caller having to know
	// which ones exist.
	Prefix string
	// Contains : Text somewhere in the payload, case-insensitive. What
	// answers a question about a particular place or person rather than
	// a particular kind.
	Contains string
	// Omit : Kinds to leave out, whatever else matches.
	//
	// For the kinds that are working material rather than history. A
	// phone reports its position every five minutes and the server
	// turns runs of those into one place.stayed; the readings
	// underneath are how that is worked out and are no sort of answer
	// to "where was I on Tuesday". Left in, they bury everything else
	// in any window long enough to be interesting.
	Omit []string
	// Limit : At most this many, newest first. Zero means no limit.
	Limit int
}
