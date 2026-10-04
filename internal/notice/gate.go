package notice

import (
	"fmt"
	"strconv"
	"strings"
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

	mu   sync.Mutex
	last time.Time
	// said : What the last remark actually was, so a refusal can name
	// it. "Something was said four minutes ago" tells the person
	// reading the page nothing they can act on; the sentence itself
	// tells them whether the thing being held back was worth more
	// than the thing that beat it to it.
	said  string
	spoke map[string]time.Time
	// weighed : Everything considered lately, whatever came of it.
	//
	// Separate from spoke, which is only what was said and holds for
	// a day. A candidate stays inside the watcher's look-back window
	// for five minutes and the watcher ticks every thirty seconds, so
	// without this one song produces ten identical rows saying it was
	// held.
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
func (g *Gate) Considered(c Candidate, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	if when, had := g.weighed[c.Label]; had && now.Sub(when) < Weighing {
		return true
	}
	if g.weighed == nil {
		g.weighed = map[string]time.Time{}
	}
	g.weighed[c.Label] = now
	for label, when := range g.weighed {
		if now.Sub(when) >= Weighing {
			delete(g.weighed, label)
		}
	}
	return false
}

// Spoke : Records what was said, so a later refusal can name it.
func (g *Gate) Spoke(said string, now time.Time) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.said = said
	g.last = now
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
	// Where they are decides how it is said, not whether. A warning
	// follows them onto the phone; a remark is company and company
	// belongs in the room.
	if !c.Urgent {
		if away, known := g.Away(userID); away {
			return false, "they are not in the room"
		} else if !known {
			return false, "it cannot tell whether they are in the room"
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// A warning is not held back for politeness. The quiet between
	// remarks exists so company does not become a nuisance; a
	// battery about to die is not company, and a person who is not
	// told because something else was said four minutes ago is worse
	// off than one who is interrupted twice.
	//
	// The once-a-day rule goes with it: a battery reaching five per
	// cent twice in a day is two warnings, not one and a silence.
	if c.Urgent {
		g.last = now
		return true, ""
	}

	if !g.last.IsZero() && now.Sub(g.last) < Quiet {
		return false, quoted(g.said) + " was said " + spoken(now.Sub(g.last)) + " ago"
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

// quoted : The last remark, for a refusal to name.
//
// Falls back to saying that something was, for the gate that has
// been handed a refusal before it has ever allowed anything -- which
// happens after a restart, when the clock is fresh but the quiet is
// not.
func quoted(said string) string {
	said = strings.TrimSpace(said)
	if said == "" {
		return "something"
	}
	return strconv.Quote(said)
}

// spoken : A duration as somebody would say it.
func spoken(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "moments"
	case d < time.Hour:
		return count(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return count(int(d.Hours()), "hour")
	default:
		return count(int(d.Hours()/24), "day")
	}
}

// count : A number and its noun, singular when it is one.
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
