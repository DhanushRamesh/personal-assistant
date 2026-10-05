package alarm

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Defaults for a Keeper.
const (
	// DefaultEvery : How often to try the ones still waiting.
	//
	// Often enough that a phone coming back onto the tailnet is
	// picked up within a few minutes, rarely enough that a phone
	// that is simply off overnight costs almost nothing.
	DefaultEvery = 2 * time.Minute

	// DefaultTries : How many times to try before giving up.
	//
	// A bound as well as the clock, so a phone that answers but
	// refuses -- a Tasker profile switched off, a changed secret --
	// does not retry every two minutes until morning.
	DefaultTries = 20

	// Margin : How long before the alarm to stop trying.
	//
	// Setting an alarm for a moment that has already arrived is no
	// use, and setting one a few seconds before it is worse than
	// useless: the phone rings while the person is still reading
	// the message that said it could not be set.
	Margin = time.Minute
)

// Keeper : A Setter that keeps trying.
//
// Owner: *"if reaching the phone failed, then it has to be retried
// a few times, before the actual alarm time."* A phone is asleep,
// off the tailnet, or in a lift far more often than it is broken,
// and all of those fix themselves within minutes. The first failure
// is still reported -- the person is told plainly that the alarm is
// not set yet -- but it is no longer the end of the matter.
//
// Retrying stops at the alarm's own time, because an alarm for a
// moment that has passed helps nobody.
type Keeper struct {
	// Setter : What actually reaches the phone. Required.
	Setter Setter
	// Every : How often to try again. Zero selects DefaultEvery.
	Every time.Duration
	// Tries : How many attempts before giving up. Zero selects
	// DefaultTries.
	Tries int
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger

	mu      sync.Mutex
	waiting map[string]*attempt
}

// attempt : One alarm still owed, and how often it has been tried.
type attempt struct {
	alarm Alarm
	tries int
}

// Set : Asks the phone, and keeps the alarm to try again if that
// failed.
//
// The error is returned either way, so the person is told the truth
// at the time: the alarm is not set. What has changed is that it
// may yet be, without them having to do anything.
func (k *Keeper) Set(ctx context.Context, a Alarm) error {
	err := k.Setter.Set(ctx, a)
	if err == nil {
		k.forget(a.Key)
		return nil
	}
	k.keep(a)
	return err
}

// Run : Tries the ones still waiting until the context ends.
func (k *Keeper) Run(ctx context.Context) {
	every := k.Every
	if every <= 0 {
		every = DefaultEvery
	}
	tick := time.NewTicker(every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			k.pass(ctx)
		}
	}
}

// Waiting : How many alarms are still owed.
func (k *Keeper) Waiting() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.waiting)
}

// pass : One go at everything still waiting.
func (k *Keeper) pass(ctx context.Context) {
	for _, owed := range k.due() {
		if err := k.Setter.Set(ctx, owed.alarm); err != nil {
			k.log("still cannot set an alarm on the phone", owed, err)
			continue
		}
		k.forget(owed.alarm.Key)
		k.log("the alarm reached the phone on a later try", owed, nil)
	}
}

// due : The attempts worth making now, dropping the ones whose time
// has come or which have been tried too often.
//
// Counted here rather than after the attempt so that a try is spent
// even when it fails, which is what bounds this.
func (k *Keeper) due() []*attempt {
	k.mu.Lock()
	defer k.mu.Unlock()

	tries := k.Tries
	if tries <= 0 {
		tries = DefaultTries
	}
	now := k.clock()

	var out []*attempt
	for key, owed := range k.waiting {
		switch {
		case !owed.alarm.At.After(now.Add(Margin)):
			delete(k.waiting, key)
			k.logLocked("gave up on an alarm, its time came first", owed, nil)
		case owed.tries >= tries:
			delete(k.waiting, key)
			k.logLocked("gave up on an alarm, the phone never took it", owed, nil)
		default:
			owed.tries++
			out = append(out, owed)
		}
	}
	return out
}

// keep : Remembers an alarm to try again, unless its time has
// effectively come.
func (k *Keeper) keep(a Alarm) {
	if !a.At.After(k.clock().Add(Margin)) {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.waiting == nil {
		k.waiting = make(map[string]*attempt)
	}
	// Keyed by the reminder, so one that is amended twice is owed
	// one alarm rather than three.
	k.waiting[a.Key] = &attempt{alarm: a}
}

// forget : Drops an alarm that is no longer owed.
func (k *Keeper) forget(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.waiting, key)
}

// clock : Now, or the real one.
func (k *Keeper) clock() time.Time {
	if k.Now == nil {
		return time.Now()
	}
	return k.Now()
}

// log : Records what happened to one owed alarm.
func (k *Keeper) log(msg string, owed *attempt, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.logLocked(msg, owed, err)
}

// logLocked : log, for callers already holding the lock.
func (k *Keeper) logLocked(msg string, owed *attempt, err error) {
	if k.Logger == nil {
		return
	}
	at := []any{"at", owed.alarm.At, "tries", owed.tries}
	if err != nil {
		at = append(at, "error", err)
	}
	k.Logger.Warn(msg, at...)
}

// Clear : Asks the phone to drop an alarm, and stops owing it one.
//
// Forgotten first. A reminder cancelled while its alarm was still
// being retried must not have that alarm appear a minute later,
// which is the one way this could wake somebody for a thing they
// called off.
func (k *Keeper) Clear(ctx context.Context, a Alarm) error {
	k.forget(a.Key)
	return k.Setter.Clear(ctx, a)
}
