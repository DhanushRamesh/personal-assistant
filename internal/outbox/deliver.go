package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Every : How often what is owed is looked at.
//
// Close to the reminder loop rather than to the slower watchers.
// Whatever decided to speak has already decided it is time, so the
// only delay this adds is its own.
const Every = 2 * time.Second

// Speaker : Somewhere an announcement can actually be said.
//
// Narrower than the announcement writer on purpose: this decides
// whether and when, and nothing about how it reaches a room or a
// phone, which the audience already settles.
type Speaker interface {
	Say(ctx context.Context, userID string, kind conversation.Kind, text string) error
}

// Presence : Whether anybody is there to hear it.
//
// One answer, not two. Presence in this house fails closed by the
// owner's decision -- a sensor that cannot say counts as away, and
// there is a switch to override it by hand -- so the judgement is
// already made before it reaches here. The same interface the
// reminder loop uses, rather than a second opinion about the same
// room.
type Presence interface {
	Away(ctx context.Context, userID string) bool
}

// Deliverer : Says what is owed, and settles what cannot be said.
//
// The only thing that speaks. A watcher writes down that something
// is owed and is finished with it; everything after that -- the
// room, the retry, the expiry -- happens here once rather than six
// times with six different answers.
type Deliverer struct {
	// Store : What is owed. Required.
	Store Store
	// Speaker : Where it is said. Required.
	Speaker Speaker
	// Presence : Whether anybody is there. Nil says everything,
	// which is how this behaved before a sensor existed.
	Presence Presence
	// Every : How often to look. Zero selects Every.
	Every time.Duration
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Pass : What one look at the outbox did.
type Pass struct {
	Said    int
	Held    int
	Expired int
	Dropped int
	Failed  int
}

// Run : Delivers what is owed until the context ends.
func (d *Deliverer) Run(ctx context.Context) {
	if d == nil || d.Store == nil || d.Speaker == nil {
		return
	}
	every := d.Every
	if every <= 0 {
		every = Every
	}
	tick := time.NewTicker(every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.Once(ctx)
		}
	}
}

// Once : One look at what is owed.
//
// Separate from Run so a test can take a single step, and so that
// a server starting up can drain what it owes before it begins
// ticking.
func (d *Deliverer) Once(ctx context.Context) Pass {
	var pass Pass
	if d == nil || d.Store == nil || d.Speaker == nil {
		return pass
	}

	now := d.clock()
	owing, err := d.Store.Outstanding(ctx, now, Limit)
	if err != nil {
		d.log("cannot read what is owed", err)
		return pass
	}
	for _, o := range owing {
		d.one(ctx, o, now, &pass)
	}
	return pass
}

// one : Deals with a single thing owed.
func (d *Deliverer) one(ctx context.Context, o Owing, now time.Time, pass *Pass) {
	rule, ok := o.Kind.Rule()
	if !ok {
		// Not an announcement at all. Settled rather than left, or
		// it would be read and skipped on every pass for ever.
		d.settle(ctx, o, Dropped, "not an announcement", now)
		pass.Dropped++
		return
	}

	// Its moment has passed. This is the case the whole thing is
	// for: the server was off, it comes back owing something, and
	// the answer is not to announce breakfast at lunchtime.
	if o.Stale(now) {
		d.settle(ctx, o, Expired, "its moment passed before it could be said", now)
		pass.Expired++
		d.note("an announcement went stale before it could be said", o)
		return
	}

	// Nobody there. What that means is the kind's business, not
	// this one's.
	if d.away(ctx, o.UserID) {
		switch rule.Away {
		case conversation.Hold:
			if o.State != Held {
				d.settle(ctx, o, Held, "nobody was in the room", now)
			}
			pass.Held++
			return
		case conversation.Drop:
			d.settle(ctx, o, Dropped, "nobody was in the room", now)
			pass.Dropped++
			return
		}
		// Anyway: it does not need the room.
	}

	if err := d.Speaker.Say(ctx, o.UserID, o.Kind, o.Text); err != nil {
		o.Tries++
		pass.Failed++
		if o.Tries >= rule.Tries {
			d.settle(ctx, o, Dropped, err.Error(), now)
			d.note("gave up saying an announcement", o)
			return
		}
		// Left owed on purpose: the next pass tries again, and the
		// expiry is what stops that going on for ever.
		d.settle(ctx, o, Owed, err.Error(), now)
		return
	}
	d.settle(ctx, o, Said, "", now)
	pass.Said++
}

// away : Whether nobody is there to hear it.
//
// Nil says everything, which is how this behaved before there was
// a sensor at all.
func (d *Deliverer) away(ctx context.Context, userID string) bool {
	return d.Presence != nil && d.Presence.Away(ctx, userID)
}

// settle : Records how one ended, or that it is still owed after a
// try that did not work.
func (d *Deliverer) settle(ctx context.Context, o Owing, state State, why string, at time.Time) {
	if err := d.Store.Settle(ctx, o.ID, state, o.Tries, why, at); err != nil {
		d.log("cannot record how an announcement ended", err)
	}
}

// clock : Now, in UTC.
func (d *Deliverer) clock() time.Time {
	if d.Now == nil {
		return time.Now().UTC()
	}
	return d.Now().UTC()
}

// log : A failure of this machinery, not of an announcement.
func (d *Deliverer) log(msg string, err error) {
	if d.Logger != nil {
		d.Logger.Error(msg, slog.Any("error", err))
	}
}

// note : Something that was owed and will not now be said.
//
// Warned rather than logged quietly. Somebody asking "why did I
// not hear about that" deserves a line that answers it.
func (d *Deliverer) note(msg string, o Owing) {
	if d.Logger != nil {
		d.Logger.Warn(msg,
			slog.String("kind", string(o.Kind)),
			slog.String("text", o.Text),
			slog.Int("tries", o.Tries))
	}
}
