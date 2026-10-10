// Package errand carries what the server owes a device.
//
// Owner, 6 October 2026: *"isolate the process where something from
// server to phone is happening. It's a separate process and channel,
// they should be decouplable."*
//
// Asking for something and getting it there are different jobs, and
// they were one. A reminder tool reached the phone inside the
// model's turn, slept a second and a half between two calls the
// phone's clock will not take back to back, and kept its retries in
// a map that a restart emptied.
//
// Here the asking writes a row and returns. One sender gets the rows
// to the device, in order, retrying until they stop being worth
// doing. Nothing that asks has to know whether the phone is
// reachable, or wait to find out.
//
// Nothing here knows what an alarm is. A kind names what is being
// asked and the body carries that kind's own arguments, so the next
// thing the server wants a device to do is a row rather than another
// way out of the building.
package errand

import (
	"encoding/json"
	"errors"
	"time"
)

// Kind : What is being asked of a device.
type Kind string

const (
	// AlarmSet : Put an alarm on the phone's clock.
	AlarmSet Kind = "alarm.set"
	// AlarmClear : Take one off it.
	AlarmClear Kind = "alarm.clear"
	// CallPlace : Ring somebody from the person's own phone.
	//
	// Owner: *"we can set an alarm right -- likewise there should be
	// an option to call a person."*
	//
	// It carries a **name** and never a number, which is the whole of
	// the design. The phone holds the contacts and resolves the name
	// against them; the server never has the address book.
	//
	// That is the same way round as everything else here -- *the
	// owner names the place, the server stores the name* -- and it
	// was already true of calls: every name the server knows for a
	// person came from the phone reporting a call, and those are
	// exactly the people worth ringing. An imported address book of
	// five hundred entries would bury the eight he actually calls,
	// need keeping in sync, and put somebody else's telephone
	// numbers on a server that has no use for them.
	CallPlace Kind = "call.place"
)

// State : Where an errand is in its life.
type State string

const (
	// Waiting : Still owed.
	Waiting State = "waiting"
	// Done : The device has it.
	Done State = "done"
	// Expired : Its moment passed before it got there.
	Expired State = "expired"
	// Superseded : Later news about the same thing made it moot.
	Superseded State = "superseded"
	// GaveUp : Tried as often as it is worth trying.
	GaveUp State = "gave up"
)

// Errors a caller can act on.
var (
	// ErrNoKind : Nothing says what is being asked.
	ErrNoKind = errors.New("errand: there is nothing to ask for")
	// ErrNoUser : Nothing says whose device it is.
	ErrNoUser = errors.New("errand: there is nobody to ask for")
)

// Errand : One thing the server owes a device.
type Errand struct {
	// ID : This errand.
	ID string
	// UserID : Whose device it is for.
	UserID string
	// Kind : What is being asked.
	Kind Kind
	// About : What it concerns, so later news can cancel it.
	//
	// A reminder's identifier, in practice. An alarm still queued
	// for a time that has since moved must never arrive.
	About string
	// Body : What to do, as the kind's own arguments.
	Body json.RawMessage

	// After : Not before this moment.
	//
	// How order is kept without anybody sleeping. Taking an alarm
	// back and putting the next one on are two starts of the
	// phone's clock activity, and back to back the second is
	// dropped in silence.
	After time.Time
	// Expires : When it stops being worth doing.
	Expires time.Time

	// State : Waiting, done, expired, superseded or given up.
	State State
	// Tries : How often it has been attempted.
	Tries int
	// Why : What went wrong last time.
	Why string
}

// New : One errand, ready to be written down.
func New(id, userID string, kind Kind, about string, body json.RawMessage,
	after, expires time.Time) (Errand, error) {
	if kind == "" {
		return Errand{}, ErrNoKind
	}
	if userID == "" {
		return Errand{}, ErrNoUser
	}
	return Errand{
		ID: id, UserID: userID, Kind: kind, About: about, Body: body,
		After: after.UTC(), Expires: expires.UTC(), State: Waiting,
	}, nil
}

// Over : Whether this is past being worth doing.
func (e Errand) Over(now time.Time) bool {
	return !e.Expires.IsZero() && !now.Before(e.Expires)
}
