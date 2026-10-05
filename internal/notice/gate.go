package notice

import (
	"sync"
	"time"
)

// Gate : What may be said, and when.
//
// Two refusals, in the order they cost nothing to check. Nobody is
// there to hear it; this exact event has been weighed already.
//
// It used to hold a third -- a quiet between things said, and a
// once-a-day rule per thing -- and both went with the remarks they
// were for. Politeness is for company. What is left here is a
// device saying something has to be known now, and the only
// question about the moment is whether anybody is in it.
type Gate struct {
	// Away : Whether the person is known to be elsewhere. Nil means
	// nobody is watching, and nothing is said at all -- an assistant
	// that cannot tell whether the room is empty talks to the wall.
	//
	// The same shape as the reminder loop's, and for the same reason
	// given there: anything that is not a confident current answer
	// must come back as "cannot tell", and the caller decides.
	Away func(userID string) (away bool, known bool)

	mu sync.Mutex
	// weighed : Every event considered lately, whatever came of it.
	//
	// A candidate stays inside the watcher's look-back window for
	// five minutes and the watcher ticks every thirty seconds, so
	// without this one warning is announced ten times.
	weighed map[string]time.Time
}

// Weighing : How long a candidate stays considered.
//
// Longer than the window it can be found in, so each thing is
// weighed once and recorded once.
const Weighing = 10 * time.Minute

// Considered : Whether this has already been weighed lately, marking
// it weighed if not.
//
// Asked before anything else is done with a candidate. A thing
// weighed and refused a minute ago has not become interesting since,
// and asking again writes a second row saying so.
//
// Owner: *"if it's urgent it should say every time, regardless of
// twenty minutes since the last and twice within ten minutes -- if
// an urgent event is captured, say it."* Quite right, and it cannot
// simply be let through: the watcher re-reads the last five minutes
// every thirty seconds, so one warning would be found ten times and
// said ten times.
//
// So it is weighed once per *event* rather than once per spell of
// time. Two warnings a minute apart are two events and both are
// said; the same one found again on the next tick is the same
// moment and is not. The record is kept longer than the window an
// event can be found in, so nothing comes back after it is
// forgotten.
func (g *Gate) Considered(c Candidate, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	k := key(c)
	if when, had := g.weighed[k]; had && now.Sub(when) < Weighing {
		return true
	}
	if g.weighed == nil {
		g.weighed = map[string]time.Time{}
	}
	g.weighed[k] = now
	for label, when := range g.weighed {
		if now.Sub(when) >= Weighing {
			delete(g.weighed, label)
		}
	}
	return false
}

// Forget : Unmarks a candidate, so the next look weighs it again.
//
// For the one case where weighing it was wasted: the model could not
// be reached. Nothing was decided and nothing was said, so the mark
// records an attempt rather than a judgement, and leaving it there
// loses the thing entirely -- the arrival falls out of the look-back
// window long before the ten minutes are up.
//
// Measured, at five past midnight on 6 October. A battery at twenty
// per cent, marked by the phone to be said at once, was found,
// allowed, and the ask failed because the provider was busy. It was
// never tried again. The one class of announcement that exists
// because waiting costs something was lost to a hiccup lasting
// seconds.
func (g *Gate) Forget(c Candidate) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.weighed, key(c))
}

// Allow : Whether this may be said now, and why not when it may not.
//
// The reason is returned rather than asked for afterwards, because
// asking twice would be two different answers: presence moves while
// nobody is looking, and a page that explained a refusal by asking
// again would explain the wrong one.
//
// Takes no candidate. Nothing about the thing itself can stop it
// being said any more -- that was the remarks' half of this -- so
// the only question left is about the room.
func (g *Gate) Allow(userID string) (bool, string) {
	if g == nil {
		return false, "there is no gate"
	}
	if g.Away == nil {
		return false, "nothing is watching the room"
	}
	// Everything is said to somebody who is there, immediate
	// announcements included. Owner: *"event arrives, check if
	// present, announce."* An announcement into an empty room is not
	// an announcement, and the phone shows its own battery.
	if away, known := g.Away(userID); away {
		return false, "they are not in the room"
	} else if !known {
		return false, "it cannot tell whether they are in the room"
	}

	return true, ""
}

// key : How one arrival is told from the next.
//
// The thing and the moment it happened. Two warnings a minute apart
// are two arrivals; the same one found again on the next tick is one.
func key(c Candidate) string {
	return c.Label + "@" + c.At.UTC().Format(time.RFC3339Nano)
}
