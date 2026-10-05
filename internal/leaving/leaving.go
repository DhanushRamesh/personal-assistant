// Package leaving says when to set off for something in the diary.
//
// The one thing on the list of announcements worth having that the
// person cannot work out faster themselves. They know the film is at
// ten past six; what they do not know, standing in the kitchen, is
// that the road is busy tonight and twenty past five is the moment.
//
// Everything it needs was already here: the diary carries the
// address, the stays carry where they are, and the maps carry the
// road in traffic. Nothing is guessed. The model is given a sentence
// to say and no decision to make, which is the division that took
// the greeting from five of forty to twenty-four.
package leaving

import (
	"context"
	"strings"
	"time"
)

const (
	// Door : How long it takes to actually leave once told.
	//
	// Finding shoes, a key, a helmet. Said rather than measured, and
	// generous on purpose: being told ten minutes early costs a wait
	// and being told five minutes late costs the thing itself.
	Door = 10 * time.Minute

	// Soon : How far ahead a journey is worth working out.
	//
	// A route costs a call to somebody else's service and the answer
	// goes stale, so nothing is asked about a thing hours away. Wide
	// enough that a long drive is still caught before it matters.
	Soon = 2 * time.Hour

	// Late : How long after the moment has passed it is still worth
	// saying.
	//
	// Being told to leave for something that started half an hour ago
	// is not help, it is an accusation with a clock on it.
	Late = 15 * time.Minute
)

// Appointment : One thing in the diary that has somewhere to be.
type Appointment struct {
	// What : What they called it.
	What string
	// When : When it starts.
	When time.Time
	// Where : The address as the diary gives it, which is what makes
	// this possible at all. An entry without one is not for here.
	Where string
}

// Journey : How long it takes this person to get there, however
// that was found.
//
// Takes who as well as where, because the answer depends on both:
// the same cinema is twenty minutes from home and five from the
// office.
type Journey func(ctx context.Context, userID, where string) (time.Duration, error)

// Leaving : What to say, and whether to say anything.
//
// Pure, and separately testable from everything that fetches. Given
// how long the journey takes, it answers the only question there is:
// is this the moment.
//
// The moment is the start, less the journey, less getting out of the
// door. Before it there is nothing to say and the person is not
// late; after it by more than Late there is nothing useful to say
// either, because the thing has begun.
func Leaving(a Appointment, takes time.Duration, now time.Time) (string, bool) {
	what := strings.TrimSpace(a.What)
	if what == "" || a.When.IsZero() || takes <= 0 {
		return "", false
	}

	set := a.When.Add(-takes).Add(-Door)
	if now.Before(set) {
		return "", false
	}
	if now.After(a.When.Add(Late)) {
		return "", false
	}

	// Past the moment and the thing has not started: say so as it is,
	// rather than counting backwards from a departure already missed.
	if now.After(set.Add(2 * time.Minute)) {
		return what + " starts " + spell(a.When.Sub(now)) +
			" from now and it is about " + spell(takes) + " away.", true
	}
	return "Time to set off for " + what + ": it is about " +
		spell(takes) + " away.", true
}

// Worth : Whether this entry is one to work a journey out for.
//
// Asked before anything is fetched, because the fetch is the
// expensive half. An entry with nowhere to go has nothing to
// compute, and one hours away has nothing to say yet.
func Worth(a Appointment, now time.Time) bool {
	return strings.TrimSpace(a.Where) != "" &&
		!a.When.IsZero() &&
		a.When.After(now.Add(-Late)) &&
		a.When.Before(now.Add(Soon))
}

// spell : A duration as somebody would say it out loud.
//
// Rounded to five minutes past a quarter of an hour, because a
// journey is not known to the minute and saying twenty-three minutes
// claims it is.
func spell(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "a moment"
	case d < 15*time.Minute:
		return count(int(d.Round(time.Minute).Minutes()), "minute")
	case d < time.Hour:
		return count(int((d/(5*time.Minute))*(5*time.Minute)/time.Minute), "minute")
	case d < 2*time.Hour:
		return "about an hour"
	default:
		return count(int(d.Round(30*time.Minute).Hours()), "hour")
	}
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
