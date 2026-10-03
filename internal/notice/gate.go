package notice

import (
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

// Allow : Whether this may be said now.
//
// Records that it was, when it says yes. A caller that asks and then
// does not speak has used up the quiet, which is the safe way round:
// asking is cheap and the cost of being wrong is one remark missed,
// not two remarks together.
func (g *Gate) Allow(userID string, c Candidate, now time.Time) bool {
	if g == nil {
		return false
	}
	if g.Away == nil {
		return false
	}
	if away, known := g.Away(userID); away || !known {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.last.IsZero() && now.Sub(g.last) < Quiet {
		return false
	}
	if when, said := g.spoke[c.Label]; said && now.Sub(when) < Over {
		return false
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
	return true
}
