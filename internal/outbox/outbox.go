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

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// State : Where an announcement is in its life.
type State string

const (
	// Owed : Decided on, not yet said.
	//
	// Written before anything is spoken, which is the whole point.
	// A record written afterwards cannot describe a failure.
	Owed State = "owed"

	// Said : Spoken, and to whom the policy asked for.
	Said State = "said"

	// Held : Nobody was there and it is worth keeping. Said when
	// they come back, while it is still worth saying.
	Held State = "held"

	// Expired : Its moment passed before it could be said.
	//
	// Kept rather than deleted. "Nothing was announced" and "six
	// things were owed and all of them went stale" look identical
	// from an empty table, and the second one is a fault.
	Expired State = "expired"

	// Dropped : Not worth keeping once it could not be said.
	//
	// For the kinds that are only true at the moment they happen.
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

	// Due : When it became worth saying.
	Due time.Time
	// Expires : When it stops being worth saying, from the kind's
	// policy. Past this it is a thing announced at the wrong time,
	// which is worse than silence.
	Expires time.Time

	// State : Where it is in its life.
	State State
	// Tries : How many attempts have been made.
	Tries int
	// Why : What went wrong on the last attempt, for somebody
	// reading back why they never heard it.
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
	// Owe : Writes one down, before anything is said.
	Owe(ctx context.Context, o Owing) error
	// Outstanding : Everything still to be delivered, oldest first.
	Outstanding(ctx context.Context, at time.Time, limit int) ([]Owing, error)
	// Settle : Records how one ended, and why if it went wrong.
	Settle(ctx context.Context, id string, state State, tries int, why string, at time.Time) error
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
	rule, ok := kind.Rule()
	if !ok {
		return Owing{}, ErrNotAnnouncement
	}
	if text == "" {
		return Owing{}, ErrNoText
	}
	return Owing{
		ID:      id,
		UserID:  userID,
		Kind:    kind,
		Text:    text,
		Due:     at,
		Expires: at.Add(rule.Worth),
		State:   Owed,
		At:      at,
	}, nil
}

// Stale : Whether its moment has passed.
func (o Owing) Stale(at time.Time) bool { return !at.Before(o.Expires) }

// Spent : Whether it has been tried as often as its kind allows.
func (o Owing) Spent() bool {
	rule, ok := o.Kind.Rule()
	return ok && o.Tries >= rule.Tries
}
