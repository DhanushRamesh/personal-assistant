package profile

import (
	"fmt"
	"sort"
	"time"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
)

// Span : One length of time, and what it is called.
//
// Owner: *"today, what's different from yesterday. Weekly, what's
// different from last week. Same for this month, last month, this
// year, last year -- for every event type. I might be listening to
// a song now, same as yesterday, same last week and now too, so
// that's what's trending in my life."*
//
// The windows are the same question at four lengths, and each is
// answered the same way: how much of this thing now, how much of it
// in the one before, and which way it has moved.
type Span struct {
	Name string
	Long time.Duration
}

// Spans : The four lengths, shortest first.
//
// A month is twenty-eight days and a year is fifty-two weeks, so
// that each window is a whole number of the one below it. Calendar
// months of unequal length would make a February look like a decline.
var Spans = []Span{
	{"today", 24 * time.Hour},
	{"this week", 7 * 24 * time.Hour},
	{"this month", 28 * 24 * time.Hour},
	{"this year", 364 * 24 * time.Hour},
}

// Seen : How often a thing must appear in a window to be worth
// comparing.
//
// Two. One is an event and not a habit, and the whole of this is
// about what repeats.
const Seen = 2

// Trending : What is rising, fading, new or gone, at each length of
// time.
//
// A window is only reported where there is evidence on both sides of
// it. With nine days of history a month has nothing to be compared
// against and is left out entirely rather than reported as a change
// from nothing -- which is the shape of mistake that makes a
// description untrustworthy the first week it runs.
func Trending(events []event.Event, where *time.Location, now time.Time) string {
	if where == nil {
		where = time.UTC
	}
	moments := collapsed(events, where)
	if len(moments) == 0 {
		return ""
	}

	oldest := moments[0].at
	for _, m := range moments {
		if m.at.Before(oldest) {
			oldest = m.at
		}
	}

	var said []string
	for _, span := range Spans {
		// Both sides or neither. A window needs a whole one of
		// itself before it, or "new this month" means only that the
		// server was switched on this month.
		if now.Sub(oldest) < 2*span.Long {
			continue
		}
		if line := moved(moments, where, now, span); line != "" {
			said = append(said, line)
		}
	}
	if len(said) == 0 {
		return ""
	}

	return prompt.Block(
		prompt.Text(
			"And what has been moving in their life, each length of time against the one before it:",
			"Only what changed is listed. A window with no history behind it yet is left out rather than guessed at.",
		),
		prompt.Lines(said...))
}

// moved : What rose, fell, arrived or stopped over one span.
func moved(moments []moment, where *time.Location, now time.Time, span Span) string {
	from := now.Add(-span.Long)
	before := now.Add(-2 * span.Long)

	nowCount, thenCount := map[string]int{}, map[string]int{}
	for _, m := range moments {
		switch {
		case m.at.After(from):
			nowCount[m.label()]++
		case m.at.After(before):
			thenCount[m.label()]++
		}
	}

	labels := map[string]bool{}
	for l := range nowCount {
		labels[l] = true
	}
	for l := range thenCount {
		labels[l] = true
	}
	ordered := make([]string, 0, len(labels))
	for l := range labels {
		ordered = append(ordered, l)
	}
	// By how much it moved, largest first: the point is what stands
	// out, and an alphabetical list buries it.
	sort.Slice(ordered, func(i, j int) bool {
		return swing(nowCount[ordered[i]], thenCount[ordered[i]]) >
			swing(nowCount[ordered[j]], thenCount[ordered[j]])
	})

	var parts []string
	for _, l := range ordered {
		is, was := nowCount[l], thenCount[l]
		switch {
		case was == 0 && is >= Seen:
			parts = append(parts, fmt.Sprintf("%s is new (%d)", l, is))
		case is == 0 && was >= Seen:
			parts = append(parts, fmt.Sprintf("%s has stopped (was %d)", l, was))
		case is >= Seen && was >= Seen && is >= was*2:
			parts = append(parts, fmt.Sprintf("%s is up, %d from %d", l, is, was))
		case was >= Seen && is*2 <= was:
			parts = append(parts, fmt.Sprintf("%s is down, %d from %d", l, is, was))
		}
		if len(parts) == 3 {
			// Three is what somebody can hold. The rest moved less
			// and would be read past.
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return span.Name + ": " + joinWith(parts, "; ") + "."
}

// swing : How far a count moved, for ordering. Arrivals and
// departures count the same, because both are news.
func swing(is, was int) int {
	d := is - was
	if d < 0 {
		d = -d
	}
	return d
}

// joinWith : The parts, separated.
func joinWith(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
