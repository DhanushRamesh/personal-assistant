package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// Together : Repeats of the same thing closer than this are one
// occurrence of it.
//
// Without this a laptop that rejoined the same network twenty-five
// times in a day reads as the most significant thing in the person's
// week. The collapsing is generic rather than a list of noisy kinds:
// the server has no business knowing which of somebody's events are
// the boring ones, and a list like that is wrong the moment a new
// device starts reporting.
const Together = 2 * time.Hour

// Soon : How close two things must be for one to count as following
// the other.
const Soon = 90 * time.Minute

// Fewest : Seen fewer times than this, it is something that happened
// rather than something they do.
const Fewest = 2

// Mostly : The most lines of any one sort, so a noisy week cannot
// crowd out everything else.
const Mostly = 12

// Rhythm : What the person's days look like, counted.
//
// Counted rather than described, and that is the point. The profile
// itself is prose, which reads like somebody who knows them and cannot
// be checked; the evidence it is written from should be the opposite.
// How many times, on how many days, at roughly what hour and what
// tends to follow what are all things a reader can argue with, and a
// model handed them writes about habits instead of inventing them.
//
// Empty when there is nothing worth saying, so a person whose devices
// report nothing is described from their words alone rather than from
// a heading with nothing under it.
func Rhythm(events []event.Event, where *time.Location) string {
	if where == nil {
		where = time.UTC
	}
	moments := collapsed(events, where)
	if len(moments) == 0 {
		return ""
	}

	days := map[string]bool{}
	for _, m := range moments {
		days[m.at.In(where).Format("2006-01-02")] = true
	}

	often := howOften(moments, where, len(days))
	after := whatFollows(moments, where)
	if often == "" && after == "" {
		return ""
	}

	return prompt.Block(
		prompt.Text(
			fmt.Sprintf("Their devices also reported what they did, over the same week: %d things on %d %s.",
				len(moments), len(days), plural("day", len(days))),
			"These are observations, not things they said. A run of the same thing in quick succession has been counted once.",
		),
		often,
		after,
	)
}

// moment : One thing happening, once.
type moment struct {
	kind  string
	value string
	at    time.Time
}

// label : The thing itself, however it is named.
func (m moment) label() string {
	if m.value == "" {
		return m.kind
	}
	return m.kind + " " + strconv.Quote(m.value)
}

// collapsed : The events in order, with runs of the same thing reduced
// to their first occurrence.
func collapsed(events []event.Event, where *time.Location) []moment {
	in := make([]moment, 0, len(events))
	for _, e := range events {
		in = append(in, moment{kind: e.Kind, value: valueOf(e.Payload), at: e.OccurredAt})
	}
	sort.Slice(in, func(i, j int) bool { return in[i].at.Before(in[j].at) })

	last := map[string]time.Time{}
	out := make([]moment, 0, len(in))
	for _, m := range in {
		key := m.label()
		if at, seen := last[key]; seen && m.at.Sub(at) < Together {
			continue
		}
		last[key] = m.at
		out = append(out, m)
	}
	return out
}

// howOften : What they do, most often first.
func howOften(moments []moment, where *time.Location, days int) string {
	type tally struct {
		times int
		days  map[string]bool
		when  []time.Time
	}
	by := map[string]*tally{}
	order := []string{}
	for _, m := range moments {
		key := m.label()
		t, seen := by[key]
		if !seen {
			t = &tally{days: map[string]bool{}}
			by[key], order = t, append(order, key)
		}
		t.times++
		t.days[m.at.In(where).Format("2006-01-02")] = true
		t.when = append(t.when, m.at)
	}

	sort.SliceStable(order, func(i, j int) bool { return by[order[i]].times > by[order[j]].times })

	lines := make([]string, 0, len(order))
	for _, key := range order {
		if len(lines) == Mostly {
			break
		}
		t := by[key]
		line := fmt.Sprintf("- %s: %d %s, on %d of %d %s",
			key, t.times, plural("time", t.times), len(t.days), days, plural("day", days))
		if at := aroundWhen(t.when, where); at != "" {
			line += ", " + at
		}
		lines = append(lines, line)
	}
	return prompt.Lines(append([]string{"How often, most to least:"}, lines...)...)
}

// whatFollows : What tends to come straight after what.
//
// Only what immediately follows, and only within Soon. Counting every
// pair within the window instead would make a busy hour look like a
// routine, and what somebody means by "I always do this then that" is
// the next thing, not any later thing.
func whatFollows(moments []moment, where *time.Location) string {
	type pair struct {
		times int
		gaps  []time.Duration
	}
	by := map[string]*pair{}
	order := []string{}
	for i := 1; i < len(moments); i++ {
		before, after := moments[i-1], moments[i]
		gap := after.at.Sub(before.at)
		if gap > Soon || before.label() == after.label() {
			continue
		}
		key := before.label() + ", then " + after.label()
		p, seen := by[key]
		if !seen {
			p = &pair{}
			by[key], order = p, append(order, key)
		}
		p.times++
		p.gaps = append(p.gaps, gap)
	}

	sort.SliceStable(order, func(i, j int) bool { return by[order[i]].times > by[order[j]].times })

	lines := make([]string, 0, len(order))
	for _, key := range order {
		if by[key].times < Fewest || len(lines) == Mostly {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %d %s, usually within %s",
			key, by[key].times, plural("time", by[key].times), spoken(middle(by[key].gaps))))
	}
	if len(lines) == 0 {
		return ""
	}
	return prompt.Lines(append([]string{"One thing following another:"}, lines...)...)
}

// aroundWhen : Roughly what time of day, when there is one.
//
// Nothing at all when the hours are spread: "usually around 2pm" for
// something that happens at any hour is the kind of detail that gets
// written into a description and then believed.
func aroundWhen(when []time.Time, where *time.Location) string {
	if len(when) < Fewest {
		return ""
	}
	hours := make([]int, 0, len(when))
	for _, t := range when {
		hours = append(hours, t.In(where).Hour())
	}
	sort.Ints(hours)
	if hours[len(hours)-1]-hours[0] > 3 {
		return "at no particular hour"
	}
	return "usually around " + oclock(hours[len(hours)/2])
}

// oclock : An hour as somebody would say it.
func oclock(h int) string {
	switch {
	case h == 0:
		return "midnight"
	case h == 12:
		return "midday"
	case h < 12:
		return fmt.Sprintf("%dam", h)
	default:
		return fmt.Sprintf("%dpm", h-12)
	}
}

// middle : The median, which a single long gap cannot drag about.
func middle(gaps []time.Duration) time.Duration {
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	return gaps[len(gaps)/2]
}

// spoken : A gap, roughly, in the words somebody would use.
func spoken(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d %s", int(d.Minutes()), plural("minute", int(d.Minutes())))
	default:
		return fmt.Sprintf("%d %s", int(d.Hours()), plural("hour", int(d.Hours())))
	}
}

// plural : A noun for a count, by the ordinary rule.
func plural(noun string, n int) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// valueOf : What an event carries, as the phone writes it.
func valueOf(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil || len(into) == 0 {
		return ""
	}
	if v, ok := into["value"]; ok && len(into) == 1 {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	keys := make([]string, 0, len(into))
	for k := range into {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, into[k]))
	}
	return strings.Join(parts, " ")
}
