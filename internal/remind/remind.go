// Package remind holds things to be said at a time rather than because
// somebody asked.
//
// A reminder only ever says something. It carries words and speaks them: a
// timer that has finished, something to be told at seven. It does not run
// instructions unattended, because a task firing with nobody watching has
// nobody to catch it.
package remind

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

const (
	// IDPrefix : Marks an identifier as belonging to a reminder.
	IDPrefix = "rem_"
	// idLen : The length of a prefixed reminder identifier.
	idLen = len(IDPrefix) + ulid.EncodedSize
)

const (
	// MaxTitle : The longest a reminder's name may be.
	MaxTitle = 160
	// MaxBody : The longest a reminder may be.
	//
	// It is going to be read aloud, so anything longer is a document being
	// recited at somebody who wanted a sentence.
	MaxBody = 500
)

// Scope : Where a reminder lands when it fires.
type Scope string

const (
	// ScopeClient : At the client that set it. What a timer wants: set on
	// the satellite, rings on the satellite.
	ScopeClient Scope = "client"
	// ScopeUser : Wherever the person is told things. What a reminder
	// wants, since they may not be where they were.
	ScopeUser Scope = "user"
)

// Valid : Whether the scope is one the code knows.
func (s Scope) Valid() bool { return s == ScopeClient || s == ScopeUser }

// Repeat : How often a reminder comes back.
//
// Words rather than a cron expression. Cron is a language, and nobody says
// it aloud.
type Repeat string

const (
	// Once : Not at all. The default.
	Once Repeat = ""
	// Daily : Every day at the same time.
	Daily Repeat = "daily"
	// Weekdays : Monday to Friday.
	Weekdays Repeat = "weekdays"
	// Weekly : The same day each week.
	Weekly Repeat = "weekly"
	// Monthly : The same date each month.
	Monthly Repeat = "monthly"
)

// Valid : Whether the repeat is one the code knows.
func (r Repeat) Valid() bool {
	switch r {
	case Once, Daily, Weekdays, Weekly, Monthly:
		return true
	}
	return false
}

// Repeats : Every repeat a caller may offer, without the empty one.
func Repeats() []Repeat { return []Repeat{Daily, Weekdays, Weekly, Monthly} }

// Status : Where a reminder is in its life.
type Status string

const (
	// Pending : Waiting for its time.
	Pending Status = "pending"
	// Done : It fired and will not come back.
	Done Status = "done"
	// Missed : Its time passed while nothing was listening, too long ago to
	// say now. Kept rather than deleted, so it can be mentioned once.
	Missed Status = "missed"
	// Held : Its time came while the person was out of the room, so it was
	// kept back rather than said to nobody.
	//
	// Not the same as missed. Missed means nothing could say it; held means
	// something could and chose to wait. The person gets a held one when
	// they walk back in, and is told a missed one happened.
	Held Status = "held"
	// Cancelled : Called off before it fired.
	Cancelled Status = "cancelled"
)

// Valid : Whether the status is one the code knows.
func (s Status) Valid() bool {
	switch s {
	case Pending, Done, Missed, Held, Cancelled:
		return true
	}
	return false
}

// Reminder : Something to be said at a time.
type Reminder struct {
	// ID : The identifier, an IDPrefix followed by a ULID.
	ID string
	// UserID : Whose reminder it is.
	UserID string
	// ClientID : Which client it belongs to. Set for ScopeClient, and empty
	// otherwise.
	ClientID string
	// Scope : Where it lands when it fires.
	Scope Scope
	// Title : What to call it in a listing, and when asking which one.
	Title string
	// Body : What is actually said.
	Body string
	// SaidLate : The same thing in the past tense, for when it is heard
	// after the moment has gone. Empty falls back to prefixing Body.
	//
	// Written by whoever set the reminder, because it is grammar and the
	// server cannot conjugate. The hour in it is not: that is put in at
	// the time, from the clock.
	SaidLate string
	// DueAt : When it is next to be said, in UTC.
	DueAt time.Time
	// Repeats : How often it comes back. Once for a one-shot.
	Repeats Repeat
	// Status : Where it is in its life.
	Status Status
	// CreatedAt : When it was made.
	CreatedAt time.Time
	// UpdatedAt : When it last changed.
	UpdatedAt time.Time
	// LastFiredAt : When it was last said, or nil if never.
	LastFiredAt *time.Time
	// MentionedAt : When a miss was brought up, or nil if it has not been.
	// Only ever set on a missed one, and only once.
	MentionedAt *time.Time
	// Fires : How many times it has been said.
	Fires int
}

// Errors a reminder can be refused for.
var (
	// ErrNoUser : A reminder belonging to nobody.
	ErrNoUser = errors.New("remind: a reminder needs an owner")
	// ErrNoClient : A client-scoped reminder with no client to fire at.
	ErrNoClient = errors.New("remind: a reminder for one client must say which")
	// ErrNoTitle : A reminder with nothing to call it.
	ErrNoTitle = errors.New("remind: a reminder needs a name")
	// ErrNoBody : A reminder with nothing to say.
	ErrNoBody = errors.New("remind: a reminder needs something to say")
	// ErrTitleTooLong : A name that is a sentence.
	ErrTitleTooLong = errors.New("remind: the name is too long")
	// ErrBodyTooLong : More than anybody wants read aloud.
	ErrBodyTooLong = errors.New("remind: what it says is too long to be read out")
	// ErrNoTime : A reminder due at no particular moment.
	ErrNoTime = errors.New("remind: a reminder needs a time")
	// ErrBadScope : A scope the code does not know.
	ErrBadScope = errors.New("remind: unknown scope")
	// ErrBadRepeat : A repeat the code does not know.
	ErrBadRepeat = errors.New("remind: unknown repeat")
	// ErrBadStatus : A status the code does not know.
	ErrBadStatus = errors.New("remind: unknown status")
	// ErrNotFound : No such reminder.
	ErrNotFound = errors.New("remind: no such reminder")
	// ErrNotSnoozable : A reminder in a state that cannot be put off.
	ErrNotSnoozable = errors.New("remind: only one that is waiting, or has just been said, can be put off")
	// ErrSnoozeRepeats : A repeating reminder cannot be moved. See Snoozable.
	ErrSnoozeRepeats = errors.New("remind: a repeating reminder is not moved; make a one-off instead")
)

// Snoozable : Whether a reminder may be put off to a later time.
//
// Both stores ask this, so that what may be snoozed cannot come to mean
// one thing in memory and another in MySQL.
//
// A repeating one is refused outright. Moving its due time would move the
// series with it: a daily seven o'clock put off by ten minutes is ten past
// seven tomorrow, and twenty past the day after. Putting off one morning's
// is a one-off of its own, which is the caller's to make.
//
// A missed one is refused because it was never said, and a cancelled one
// because it was called off on purpose. Neither is something to be put off;
// they are something to be set again.
func Snoozable(r *Reminder) error {
	switch {
	case r == nil:
		return ErrNotFound
	case r.Repeats != Once:
		return ErrSnoozeRepeats
	case r.Status != Pending && r.Status != Done && r.Status != Held:
		return ErrNotSnoozable
	}
	return nil
}

// Valid : Whether the reminder can be stored.
func (r *Reminder) Valid() error {
	switch {
	case strings.TrimSpace(r.UserID) == "":
		return ErrNoUser
	case !r.Scope.Valid():
		return ErrBadScope
	case r.Scope == ScopeClient && strings.TrimSpace(r.ClientID) == "":
		return ErrNoClient
	case strings.TrimSpace(r.Title) == "":
		return ErrNoTitle
	case strings.TrimSpace(r.Body) == "":
		return ErrNoBody
	case utf8.RuneCountInString(r.Title) > MaxTitle:
		return ErrTitleTooLong
	case utf8.RuneCountInString(r.Body) > MaxBody:
		return ErrBodyTooLong
	case utf8.RuneCountInString(r.SaidLate) > MaxBody:
		return ErrBodyTooLong
	case r.DueAt.IsZero():
		return ErrNoTime
	case !r.Repeats.Valid():
		return ErrBadRepeat
	case !r.Status.Valid():
		return ErrBadStatus
	}
	return nil
}

// NewID : A fresh reminder identifier.
func NewID() string { return IDPrefix + ulid.Make().String() }

// ValidID : Whether id is shaped like a reminder identifier. It checks the
// form only; no such reminder need exist.
func ValidID(id string) bool {
	if len(id) != idLen || !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	_, err := ulid.ParseStrict(strings.TrimPrefix(id, IDPrefix))
	return err == nil
}

// New : A reminder ready to be stored, or why it cannot be.
func New(userID, clientID string, scope Scope, title, body string, dueAt time.Time, repeats Repeat) (*Reminder, error) {
	return NewLate(userID, clientID, scope, title, body, "", dueAt, repeats)
}

// NewLate : A reminder that also knows how to say itself in the past.
func NewLate(userID, clientID string, scope Scope, title, body, late string,
	dueAt time.Time, repeats Repeat) (*Reminder, error) {
	at := now()
	if scope != ScopeClient {
		clientID = ""
	}

	r := &Reminder{
		ID:        NewID(),
		UserID:    strings.TrimSpace(userID),
		ClientID:  strings.TrimSpace(clientID),
		Scope:     scope,
		Title:     strings.TrimSpace(title),
		Body:      strings.TrimSpace(body),
		SaidLate:  strings.TrimSpace(late),
		DueAt:     dueAt.UTC(),
		Repeats:   repeats,
		Status:    Pending,
		CreatedAt: at,
		UpdatedAt: at,
	}
	if err := r.Valid(); err != nil {
		return nil, err
	}
	return r, nil
}

// now : The clock, replaceable in tests.
var now = func() time.Time { return time.Now().UTC() }
