package profile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/base/world/routine"
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

// Reach : How many events ahead to look for something that went with
// this one.
//
// Pairing used to be adjacency -- each event against the one directly
// after it -- which only finds a relationship when nothing else
// happens in between. The useful ones are exactly the opposite: a
// network dropping as somebody leaves a place is two devices reporting
// the same departure, and anything else landing between them hid it.
// A window finds it; adjacency found it by luck.
//
// Bounded, because a window is quadratic in how densely events arrive
// and a device that starts reporting every few seconds should cost
// this a constant rather than a square. Twelve is well past any run of
// related events seen so far, and Soon ends the window first in all
// but the densest minute.
const Reach = 12

// Fewest : Seen fewer times than this, it is something that happened
// rather than something they do.
const Fewest = 2

// Mostly : The most lines of any one sort, so a noisy week cannot
// crowd out everything else.
const Mostly = 12

// Kinds : The most sorts of thing listed in the counted half.
//
// Twenty where a list of labels had twelve, because the two lists do
// not grow the same way. Labels grow with every new artist, every
// caller and every battery percentage, so a cap on them is a cap on
// how much of somebody fits. Kinds grow only when a device starts
// reporting something it has never reported before, which is a
// handful of times a year.
const Kinds = 20

// Each : The most values shown within one kind.
//
// Enough to show a leaning without becoming the flat list this
// replaced. The count of how many there were in all is given beside
// them, so five of fifty-four does not read as five of five.
const Each = 5

// Arrived, Left : The ends of a visit, as the kinds a phone reports
// them under.
//
// Named here because pairing an arrival with the departure that
// follows it is the only way to say how long somebody stays, and
// nothing in a bare event says which kinds are two ends of the same
// thing. This is the server's own naming for its own events, not a
// guess at what a word means: a device reporting anything else is
// counted but not paired, which is the safe direction.
const Arrived, Left = ".entered", ".exited"

// Briefly : A visit shorter than this is passing through rather than
// going somewhere, and its length says nothing.
const Briefly = 5 * time.Minute

// Lingering : Longer than this with no departure and the departure was
// missed rather than late. A phone that went flat at the office does
// not mean somebody slept there.
const Lingering = 16 * time.Hour

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

	where_, often, after := visits(moments, where), howOften(moments, where, len(days)), whatFollows(moments, where)
	if often == "" && after == "" && where_ == "" {
		return ""
	}

	return prompt.Block(
		prompt.Text(
			fmt.Sprintf("Their devices also reported what they did, over the past four weeks: %d things on %d %s.",
				len(moments), len(days), plural("day", len(days))),
			"These are observations, not things they said. A run of the same thing in quick succession has been counted once.",
		),
		where_,
		often,
		after,
	)
}

// Asking : What the assistant ended up doing for them, counted.
//
// The other half of what somebody asks for. What they typed says it in
// their own words, which vary; what was run in answer says it in a
// fixed vocabulary, so forty diary readings look like forty diary
// readings however each question was phrased.
//
// Named as the thing rather than the tool, because a description
// saying somebody "frequently triggers calendar_events" is written
// about a system and this is meant to be about a person.
func Asking(called []conversation.Message, where *time.Location, domainOf func(string) string) string {
	seen := map[string]int{}
	order := []string{}
	for _, m := range called {
		for _, c := range m.ToolCalls {
			if c.Name == "" || c.Name == tool.DescribeName {
				continue
			}
			name := c.Name
			if domainOf != nil {
				if d := domainOf(c.Name); d != "" {
					name = d
				}
			}
			if _, had := seen[name]; !had {
				order = append(order, name)
			}
			seen[name]++
		}
	}
	if len(order) == 0 {
		return ""
	}

	sort.SliceStable(order, func(i, j int) bool { return seen[order[i]] > seen[order[j]] })
	if len(order) > Mostly {
		order = order[:Mostly]
	}

	return prompt.Block(
		prompt.Text(
			"What they keep coming back to, most first:",
			strings.Join(order, ", ")+".",
		),
		prompt.Text(
			"This is the only thing here with no counts, deliberately.",
			"How many times somebody asked for their diary is a fact about a diary being read, not a fact about them,",
			"and it was being copied into the description as though it were one.",
			"What it is for is the order: it says what they care about. Nothing in it belongs in a list of what is so about them.",
		),
	)
}

// visits : Where they go, and how long they stay.
//
// An arrival paired with the departure that follows it at the same
// place. Counting arrivals alone answers "where do they go" and not
// "when are they there", and the second is the half that lets anything
// be ready before it is asked for.
func visits(moments []moment, where *time.Location) string {
	type stay struct {
		arrivals   []time.Time
		departures []time.Time
		lengths    []time.Duration
		days       map[string]bool
	}
	by := map[string]*stay{}
	order := []string{}
	open := map[string]time.Time{}

	at := func(place string) *stay {
		s, seen := by[place]
		if !seen {
			s = &stay{days: map[string]bool{}}
			by[place], order = s, append(order, place)
		}
		return s
	}

	for _, m := range moments {
		place := m.value
		switch {
		case strings.HasSuffix(m.kind, Arrived) && place != "":
			s := at(place)
			s.arrivals = append(s.arrivals, m.at)
			s.days[m.at.In(where).Format("2006-01-02")] = true
			open[place] = m.at
		case strings.HasSuffix(m.kind, Left) && place != "":
			s := at(place)
			s.departures = append(s.departures, m.at)
			s.days[m.at.In(where).Format("2006-01-02")] = true
			from, waiting := open[place]
			if !waiting {
				continue
			}
			delete(open, place)
			// A visit of seconds is passing the door, and one that ran
			// all night is a departure nobody reported.
			if d := m.at.Sub(from); d >= Briefly && d <= Lingering {
				s.lengths = append(s.lengths, d)
			}
		}
	}
	if len(order) == 0 {
		return ""
	}

	sort.SliceStable(order, func(i, j int) bool { return len(by[order[i]].days) > len(by[order[j]].days) })

	lines := make([]string, 0, len(order))
	for _, place := range order {
		if len(lines) == Mostly {
			break
		}
		s := by[place]
		line := fmt.Sprintf("- %s: arriving or leaving recorded on %d %s",
			place, len(s.days), plural("day", len(s.days)))
		if at := aroundWhen(s.arrivals, where); at != "" && len(s.arrivals) >= Fewest {
			line += ", arriving " + at
		}
		if at := aroundWhen(s.departures, where); at != "" && len(s.departures) >= Fewest {
			line += ", leaving " + at
		}
		if len(s.lengths) >= Fewest {
			line += ", usually staying " + spoken(middle(s.lengths))
		}
		lines = append(lines, line)
	}
	return prompt.Block(
		prompt.Lines(append([]string{"Where they went:"}, lines...)...),
		prompt.Text(
			"These count days a crossing was recorded, which is not the same as days they were there.",
			"A boundary is only noticed when the phone notices it, and one that goes unreported leaves no trace at all: a stay can have an arrival and no departure, a departure and no arrival, or neither.",
			"So a day missing from a count is a day nothing was recorded and never a day they did not go, and the number is a floor rather than a frequency.",
			"Say it as what was seen -- recorded on four days -- and not as how often they do it.",
		),
	)
}

// Usually : What they usually do, as facts a condition can read.
//
// The same arrivals and departures [visits] counts into a sentence,
// given as numbers instead. One function over one set of
// observations, so a rule conditioned on when somebody leaves home
// and a description saying when they leave home cannot disagree --
// which two separate implementations of the same arithmetic would,
// the first time one of them was edited.
//
// Only the places whose hours actually agree. A place somebody
// goes at no particular hour has no fact here rather than a fact
// with a low number on it: a condition is a thing that fires, and
// one standing on three observations scattered across a day would
// fire at a time nobody keeps.
func Usually(all []event.Event, where *time.Location) []routine.Fact {
	if where == nil {
		where = time.UTC
	}
	at := time.Time{}
	for _, e := range all {
		if e.ReceivedAt.After(at) {
			at = e.ReceivedAt
		}
	}

	type ends struct {
		arrivals   []time.Time
		departures []time.Time
		days       map[string]bool
	}
	by := map[string]*ends{}
	order := []string{}

	// Collapsed, as the sentence does it: a phone that reports the
	// same crossing three times in a minute reported one crossing.
	for _, m := range collapsed(all, where) {
		place := m.value
		if place == "" {
			continue
		}
		arriving := strings.HasSuffix(m.kind, Arrived)
		leaving := strings.HasSuffix(m.kind, Left)
		if !arriving && !leaving {
			continue
		}
		e, seen := by[place]
		if !seen {
			e = &ends{days: map[string]bool{}}
			by[place], order = e, append(order, place)
		}
		e.days[m.at.In(where).Format("2006-01-02")] = true
		if arriving {
			e.arrivals = append(e.arrivals, m.at)
		} else {
			e.departures = append(e.departures, m.at)
		}
	}

	out := make([]routine.Fact, 0, len(order)*2)
	for _, place := range order {
		e := by[place]
		for _, side := range []struct {
			what routine.What
			when []time.Time
		}{
			{routine.Arrives, e.arrivals},
			{routine.Leaves, e.departures},
		} {
			hour, most, of, ok := usualHour(side.when, where)
			if !ok {
				continue
			}
			out = append(out, routine.Fact{
				Subject: place, What: side.what,
				Hour: hour, Days: most, Over: of, At: at,
			})
		}
	}
	return out
}

// moment : One thing happening, once.
type moment struct {
	kind  string
	value string
	at    time.Time
	// reading : Its value is a measurement rather than what the
	// thing is, so the value does not say which thing happened.
	reading bool
}

// label : The thing itself, however it is named.
//
// A reading is its kind alone. The owner's decision that a varying
// number is never a value is right and this cannot rely on it:
// battery.level arrives with a percentage in the value, and counting
// kind-and-value made a phone discharging into a hundred separate
// things somebody does. Eight of the twelve lines here were battery
// percentages and nine of the twelve in [whatFollows] were one
// battery falling, stated as "battery.level 38, then battery.level
// 37" and eleven near-identical others.
func (m moment) label() string {
	if m.value == "" || m.reading {
		return m.kind
	}
	return m.kind + " " + strconv.Quote(m.value)
}

// readings : The kinds whose every value is a number.
//
// Per kind and never per value, which is the difference between
// folding a battery percentage and losing a caller. One call came
// in from a number nobody had named, and "+916746580844" parses as
// a number perfectly well; it stays a thing of its own because the
// other callers that kind has seen are people with names.
func readings(all []moment) map[string]bool {
	num := map[string]bool{}
	for _, m := range all {
		if m.value == "" {
			continue
		}
		if _, seen := num[m.kind]; !seen {
			num[m.kind] = true
		}
		if _, err := strconv.ParseFloat(m.value, 64); err != nil {
			num[m.kind] = false
		}
	}
	return num
}

// shown : The same, as it should be read.
//
// Counting keys on label, which keeps a stranger's number so that one
// stranger can be told from another. This is what reaches the prompt,
// and a description that is read before every answer has no business
// carrying somebody's telephone number about with it.
func (m moment) shown() string {
	if m.value == "" || m.reading {
		return m.kind
	}
	return m.kind + " " + strconv.Quote(event.Readable(m.value))
}

// collapsed : The events in order, with runs of the same thing reduced
// to their first occurrence.
func collapsed(events []event.Event, where *time.Location) []moment {
	in := make([]moment, 0, len(events))
	for _, e := range events {
		in = append(in, moment{kind: e.Kind, value: valueOf(e.Payload), at: e.OccurredAt})
	}
	is := readings(in)
	for i := range in {
		in[i].reading = is[in[i].kind]
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

// howOften : What they do, most often first -- by sort of thing,
// with what it was about underneath.
//
// It ranked labels, where a label is the kind and the value
// together, and cut at twelve. That hid everything about the person
// and kept everything about the hardware, for a reason that is
// arithmetic rather than judgement: a kind with one value
// concentrates all its count on one line and a kind with many
// disperses across all of them. Measured over four weeks, with the
// cut at twelve:
//
//   - battery.level: 1,230 moments over a hundred percentages,
//     every one of them between nineteen and twenty, taking eight
//     of the twelve lines to say a phone charges and discharges.
//   - music.state: 97 moments over fifty-four artists, the busiest
//     of them nine. Not one line.
//   - call.received: 24 moments over ten people -- Amma seven,
//     Alekhya six. Not one line.
//
// Taste is by nature many distinct values, and so is who somebody
// rings. No cap could have surfaced either, because raising it buys
// more battery percentages before it buys an artist.
//
// So the kind is the line and the values sit inside it. A kind is
// something a device reports, which is a short and slow-growing
// list; the values under it are where a person is.
func howOften(moments []moment, where *time.Location, days int) string {
	type tally struct {
		reading bool
		times   int
		days    map[string]bool
		when    []time.Time
		values  map[string]int
		order   []string
		numbers []float64
	}
	by := map[string]*tally{}
	order := []string{}
	for _, m := range moments {
		t, seen := by[m.kind]
		if !seen {
			t = &tally{reading: m.reading, days: map[string]bool{}, values: map[string]int{}}
			by[m.kind], order = t, append(order, m.kind)
		}
		t.times++
		t.days[m.at.In(where).Format("2006-01-02")] = true
		t.when = append(t.when, m.at)

		switch {
		case m.value == "":
		case m.reading:
			if n, err := strconv.ParseFloat(m.value, 64); err == nil {
				t.numbers = append(t.numbers, n)
			}
		default:
			// As it should be read and not as it is counted: a
			// description carried into every answer has no
			// business holding somebody's telephone number.
			said := event.Readable(m.value)
			if _, had := t.values[said]; !had {
				t.order = append(t.order, said)
			}
			t.values[said]++
		}
	}

	sort.SliceStable(order, func(i, j int) bool { return by[order[i]].times > by[order[j]].times })

	lines := make([]string, 0, len(order))
	for _, kind := range order {
		if len(lines) == Kinds {
			break
		}
		t := by[kind]
		line := fmt.Sprintf("- %s: %d %s, on %d of %d %s",
			kind, t.times, plural("time", t.times), len(t.days), days, plural("day", days))
		if at := aroundWhen(t.when, where); at != "" {
			line += ", " + at
		}
		switch {
		case t.reading && len(t.numbers) > 0:
			sort.Float64s(t.numbers)
			line += fmt.Sprintf("; readings from %s to %s",
				figure(t.numbers[0]), figure(t.numbers[len(t.numbers)-1]))
		case len(t.order) == 1:
			// Kept rather than left off as part of what the kind
			// is. A person whose calls all come from one person
			// is the single most worth knowing thing that kind
			// has to say, and a kind whose sender names nothing
			// says `always "unknown"`, which is true and is
			// about the sender.
			line += "; always " + strconv.Quote(t.order[0])
		case len(t.order) > 1:
			sort.SliceStable(t.order, func(i, j int) bool {
				return t.values[t.order[i]] > t.values[t.order[j]]
			})
			most := t.order
			if len(most) > Each {
				most = most[:Each]
			}
			named := make([]string, 0, len(most))
			for _, v := range most {
				named = append(named, fmt.Sprintf("%s (%d)", strconv.Quote(v), t.values[v]))
			}
			line += fmt.Sprintf("; %d different, most often %s",
				len(t.order), strings.Join(named, ", "))
		}
		lines = append(lines, line)
	}
	return prompt.Block(
		prompt.Lines(append([]string{"How often, most to least:"}, lines...)...),
		prompt.Text(
			"The number after each is how many times, and the ones named inside a kind are the most frequent of however many there were -- five of fifty-four artists is what they listen to most, not everything they listen to.",
			"A kind whose value is a measurement is given as a range instead, because which reading it was does not say what happened.",
		),
	)
}

// figure : A number as it was reported, without a decimal point it
// did not have.
func figure(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// whatFollows : What tends to come shortly after what.
//
// Everything within Soon of each other, not only what came next. It
// was adjacency until 2 October 2026, which only found a pairing when
// nothing else happened in between -- and the useful ones are exactly
// the opposite, two devices reporting one real act with other traffic
// between them.
func whatFollows(moments []moment, where *time.Location) string {
	type pair struct {
		shown string
		times int
		gaps  []time.Duration
	}
	by := map[string]*pair{}
	order := []string{}
	for i := range moments {
		for j := i + 1; j < len(moments) && j-i <= Reach; j++ {
			before, after := moments[i], moments[j]
			gap := after.at.Sub(before.at)
			// Ordered by time, so the first one out of reach ends the
			// window rather than only being skipped.
			if gap > Soon {
				break
			}
			if before.label() == after.label() {
				continue
			}
			key := before.label() + ", then " + after.label()
			p, seen := by[key]
			if !seen {
				p = &pair{shown: before.shown() + ", then " + after.shown()}
				by[key], order = p, append(order, key)
			}
			p.times++
			p.gaps = append(p.gaps, gap)
		}
	}

	sort.SliceStable(order, func(i, j int) bool { return by[order[i]].times > by[order[j]].times })

	lines := make([]string, 0, len(order))
	for _, key := range order {
		if by[key].times < Fewest || len(lines) == Mostly {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %d %s, usually within %s",
			by[key].shown, by[key].times, plural("time", by[key].times), spoken(middle(by[key].gaps))))
	}
	if len(lines) == 0 {
		return ""
	}
	return prompt.Lines(append([]string{"Things that happen together, in the order they happened:"}, lines...)...)
}

// Agreeing : The fewest observations in a band that can be a time
// of day.
//
// Two of anything is a coincidence. It is the number the same
// judgement uses when deciding whether something has changed.
const Agreeing = 3

// Band : How many hours wide a time of day may be and still be one
// time of day.
//
// Four, counted inclusively: something at nine and something at
// noon are the same part of the morning, and something at nine and
// something at two is not.
const Band = 3

// aroundWhen : Roughly what time of day, when there is one.
//
// Nothing at all when the hours are spread: "usually around 2pm" for
// something that happens at any hour is the kind of detail that gets
// written into a description and then believed.
//
// The densest band rather than the whole range, which is what this
// tested until 7 October 2026 and why it almost never found an
// hour. Coming home was 2, 10, 11, 21, 21, 21, 21 -- four of the
// seven at nine in the evening, and the test asked whether the
// furthest two were within four hours of each other. They were
// nineteen apart, so the answer was "at no particular hour" and a
// plain daily habit was invisible. Reaching the office was 11, 11,
// 11, 17, 18 and went the same way.
//
// [middle] in this file already says the principle, about gaps: the
// median is what a single long one cannot drag about. One late
// night should not be able to say somebody has no bedtime.
//
// Round, because the hours are. A band may run from eleven at night
// to two in the morning, which is where a bedtime lives.
func aroundWhen(when []time.Time, where *time.Location) string {
	at, most, of, ok := usualHour(when, where)
	if of == 0 {
		return ""
	}
	if !ok {
		return "at no particular hour"
	}

	// How much of it, in a word. Two thirds is a habit; half is a
	// leaning, and saying "usually" of it would be the overstating
	// this function exists to avoid.
	how := "often around "
	if most*3 >= of*2 {
		how = "usually around "
	}
	return how + oclock(at)
}

// usualHour : The hour somebody does a thing, how many of the
// observations agree on it, and how many there were.
//
// Split out of [aroundWhen] so the same answer can be stored as a
// number and not only said in a sentence. A rule conditioned on
// when somebody usually leaves cannot read "usually around 10am",
// and two ways of working out the hour would agree until one of
// them was edited.
//
// ok is false when the observations do not agree on an hour at
// all; of is zero when there are too few to ask.
func usualHour(when []time.Time, where *time.Location) (at, most, of int, ok bool) {
	if len(when) < Fewest {
		return 0, 0, 0, false
	}
	hours := make([]int, 0, len(when))
	for _, t := range when {
		hours = append(hours, t.In(where).Hour())
	}

	from, most := densest(hours)
	// Fewer than half of them is not a time of day, it is some of
	// them happening to agree.
	//
	// And a half of something tiny is not a time of day either.
	// Office arrivals were 19, 11 and 13: two of the three fall in
	// one band, which passes the half, and the band's middle is
	// one in the afternoon -- a confident hour out of two
	// observations two and a quarter hours apart. Three is the
	// fewest that can be a habit rather than a coincidence with a
	// witness, which is what [changed.Enough] says about the same
	// question elsewhere.
	if most < Agreeing || most*2 < len(hours) {
		return 0, most, len(hours), false
	}

	within := make([]int, 0, most)
	for _, h := range hours {
		if (h-from+24)%24 <= Band {
			within = append(within, (h-from+24)%24)
		}
	}
	sort.Ints(within)
	return (from + within[len(within)/2]) % 24, most, len(hours), true
}

// densest : The start of the fullest band of hours, and how many
// are in it.
//
// Every hour is tried as a start, which is twenty four passes over
// a handful of numbers and not worth being cleverer about.
func densest(hours []int) (from, most int) {
	for start := 0; start < 24; start++ {
		n := 0
		for _, h := range hours {
			if (h-start+24)%24 <= Band {
				n++
			}
		}
		if n > most {
			from, most = start, n
		}
	}
	return from, most
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

// valueOf : What an event is about, which is its value and nothing
// else.
//
// Everything here is counted by kind and value together, so the value
// has to be the part that repeats: the person who rang, the network
// joined, the place stayed in. A payload rendered whole is a label
// that is different every time, and a label that is different every
// time is counted once, paired with nothing, and reported as a thing
// that happened rather than a thing they do.
//
// This did render the whole payload, and the moment an event arrived
// carrying anything besides a value it broke -- a call with its length
// in seconds became "seconds=140 value=Amit", a different thing from
// "seconds=209 value=Amit", and a man who rings most mornings read as
// six unrelated events. A stay carrying its minutes had the same
// fault, and its minutes change every five of them as it grows.
//
// It is the third place this mistake has been made in a day. The
// events tool and the greeting both key on the value alone for the
// same reason, and this did not.
func valueOf(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil || into.Value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(into.Value))
}
