package profile

import (
	"fmt"
	"sort"
	"time"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
)

// Apart : How far from the usual hour is worth remarking on.
//
// Ninety minutes. Half an hour is traffic and an hour is a long
// queue; by an hour and a half somebody has done something
// different, and saying so is a remark rather than a pedantry.
const Apart = 90 * time.Minute

// Windows : The same habit at three distances -- usually, this
// week, and today.
//
// Owner, on being told the profile could not say he was up late:
// *"recent should count more than old."*
//
// It could not, and the reason was that there was only ever one
// window. Every habit in the description is worked out over four
// weeks flat, so a day three weeks ago counts exactly as much as
// yesterday: somebody who started coming home at eleven this week
// is still described as arriving at nine, and stays described that
// way until three old weeks have aged out. The description is true
// of the month and wrong about tonight, which is the one thing
// anybody is actually asking about.
//
// So the habit is given three distances and only the differences
// are said. "Usually around 9pm, this week around 10pm, tonight at
// 11:40" is a sentence somebody can act on; three identical hours
// are not, and are left out.
//
// Nothing here is new evidence. It is the same moments the rest of
// the description is built from, counted three times over three
// lengths of time.
func Windows(events []event.Event, where *time.Location, now time.Time) string {
	if where == nil {
		where = time.UTC
	}
	moments := collapsed(events, where)
	if len(moments) == 0 {
		return ""
	}

	// Grouped by the thing itself, so arriving home and leaving home
	// are two habits and not one.
	byLabel := map[string][]moment{}
	for _, m := range moments {
		byLabel[m.label()] = append(byLabel[m.label()], m)
	}

	labels := make([]string, 0, len(byLabel))
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	midnight := time.Date(now.In(where).Year(), now.In(where).Month(), now.In(where).Day(),
		0, 0, 0, 0, where)
	weekAgo := now.Add(-Lately)

	var lines []string
	for _, label := range labels {
		got := byLabel[label]
		if len(got) < Enough {
			continue
		}

		var all, week, today []time.Time
		for _, m := range got {
			all = append(all, m.at)
			if m.at.After(weekAgo) {
				week = append(week, m.at)
			}
			if m.at.After(midnight) {
				today = append(today, m.at)
			}
		}

		usual, _, of, steady := usualHour(all, where)
		if of == 0 || !steady {
			// No hour to be late against. A thing somebody does at
			// no particular time cannot be done late.
			continue
		}

		// Said only where it differs. Three identical hours is a
		// sentence that teaches nobody anything and costs the same
		// to read as one that does.
		said := ""
		if len(today) > 0 {
			if d := apart(today[len(today)-1], usual, where); d >= Apart {
				said = fmt.Sprintf("%s: usually around %s, today at %s.",
					label, oclock(usual), oclock(today[len(today)-1].In(where).Hour()))
			}
		}
		if said == "" && len(week) >= Enough {
			if lately, _, _, ok := usualHour(week, where); ok {
				if d := hoursApart(lately, usual); d >= Apart {
					said = fmt.Sprintf("%s: usually around %s, but around %s this week.",
						label, oclock(usual), oclock(lately))
				}
			}
		}
		if said != "" {
			lines = append(lines, said)
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return prompt.Block(
		prompt.Text(
			"And where today or this week sits oddly against the habit, the same thing at three distances:",
			"These are the only ones that differ. Everything else is happening at its usual hour and is not worth saying.",
		),
		prompt.Lines(lines...))
}

// apart : How far a moment is from an hour of the day.
func apart(at time.Time, hour int, where *time.Location) time.Duration {
	return hoursApart(at.In(where).Hour(), hour)
}

// hoursApart : The distance between two hours of the day, the short
// way round the clock. Eleven at night and one in the morning are two
// hours apart and not twenty-two.
func hoursApart(a, b int) time.Duration {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 12 {
		d = 24 - d
	}
	return time.Duration(d) * time.Hour
}
