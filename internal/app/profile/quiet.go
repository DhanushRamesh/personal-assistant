package profile

import (
	"fmt"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/event"
	"github.com/DhanushRamesh/personal-assistant/internal/base/prompt"
)

// Still : The shortest silence worth counting.
//
// Four hours. Three was tried and caught the ordinary sparseness of
// a day: their devices report perhaps sixty things in a day, bursty,
// so an afternoon with nothing in it for three hours is not a thing
// they do, it is a quiet afternoon. Four is long enough that
// something has actually stopped.
const Still = 4 * time.Hour

// Nights : The most silences listed.
//
// Four weeks of them, one or two a day, so this is a ceiling rather
// than a limit anybody meets. It exists because a device that goes
// quiet for a fortnight produces one silence and a device that
// flickers produces hundreds.
const Nights = 60

// Quiet : The long silences, and when they happen.
//
// The half of somebody's day that nothing reports. Owner, on why the
// phone's locking was worth sending at all: *"it can understand the
// pattern that at night the screen will be locked around 1 am and
// will be opened again by 9 am... it can understand like I'm
// sleeping."*
//
// Nothing else here could see that. Things that happen are counted
// by the dozen, and what tends to follow what is looked for inside
// ninety minutes, so an eight hour silence is invisible to every
// other part of the evidence. It is not a gap in the data: it is the
// largest thing they do each day.
//
// Reported as a shape and never as a conclusion. This does not know
// that a silence from one in the morning is sleep, and must not: the
// same shape in the afternoon is something else entirely, and a
// person who works nights would be described wrongly by anything
// that assumed. The hours are given and the reading is left to the
// model, which is the division everywhere else here.
//
// Nothing in it names a kind of event. A silence is a silence
// whatever stopped.
func Quiet(events []event.Event, where *time.Location) string {
	if where == nil {
		where = time.UTC
	}
	moments := collapsed(events, where)
	if len(moments) < 2 {
		return ""
	}

	lines := make([]string, 0, Nights)
	for i := 1; i < len(moments); i++ {
		gap := moments[i].at.Sub(moments[i-1].at)
		if gap < Still {
			continue
		}
		if len(lines) == Nights {
			break
		}
		from := moments[i-1].at.In(where)
		to := moments[i].at.In(where)
		lines = append(lines, fmt.Sprintf("- %s %s to %s %s, %s",
			from.Format("Mon 2 Jan"), from.Format("3:04 pm"),
			to.Format("Mon 2 Jan"), to.Format("3:04 pm"), spoken(gap)))
	}
	if len(lines) == 0 {
		return ""
	}

	return prompt.Block(
		prompt.Text(
			"The stretches when nothing at all was reported, longer than four hours, in order.",
			"Everything above is things that happened; these are the times nothing did, and no amount of counting what happened can show them.",
			"They are listed and not summarised, because which of them belong together is the question.",
			"Look at what hour they begin, how long they run, whether they fall on the same days of the week, and whether they have been moving.",
			"A stretch that keeps falling at the same hour is the largest thing they do that day, and nothing will ever report it directly.",
			"What it is depends on the hour and the length, and that is yours to work out: the same shape at night and in the afternoon are not the same thing, and somebody who works nights would be described wrongly by anything that assumed.",
			"Whatever you work out, name it for what was recorded and not for what you take it to be.",
			"Nothing reported between one and nine is the observation, and it stays the observation however obvious its explanation looks.",
			"Naming one of these after the activity you believe it is turns a conclusion into a heading, and nobody reading it a month later can tell that it ever was one.",
			"So write the stretch first and your reading of it after, marked as yours -- which would fit, which may be -- and leave them in that order.",
		),
		prompt.Lines(lines...),
	)
}
