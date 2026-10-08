package remind

import (
	"context"
	"github.com/DhanushRamesh/personal-assistant/internal/base/alarm"
	"github.com/DhanushRamesh/personal-assistant/internal/base/logging"
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

	// DefaultLead : How far ahead of its time a reminder is picked up.
	//
	// Saying one is not instant. Measured on this server, a
	// reminder due at 02:34:33 was spoken at 02:34:36.7: the look
	// at the clock, then the call to Home Assistant, then Piper,
	// then the satellite. Meanwhile the phone alarm rings exactly
	// on its minute, so the alarm led the voice every time and the
	// voice sounded like an afterthought.
	//
	// Starting the sentence slightly early puts the voice where it
	// belongs: at the moment, or just before the alarm backs it up.
	DefaultLead = 4 * time.Second

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
// Alarms : Where a phone alarm is asked for.
//
// Asking rather than setting: whether the phone is reachable is
// somebody else's business, and this runs on a two second tick
// that must not wait on a tailnet.
type Alarms interface {
	// Want : Asks for an alarm on this person's phone.
	Want(ctx context.Context, userID string, a alarm.Alarm) error
}

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
	// Lead : How far ahead of its time to pick one up, so that the
	// speaking of it lands on the moment rather than after it.
	// Zero selects DefaultLead.
	Lead time.Duration
	// Alarm : Where a phone alarm is asked for. Nil asks for none,
	// which is what this did before the phone was reachable.
	//
	// Only the repeats the phone cannot hold are re-armed here --
	// monthly, in practice. A one-off had its alarm set when it was
	// made, and a daily or weekly one is already repeating on the
	// phone, where it keeps ringing through this server being down.
	Alarm Alarms
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
			// A tick that panics costs this tick. Without it the
			// loop ends and nothing is ever said again, which is a
			// worse way to lose reminders than losing one.
			logging.Surviving(ctx, l.Logger, "a reminder pass", func() { l.Once(ctx) })
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
	// Asked for slightly ahead, because saying one takes a few
	// seconds and the phone alarm does not wait.
	due, err := l.Store.Due(ctx, at.Add(l.lead()), DefaultDueLimit)
	if err != nil {
		l.log(ctx, "cannot read what is due", err)
		return pass
	}

	for i := range due {
		l.one(ctx, due[i], at, &pass)
	}

	l.retire(ctx, at, &pass)
	l.arm(ctx, at)
	return pass
}

// arm : Asks the phone for the alarms of reminders that have come
// within a day.
//
// An alarm holds an hour and a minute and no date, so one asked for
// further ahead than that rings the next time the clock reads it --
// days early, and acted on. So it is not asked for when the reminder
// is made; it is asked for here, once the day it belongs to is the
// next one that clock face comes round.
//
// Asked once. The phone cannot tell a second alarm from the first, so
// a repeat leaves two on it and only one can be taken back, which is
// what [Store.Armed] is for.
//
// A repeating reminder is left alone: the phone holds those itself
// where it can, and where it cannot they are re-armed as they fire.
func (l *Loop) arm(ctx context.Context, at time.Time) {
	if l.Alarm == nil || l.Store == nil {
		return
	}

	soon, err := l.Store.Due(ctx, at.Add(alarm.Within), DefaultDueLimit)
	if err != nil {
		l.log(ctx, "cannot read what is coming into range for an alarm", err)
		return
	}

	var armed []string
	for i := range soon {
		r := &soon[i]
		if r.AlarmAskedAt != nil || !alarm.Reaches(r.DueAt, at) {
			continue
		}
		if len(AlarmDays(r.Repeats, r.DueAt)) > 0 {
			continue
		}
		a := alarm.Alarm{
			At:    alarm.OnTheMinute(r.DueAt.In(l.where())),
			Label: r.Title,
			Key:   r.ID,
		}
		if err := l.Alarm.Want(ctx, r.UserID, a); err != nil {
			l.log(ctx, "cannot ask for an alarm coming into range", err)
			continue
		}
		armed = append(armed, r.ID)
	}

	if err := l.Store.Armed(ctx, armed, at); err != nil {
		// Asked for and not written down, which means it will be
		// asked for again on the next pass and the phone will hold
		// two. Loud, because the second cannot be taken back.
		l.log(ctx, "an alarm was asked for but not recorded, so it may be asked for twice", err)
	}
}

// retire : Gives up on reminders held back for too long.
//
// A held one waits for somebody to walk in, and until this it waited for
// ever: Due returns only pending ones, so nothing looked at it again.
// Away all day and a ten o'clock reminder was still spoken at seven, as
// though it were news.
//
// Past the grace it becomes missed, which is the honest description and
// already has the right behaviour: reported once, in words, as a thing
// that did not happen rather than a thing being announced now.
func (l *Loop) retire(ctx context.Context, at time.Time, pass *Pass) {
	if l.Store == nil {
		return
	}

	held, err := l.Store.Stale(ctx, at.Add(-l.grace()), DefaultDueLimit)
	if err != nil {
		l.log(ctx, "cannot read what has been held too long", err)
		return
	}

	for i := range held {
		if err := l.Store.Missed(ctx, held[i].ID, at); err != nil {
			l.log(ctx, "cannot give up on a held reminder", err)
			continue
		}
		pass.Missed++
		l.note(ctx, "a reminder was held too long and is now a miss", held[i])
	}
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
			l.rearm(ctx, r, next)
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
	} else {
		l.rearm(ctx, r, next)
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

// rearm : Sets the phone alarm for a repeating reminder's next turn.
//
// The phone's alarm clock can hold a repeat, but not one set from
// outside: Tasker's Set Alarm takes an hour, a minute and a label
// and nothing else. So the standing alarm is kept standing from
// here, one occurrence at a time.
//
// What that costs, stated plainly: a server that is down across a
// firing does not set the next one. The alarm already on the phone
// still rings, so this degrades by one occurrence rather than
// going quiet for good.
//
// Never fatal. The reminder itself is the thing that must not be
// lost, and it has already been said by the time this runs.
func (l *Loop) rearm(ctx context.Context, r Reminder, next time.Time) {
	if l.Alarm == nil || next.IsZero() {
		return
	}
	// The phone holds this repeat itself and is already set for the
	// next one. Asking again would stack a second identical alarm
	// beside it every morning.
	if len(AlarmDays(r.Repeats, next)) > 0 {
		return
	}
	at := alarm.OnTheMinute(next.In(l.where()))
	if err := l.Alarm.Want(ctx, r.UserID, alarm.Alarm{At: at, Label: r.Title, Key: r.ID}); err != nil {
		l.log(ctx, "cannot ask for the next alarm for a repeating reminder", err)
	}
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

// lead : How far ahead of its time to pick one up.
func (l *Loop) lead() time.Duration {
	if l.Lead < 0 {
		return 0
	}
	if l.Lead == 0 {
		return DefaultLead
	}
	return l.Lead
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
