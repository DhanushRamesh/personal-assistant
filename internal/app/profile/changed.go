package profile

import (
	"fmt"
	"sort"
	"time"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
)

// Lately : How much of the four weeks counts as now.
//
// The rest is what it is compared against. A week against three
// weeks, so "now" is long enough to contain a whole weekly cycle and
// "before" is long enough that one odd week does not set the
// baseline.
const Lately = 7 * 24 * time.Hour

// Enough : The fewest occurrences before a change is worth reporting.
//
// Two of anything is a coincidence, and a description that announces
// a person has stopped doing something on the strength of two is
// worse than one that says nothing. The same number guards both
// directions.
const Enough = 3

// Spread : The fewest distinct days a thing must span.
//
// Three in one evening is one occasion, not a habit that has since
// ended. Counting days as well as times is what tells those apart.
const Spread = 2

// Swing : How much a rate must move to be called a change.
//
// Measured per week, because the two periods are different lengths.
// Twice or half, rather than any difference at all: devices miss
// things, weeks differ, and a description noting every wobble is
// noise that reads as insight.
const Swing = 2.0

// Changed : What is different about now compared with before.
//
// The half of a description that cannot be produced by sending more
// evidence. Everything else here answers "what do they do", which a
// model can read off a list; this answers "what did they stop doing",
// and a thing that stopped appears in no list at all. It exists only
// as the difference between two counts, so it is computed rather than
// asked for.
//
// Generic, like everything else here: nothing in it knows the name of
// a single kind of event. A thing is whatever label it carries, and
// what matters is whether that label used to turn up and does not any
// more.
//
// Empty when there is not enough history to compare against, which is
// the first four weeks and every person whose devices have just
// started reporting.
func Changed(events []event.Event, where *time.Location, now time.Time) string {
	if where == nil {
		where = time.UTC
	}
	moments := collapsed(events, where)
	if len(moments) == 0 {
		return ""
	}

	from := now.Add(-Lately)
	type counts struct {
		lately, earlier         int
		daysLately, daysEarlier map[string]bool
		shown                   string
	}
	by := map[string]*counts{}
	order := []string{}
	var earliest time.Time
	for _, m := range moments {
		if earliest.IsZero() || m.at.Before(earliest) {
			earliest = m.at
		}
		key := m.label()
		c, seen := by[key]
		if !seen {
			c = &counts{shown: m.shown(),
				daysLately: map[string]bool{}, daysEarlier: map[string]bool{}}
			by[key], order = c, append(order, key)
		}
		day := m.at.In(where).Format("2006-01-02")
		if m.at.Before(from) {
			c.earlier++
			c.daysEarlier[day] = true
		} else {
			c.lately++
			c.daysLately[day] = true
		}
	}

	// Nothing to compare against. Reporting "started" for every label
	// a device has ever sent would describe the day the phone was set
	// up, not the person.
	if earliest.After(from) || earliest.Equal(from) {
		return ""
	}

	// Per week, since the two periods are different lengths.
	weeksBefore := from.Sub(earliest).Hours() / (24 * 7)
	if weeksBefore <= 0 {
		return ""
	}

	var stopped, started, moreOrLess []string
	for _, key := range order {
		c := by[key]
		was := float64(c.earlier) / weeksBefore
		is := float64(c.lately)

		switch {
		case c.lately == 0 && c.earlier >= Enough && len(c.daysEarlier) >= Spread:
			stopped = append(stopped, fmt.Sprintf("- %s: %d %s before, none this week",
				c.shown, c.earlier, plural("time", c.earlier)))

		case c.earlier == 0 && c.lately >= Enough && len(c.daysLately) >= Spread:
			started = append(started, fmt.Sprintf("- %s: %d %s this week, none before",
				c.shown, c.lately, plural("time", c.lately)))

		case c.earlier == 0 || c.lately == 0:
			// Too little of it either way to say anything.

		case is >= was*Swing && c.lately >= Enough, was >= is*Swing && c.earlier >= Enough:
			moreOrLess = append(moreOrLess, fmt.Sprintf("- %s: %d this week against about %.0f a week before",
				c.shown, c.lately, was))
		}
	}

	if len(stopped)+len(started)+len(moreOrLess) == 0 {
		return ""
	}

	sort.Strings(stopped)
	sort.Strings(started)
	sort.Strings(moreOrLess)

	parts := []string{prompt.Text(
		"What is different about this week compared with the three before it.",
		"A thing that has stopped appears nowhere in the lists above, so it is counted here instead.",
		"These are counts, not conclusions: something may have stopped because they stopped doing it, or because a device stopped reporting it.",
		"Say which you think it is, or say the change without a reason.",
	)}
	if len(stopped) > 0 {
		parts = append(parts, prompt.Lines(append([]string{"No longer happening:"}, atMost(stopped)...)...))
	}
	if len(started) > 0 {
		parts = append(parts, prompt.Lines(append([]string{"New this week:"}, atMost(started)...)...))
	}
	if len(moreOrLess) > 0 {
		parts = append(parts, prompt.Lines(append([]string{"Happening at a different rate:"}, atMost(moreOrLess)...)...))
	}
	return prompt.Block(parts...)
}

// atMost : Mostly lines, so one strange week cannot fill the
// description with changes.
func atMost(lines []string) []string {
	if len(lines) > Mostly {
		return lines[:Mostly]
	}
	return lines
}
