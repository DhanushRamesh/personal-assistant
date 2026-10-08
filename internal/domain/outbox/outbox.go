// Package outbox keeps what the assistant owes somebody to say.
//
// Owner: *"it feels like everything is scattered ... there should be
// a framework on how these should be done, like what if an event is
// not announced due to any issue -- server off, phone off."*
//
// Six watchers decide to speak, and until now each delivered its own
// announcement and recorded that it had spoken **afterwards**. So a
// failure between deciding and speaking left nothing behind: the
// situation, the leave-by, the thing a device said to announce at
// once were simply gone, and nothing anywhere knew they had been
// owed. Only reminders behaved properly, because they alone had a
// lifecycle in a table.
//
// This is that lifecycle, taken out of reminders and made the one
// way anything gets said.
//
// The line it draws is between deciding and delivering. *Is this
// worth saying* stays with the watcher, where it belongs: a diary
// entry three weeks out and a battery at fifteen per cent have
// nothing in common and never will. *Did it get said, to whom, and
// what if it did not* is one job, and is here.
package outbox

import (
	"context"
	"errors"
	"time"

	"personal-assistant/internal/base/conversation"
)

// State : What one client has done about one announcement.
//
// Per client, not per announcement. The same warning can be read on
// a laptop and missed by a speaker in an empty room, and a single
// verdict for both was the wrong shape: it had to be decided
// centrally, from a sensor that describes one room and knows
// nothing about where the laptop is.
type State string

const (
	// Pending : Published to this client, not yet said by it.
	//
	// The default, and not something a client reports. A voice
	// client in an empty room says nothing and writes nothing, and
	// the announcement is still waiting for it when the watch comes
	// back into range.
	Pending State = "pending"

	// Said : This client delivered it.
	//
	// For a typed client that is the moment it arrives: a screen
	// does not need anybody to be in front of it, and presence
	// there is always true.
	Said State = "said"

	// Expired : Its moment passed before this client could say it.
	//
	// Kept rather than deleted. "Nothing was announced" and "six
	// things were owed and all of them went stale" look identical
	// from an empty table, and the second one is a fault.
	Expired State = "expired"

	// Dropped : This client could not say it and will not try
	// again, either because its kind is only true now or because it
	// has tried as often as it may.
	Dropped State = "dropped"
)

// Errors a caller can act on.
var (
	// ErrNotAnnouncement : Something that is not said unasked was
	// put in the outbox. A reply is not a thing to deliver later.
	ErrNotAnnouncement = errors.New("outbox: that kind is not an announcement")

	// ErrNoText : An announcement with nothing to say.
	ErrNoText = errors.New("outbox: there is nothing to say")
)

// Owing : One thing the assistant owes somebody.
type Owing struct {
	// ID : This record.
	ID string
	// UserID : Who it is for.
	UserID string
	// Kind : Which kind of announcement, which decides the policy.
	Kind conversation.Kind
	// Text : What to say. Settled before it is stored, so that a
	// restart does not have to reconstruct the sentence -- and a
	// model that wrote it is not asked twice.
	Text string

	// From : The client that caused it, where one did.
	//
	// Not who it is for: that is worked out from the kind's
	// audience. A conversation renamed on a laptop is the laptop's
	// business and nobody else's, and this is how the publisher
	// knows which laptop. Empty for anything a clock caused, which
	// belongs to no client.
	From string

	// Due : When it became worth saying.
	Due time.Time
	// Expires : When it stops being worth saying, from the kind's
	// policy. Past this it is a thing announced at the wrong time,
	// which is worse than silence.
	Expires time.Time

	// At : When it was published.
	At time.Time
}

// Delivery : One client's copy of one announcement.
//
// Written when the announcement is published rather than when it is
// delivered. A row that does not exist cannot be asked "why did
// nobody hear this", which is the whole reason any of this exists.
type Delivery struct {
	// ID : This row.
	ID string
	// Owing : The announcement it is a copy of.
	Owing Owing
	// ClientID : Whose copy it is.
	ClientID string
	// State : What this client has done about it.
	State State
	// Tries : How many times this client has tried.
	Tries int
	// Why : What the client said went wrong, in its own words.
	Why string
	// At : When it was last moved on.
	At time.Time
}

// Store : Where what is owed is kept.
//
// Durable by requirement rather than by preference: the case this
// exists for is the server having been off, and an in-memory queue
// is empty at exactly the moment it is needed.
type Store interface {
	// Publish : Writes an announcement down and gives every named
	// client its own copy, before anything is said.
	//
	// Both together, because an announcement nobody is subscribed
	// to is a row that can never be delivered or explained.
	Publish(ctx context.Context, o Owing, clients []string) error

	// Pending : What one client still owes somebody, oldest first.
	//
	// Stale ones are included. Whether a thing is past saying is
	// the caller's judgement and it needs to see them to make it --
	// and to record that they went stale rather than leaving them
	// pending for ever.
	Pending(ctx context.Context, clientID string, limit int) ([]Delivery, error)

	// Mark : Records what one client did about one announcement.
	Mark(ctx context.Context, clientID, owedID string, state State,
		tries int, why string, at time.Time) error
}

// Limit : How many are taken in one pass.
//
// A bound rather than a guess at the number. Nothing should ever
// owe this many at once, and if it does, the right behaviour is to
// work through them steadily rather than to say forty things in a
// row to somebody who just walked in.
const Limit = 50

// New : An announcement to be delivered, with its policy applied.
//
// Expiry is worked out here rather than at delivery, so that it is
// fixed at the moment the thing was true. Worked out later it would
// be measured from whenever the server happened to come back, which
// is the one case this is for.
func New(id, userID string, kind conversation.Kind, text string, at time.Time) (Owing, error) {
	return Keeping(id, userID, kind, text, at, 0)
}

// Keeping : The same, with how long this one is worth saying.
//
// Owner, 7 October 2026, shown that eighteen announcements in a row
// had expired before he was ever in the room to hear them: *"fix
// the five minutes."*
//
// One number was the wrong fix, and the eighteen say so. Six were
// battery warnings and three were the assistant naming a
// conversation, none of which is worth hearing hours later -- a
// warning that the battery was at four percent, read out at the
// door that evening, is worse than silence. Two were missed calls
// from somebody the phone could not name, and those were worth
// keeping.
//
// Nothing about the kind can tell those apart. They are all the
// same kind. What knows is whoever wrote the rule, so that is who
// is asked, and zero means the kind decides as it always did.
func Keeping(id, userID string, kind conversation.Kind, text string,
	at time.Time, worth time.Duration,
) (Owing, error) {
	rule, ok := kind.Rule()
	if !ok {
		return Owing{}, ErrNotAnnouncement
	}
	if text == "" {
		return Owing{}, ErrNoText
	}
	if worth <= 0 {
		worth = rule.Worth
	}
	return Owing{
		ID:      id,
		UserID:  userID,
		Kind:    kind,
		Text:    text,
		Due:     at,
		Expires: at.Add(worth),
		At:      at,
	}, nil
}

// Stale : Whether its moment has passed.
func (o Owing) Stale(at time.Time) bool { return !at.Before(o.Expires) }

// Spent : Whether this client has tried as often as the kind allows.
func (d Delivery) Spent() bool {
	rule, ok := d.Owing.Kind.Rule()
	return ok && d.Tries >= rule.Tries
}
