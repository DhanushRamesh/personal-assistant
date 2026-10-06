package errand

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Defaults for the sender.
const (
	// Tick : How often it looks for what is owed.
	//
	// Slower than the job runner, because nothing here is about a
	// moment: an alarm for seven o'clock is just as good set at one
	// minute past six as at six exactly. A nudge on being asked
	// covers the ordinary case, and this covers the retries.
	Tick = 30 * time.Second
	// Tries : How many attempts before giving up on one.
	Tries = 20
	// Limit : How many are taken in one look.
	Limit = 50
	// Backlog : How many nudges wait to be noticed.
	Backlog = 16
)

// Store : Where errands are kept.
type Store interface {
	// Add : Writes one down.
	Add(ctx context.Context, e Errand) error
	// Due : What is owed and ready, oldest first.
	Due(ctx context.Context, at time.Time, limit int) ([]Errand, error)
	// Did : Records what came of one attempt.
	Did(ctx context.Context, id string, state State, tries int, why string, at time.Time) error
	// Drop : Marks what is still waiting about a thing as
	// superseded, and says how many.
	Drop(ctx context.Context, userID, about string, at time.Time) (int, error)
}

// Hands : What actually reaches a device.
//
// One per kind, so that what the server can ask a device to do
// grows by wiring a function rather than by this package learning
// what an alarm is.
type Hands map[Kind]func(ctx context.Context, e Errand) error

// Sender : Gets what is owed to a device there.
type Sender struct {
	// Store : Where errands are kept. Required.
	Store Store
	// Hands : What carries each kind out. Required.
	Hands Hands
	// Tick : How often to look. Zero selects [Tick].
	Tick time.Duration
	// Tries : How many attempts before giving up. Zero selects
	// [Tries].
	Tries int
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
	// Told : How the person hears that something was never
	// delivered. Optional.
	//
	// The one thing that must not be swallowed. Somebody who
	// believes an alarm is set and finds it never was is worse off
	// than somebody told it failed, and with the asking and the
	// getting separated this is the only place the truth is known.
	Told func(ctx context.Context, userID, said string)

	once   sync.Once
	nudged chan struct{}
}

// Ask : Writes an errand down and nudges the sender.
//
// Never waits on the device. Whatever is asking is in the middle of
// answering somebody.
func (s *Sender) Ask(ctx context.Context, e Errand) error {
	if s == nil || s.Store == nil {
		return nil
	}
	if err := s.Store.Add(ctx, e); err != nil {
		return err
	}
	s.Wake()
	return nil
}

// Drop : Gives up on what is still waiting about one thing.
func (s *Sender) Drop(ctx context.Context, userID, about string) (int, error) {
	if s == nil || s.Store == nil {
		return 0, nil
	}
	return s.Store.Drop(ctx, userID, about, s.clock())
}

// Wake : Says there may be something to do, without blocking.
func (s *Sender) Wake() {
	if s == nil {
		return
	}
	select {
	case s.inbox() <- struct{}{}:
	default:
	}
}

// inbox : The nudge channel, made once.
func (s *Sender) inbox() chan struct{} {
	s.once.Do(func() { s.nudged = make(chan struct{}, Backlog) })
	return s.nudged
}

// Run : Carries out what is owed until the context ends.
func (s *Sender) Run(ctx context.Context) {
	if s == nil || s.Store == nil || s.Hands == nil {
		return
	}
	every := s.Tick
	if every <= 0 {
		every = Tick
	}
	tick := time.NewTicker(every)
	defer tick.Stop()

	in := s.inbox()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Once(ctx)
		case <-in:
			s.Once(ctx)
		}
	}
}

// Pass : What one look did.
type Pass struct {
	Sent    int
	Failed  int
	Expired int
}

// Once : One look at what is owed.
func (s *Sender) Once(ctx context.Context) Pass {
	var pass Pass
	if s == nil || s.Store == nil || s.Hands == nil {
		return pass
	}

	now := s.clock()
	owed, err := s.Store.Due(ctx, now, Limit)
	if err != nil {
		s.log("cannot read what is owed to the device", err)
		return pass
	}
	for _, e := range owed {
		s.one(ctx, e, now, &pass)
	}
	return pass
}

// one : One errand.
//
// Carried out in the order they come back, and one at a time. The
// phone's clock drops a second request that arrives on the heels of
// the first, so there is nothing to gain from doing these at once
// and a known fault in trying.
func (s *Sender) one(ctx context.Context, e Errand, now time.Time, pass *Pass) {
	if e.Over(now) {
		pass.Expired++
		s.settle(ctx, e, Expired, "its moment passed before the device had it", now)
		return
	}

	hand, ok := s.Hands[e.Kind]
	if !ok {
		// A kind nothing carries out would be read and skipped for
		// ever. Ended, loudly.
		s.note("nothing knows how to carry out this errand", e, string(e.Kind))
		s.settle(ctx, e, GaveUp, "nothing here knows how to do that", now)
		return
	}

	if err := hand(ctx, e); err != nil {
		pass.Failed++
		tries := e.Tries + 1
		if tries >= s.tries() {
			s.note("giving up on an errand to the device", e, err.Error())
			s.settle(ctx, e, GaveUp, err.Error(), now)
			s.tell(ctx, e)
			return
		}
		if err := s.Store.Did(ctx, e.ID, Waiting, tries, err.Error(), now); err != nil {
			s.log("cannot record that an errand failed", err)
		}
		return
	}

	pass.Sent++
	s.settle(ctx, e, Done, "", now)
}

// settle : Records how an errand ended.
func (s *Sender) settle(ctx context.Context, e Errand, state State, why string, now time.Time) {
	if err := s.Store.Did(ctx, e.ID, state, e.Tries+1, why, now); err != nil {
		s.log("cannot record what became of an errand", err)
	}
}

// tell : Says that something never got there.
func (s *Sender) tell(ctx context.Context, e Errand) {
	if s.Told == nil || e.Kind != AlarmSet {
		return
	}
	s.Told(ctx, e.UserID, "I could not set the alarm on your phone, sir. "+
		"I have stopped trying; it will need setting by hand.")
}

// tries : How many attempts are worth making.
func (s *Sender) tries() int {
	if s.Tries <= 0 {
		return Tries
	}
	return s.Tries
}

// clock : Now, in UTC.
func (s *Sender) clock() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now().UTC()
}

// log : A failure of the machinery rather than of an errand.
func (s *Sender) log(msg string, err error) {
	if s.Logger != nil {
		s.Logger.Error(msg, slog.Any("error", err))
	}
}

// note : Something that happened to one errand.
func (s *Sender) note(msg string, e Errand, why string) {
	if s.Logger == nil {
		return
	}
	s.Logger.Warn(msg,
		slog.String("kind", string(e.Kind)),
		slog.String("about", e.About),
		slog.Int("tries", e.Tries),
		slog.String("why", why))
}
