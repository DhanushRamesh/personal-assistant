package event

import (
	"sort"
	"time"
)

// Touches : The kinds that mean the person did something to the phone.
//
// Deliberately not every kind. A fix and a battery reading arrive while
// the phone lies on a table, and an incoming call is something that
// happens to somebody rather than something they did -- counting one at
// four in the morning would cut a night in half and report two short
// sleeps where there was one long one. A call that was answered shows up
// here anyway, because answering it unlocks the screen.
var Touches = map[string]bool{
	"screen.unlocked": true,
	"screen.locked":   true,
	"call.made":       true,
	"music.state":     true,
	"power.plugged":   true,
	"power.unplugged": true,
}

// Untouched : A stretch in which nobody touched the phone.
//
// Named for what was measured and not for what it means. This is the
// phone lying still; the person was probably asleep, and may have been
// reading a book in the next room. The difference matters at the point
// where it is said out loud, so the words for it are the caller's to
// choose and this says only what it saw.
type Untouched struct {
	// From, To : When the phone was last touched, and next touched.
	From, To time.Time
	// Next : The longest other gap in the same window, which is what
	// makes a night clear-cut or arguable. One stretch of six hours
	// against a next-longest of fifty minutes is not a judgement call;
	// two of three hours each is.
	Next time.Duration
}

// Long : How long the phone lay untouched.
func (u Untouched) Long() time.Duration { return u.To.Sub(u.From) }

// Clear : Whether one stretch stands out enough to be worth saying
// without hedging.
//
// Twice the next-longest. Measured on two of his own nights: 5.74 hours
// against a next-longest of 0.87, and 6.77 against 1.63 -- four times
// and six times over. A night that only manages twice is one where he
// was up and about in the middle of it, and the honest answer there
// names both stretches rather than picking one.
func (u Untouched) Clear() bool {
	return u.Next <= 0 || u.Long() >= 2*u.Next
}

// Quiet : The longest stretch in which the phone went untouched between
// from and to, and whether there was one at all.
//
// The times given are every moment the phone was touched; anything
// outside the window is ignored, and the window's own edges do not
// count as touches. A window with nothing in it has no answer -- a
// phone that was never touched says nothing about whether anybody
// slept, and a silence with no activity either side of it is not a gap
// between two things.
func Quiet(touched []time.Time, from, to time.Time) (Untouched, bool) {
	within := make([]time.Time, 0, len(touched))
	for _, t := range touched {
		if !t.Before(from) && t.Before(to) {
			within = append(within, t)
		}
	}
	if len(within) < 2 {
		return Untouched{}, false
	}
	sort.Slice(within, func(i, j int) bool { return within[i].Before(within[j]) })

	var best, next Untouched
	for i := 1; i < len(within); i++ {
		gap := Untouched{From: within[i-1], To: within[i]}
		switch {
		case gap.Long() > best.Long():
			next, best = best, gap
		case gap.Long() > next.Long():
			next = gap
		}
	}
	if best.Long() <= 0 {
		return Untouched{}, false
	}
	best.Next = next.Long()
	return best, true
}
