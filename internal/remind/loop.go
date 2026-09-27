package remind

import (
	"context"
	"log/slog"
	"time"
)

const (
	// DefaultEvery : How often the loop asks what is due.
	//
	// This is how late a timer can be. Five seconds was chosen when the
	// shortest was a minute; a thirty second timer five seconds late is
	// noticeably wrong, and the question is one indexed read.
	DefaultEvery = 2 * time.Second

	// DefaultGrace : How late a reminder may be and still be worth saying.
	//
	// The server having been off is the usual reason. A timer said three
	// hours late is wrong; a reminder to call somebody usually is not, and
	// an hour is about where one turns into the other.
	DefaultGrace = time.Hour
)

// Presence : Whether somebody is there to hear a reminder.
//
// Deliberately a single question with a single safe answer. Anything that
// is not a confident, current "they are elsewhere" must come back false,
// because the cost of the two mistakes is not equal: speaking to an empty
// room wastes a sentence, and holding a reminder back from somebody who
// was sitting there loses it for as long as they take to notice.
type Presence interface {
	// Away : Whether the person is known to be out of the room. False
	// when it cannot be told.
	Away(ctx context.Context, userID string) bool
}

// Loop : Says reminders when their time comes.
//
// The first work in this server that happens because of the clock rather
// than because somebody asked.
type Loop struct {
	// Store : Where reminders are kept. Required.
	Store Store
	// Speaker : Where they are said. Required.
	Speaker Speaker
	// Presence : Whether anybody is there to hear it. Nil says everything
	// aloud, which is what this did before there was any way to tell.
	Presence Presence
	// Location : The person's zone, for working out when a repeating one
	// next falls. Nil is UTC.
	Location *time.Location
	// Now : The clock, replaceable in tests. Nil uses the real one.
	Now func() time.Time
	// Every : How often to look. Zero selects DefaultEvery.
	Every time.Duration
	// Grace : How late is still worth saying. Zero selects DefaultGrace.
	Grace time.Duration
	// Logger : Where failures go. Required in use; nil is silent.
	Logger *slog.Logger
}

// Run : Says reminders until the context ends.
//
// One pass at a time and never concurrently, so a reminder cannot be said
// twice by two passes overlapping.
func (l *Loop) Run(ctx context.Context) {
	if l == nil || l.Store == nil || l.Speaker == nil {
		return
	}

	every := l.Every
	if every <= 0 {
		every = DefaultEvery
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	// Once before waiting, so anything that came due while the server was
	// off is dealt with at startup rather than five seconds later.
	l.Once(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Once(ctx)
		}
	}
}

// Pass : What one look at the clock did.
type Pass struct {
	// Said : Reminders delivered.
	Said int
	// Missed : One-shots too late to be worth saying.
	Missed int
	// Moved : Repeating ones whose turn was too late, put to their next.
	Moved int
	// Held : Kept back because nobody was in the room to hear them.
	Held int
	// Failed : Ones nothing would take, left pending to try again.
	Failed int
}

// Once : One look at the clock.
//
// Separate from Run so a test can take a single step without waiting.
func (l *Loop) Once(ctx context.Context) Pass {
	var pass Pass
	if l == nil || l.Store == nil || l.Speaker == nil {
		return pass
	}

	at := l.clock()
	due, err := l.Store.Due(ctx, at, DefaultDueLimit)
	if err != nil {
		l.log(ctx, "cannot read what is due", err)
		return pass
	}

	for i := range due {
		l.one(ctx, due[i], at, &pass)
	}
	return pass
}

// one : Deals with a single reminder whose time has come.
func (l *Loop) one(ctx context.Context, r Reminder, at time.Time, pass *Pass) {
	next, repeating := Next(r.DueAt, r.Repeats, at, l.where())

	// Too late to be worth saying. A repeating one is moved on rather than
	// ended: marking it missed would stop it for good.
	if at.Sub(r.DueAt) > l.grace() {
		switch {
		case repeating:
			if err := l.Store.Reschedule(ctx, r.ID, next); err != nil {
				l.log(ctx, "cannot move a missed repeat on", err)
				return
			}
			pass.Moved++
			l.note(ctx, "a repeating reminder was too late to say, and was moved on", r)
		default:
			if err := l.Store.Missed(ctx, r.ID, at); err != nil {
				l.log(ctx, "cannot record a missed reminder", err)
				return
			}
			pass.Missed++
			l.note(ctx, "a reminder was missed", r)
		}
		return
	}

	// Nobody in the room. Kept back rather than said to an empty one, and
	// delivered when they walk in. A repeating one is not held: its next
	// turn is along soon enough, and holding one would queue up a morning
	// alarm to go off the moment somebody walked past at lunchtime.
	if !repeating && l.Presence != nil && l.Presence.Away(ctx, r.UserID) {
		if err := l.Store.Hold(ctx, r.ID, at); err != nil {
			l.log(ctx, "cannot hold a reminder back", err)
			return
		}
		pass.Held++
		l.note(ctx, "held a reminder back, nobody was in the room", r)
		return
	}

	if err := l.Speaker.Say(ctx, r); err != nil {
		// Left pending on purpose. It is tried again next pass, and the
		// grace window is what stops that going on for ever.
		pass.Failed++
		l.log(ctx, "cannot say a reminder, leaving it to try again", err)
		return
	}

	if !repeating {
		next = time.Time{}
	}
	if err := l.Store.Fired(ctx, r.ID, at, next); err != nil {
		// Said and not recorded. Logged loudly: the next pass will say it
		// again, which is the one way this is heard twice.
		l.log(ctx, "said a reminder but could not record it", err)
		return
	}
	pass.Said++
	l.note(ctx, "reminder said", r)
}

// clock : Now, in UTC.
func (l *Loop) clock() time.Time {
	if l.Now == nil {
		return time.Now().UTC()
	}
	return l.Now().UTC()
}

// where : The person's zone, or UTC.
func (l *Loop) where() *time.Location {
	if l.Location == nil {
		return time.UTC
	}
	return l.Location
}

// grace : How late is still worth saying.
func (l *Loop) grace() time.Duration {
	if l.Grace <= 0 {
		return DefaultGrace
	}
	return l.Grace
}

// note : Records what happened to one reminder.
func (l *Loop) note(ctx context.Context, msg string, r Reminder) {
	if l.Logger != nil {
		l.Logger.InfoContext(ctx, msg,
			slog.String("reminder_id", r.ID),
			slog.String("title", r.Title),
			slog.String("scope", string(r.Scope)))
	}
}

// log : Records a failure.
func (l *Loop) log(ctx context.Context, msg string, err error) {
	if l.Logger != nil {
		l.Logger.WarnContext(ctx, msg, slog.Any("error", err))
	}
}
