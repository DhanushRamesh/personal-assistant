package notice

import (
	"fmt"
	"sync"
	"time"
)

// Quiet : The least time between two things said unasked.
//
// Deliberately long to begin with. The failure that kills a feature
// like this is not missing something; it is being a nuisance by
// Tuesday, after which the person stops listening to the remarks
// that were worth hearing. It is easier to loosen this once it has
// earned its place than to win back somebody who has started
// ignoring it.
const Quiet = 20 * time.Minute

// Over : How long a thing stays said.
//
// Once per thing, but not for ever: somebody being called repeatedly
// today and again next month is two pieces of news, and a gate that
// remembers for ever turns the second into silence.
const Over = 24 * time.Hour

// Gate : What may be said, and when.
//
// Three refusals, in the order they cost nothing to check. Nobody is
// there to hear it; something was said too recently; this exact
// thing has been said already.
//
// Deliberately separate from deciding what is worth saying. One is
// about the remark and the other is about the moment, and the
// moment's rules are the ones that will be tuned.
type Gate struct {
	// Away : Whether the person is known to be elsewhere. Nil means
	// nobody is watching, and nothing is said at all -- an assistant
	// that cannot tell whether the room is empty talks to the wall.
	//
	// The same shape as the reminder loop's, and for the same reason
	// given there: anything that is not a confident current answer
	// must come back as "cannot tell", and the caller decides.
	Away func(userID string) (away bool, known bool)

	mu    sync.Mutex
	last  time.Time
	spoke map[string]time.Time
}

// Allow : Whether this may be said now, and why not when it may not.
//
// The reason is returned rather than asked for afterwards, because
// asking twice would be two different answers: the clock moves, and
// a gate that said no because something was said thirty seconds ago
// will say no for a different reason a minute later.
//
// Records that it was allowed, when it says yes. A caller that asks
// and then does not speak has used up the quiet, which is the safe
// way round: asking is cheap, and the cost of being wrong is one
// remark missed rather than two remarks together.
func (g *Gate) Allow(userID string, c Candidate, now time.Time) (bool, string) {
	if g == nil {
		return false, "there is no gate"
	}
	if g.Away == nil {
		return false, "nothing is watching the room"
	}
	if away, known := g.Away(userID); away {
		return false, "they are not in the room"
	} else if !known {
		return false, "it cannot tell whether they are in the room"
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.last.IsZero() && now.Sub(g.last) < Quiet {
		return false, "something was said " + spoken(now.Sub(g.last)) + " ago"
	}
	if when, said := g.spoke[c.Label]; said && now.Sub(when) < Over {
		return false, "this was said " + spoken(now.Sub(when)) + " ago"
	}

	if g.spoke == nil {
		g.spoke = map[string]time.Time{}
	}
	g.spoke[c.Label] = now
	g.last = now
	// What has gone stale, so a long-running server does not hold
	// every label it has ever seen.
	for label, when := range g.spoke {
		if now.Sub(when) >= Over {
			delete(g.spoke, label)
		}
	}
	return true, ""
}

// spoken : A duration as somebody would say it.
func spoken(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
