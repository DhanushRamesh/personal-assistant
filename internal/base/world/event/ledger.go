package event

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Twice : How close together two identical reports of one action are
// counted as the same report sent again rather than two things that
// happened.
const Twice = 2 * time.Second

// Same : Whether two payloads say the same thing, whatever order or
// spacing they were written in.
func Same(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

// Again : Whether b is a sent again -- the same reporter, the same
// action and the same payload as a, within Twice of it.
func Again(a, b *Event) bool {
	if a.Source != b.Source || a.Device != b.Device || a.Kind != b.Kind {
		return false
	}
	gap := b.OccurredAt.Sub(a.OccurredAt)
	if gap < 0 {
		gap = -gap
	}
	return gap < Twice && Same(a.Payload, b.Payload)
}

// Line : One reporter's one action over a window -- how much of it,
// when, and how cleanly it was kept.
//
// A reporter is a source and a device: the phone's Tasker, the laptop,
// Home Assistant. An action is the kind it sends, such as
// place.entered or bluetooth.disconnected.
type Line struct {
	Source string
	Device string
	Kind   string
	// Count : How many arrived in the window.
	Count int64
	// First, Last : When the earliest and latest of them were received.
	First time.Time
	Last  time.Time
	// Unknown : How many carried the value "unknown".
	Unknown int64
	// Valueless : How many carried no value at all.
	Valueless int64
	// Repeats : How many were the same report as the one before it,
	// within Twice -- sent again rather than happening again.
	Repeats int64
	// Longest : The longest it went without one, between two that
	// arrived in the window.
	Longest time.Duration
	// Distinct : How many different values it carried.
	Distinct int64
	// Only : The value, when there was exactly one.
	Only string
}

// Every : The usual time between two of these, from how many arrived
// across the span they arrived in. Zero for fewer than two.
func (l Line) Every() time.Duration {
	if l.Count < 2 {
		return 0
	}
	return l.Last.Sub(l.First) / time.Duration(l.Count-1)
}

// Valued : How many of one reporter's one action carried one value.
type Valued struct {
	Source string
	Device string
	Kind   string
	Value  string
	Count  int64
}

// Pair : Two actions of one family that should come in step, counted
// for one value: entered and exited for "office", connected and
// disconnected for "bike helmet".
type Pair struct {
	// Source, Device : The reporter both actions come from.
	Source string
	Device string
	// Family : The part of the kind before the dot.
	Family string
	// A, B : The two actions, in name order, with how many of each.
	A, B   string
	NA, NB int64
	// Value : What they were about. Empty for the family as a whole.
	Value string
}

// Off : Whether the two sides disagree by more than ordinary edges of a
// window allow: one either way is a pair the window cut in half.
func (p Pair) Off() bool {
	d := p.NA - p.NB
	if d < 0 {
		d = -d
	}
	return d > 1
}

// Reporter : Who sent a line -- a source and a device -- and the family
// of what they sent.
type Reporter struct{ Source, Device, Family string }

// Paired : Each reporter's families with exactly two actions -- the ones
// that describe a thing starting and stopping -- out of the kinds in
// lines.
//
// Read from the data rather than listed. A family of one has nothing to
// balance against; a family of several (power.charging, .discharging,
// .full, .not_charging) is a state, not a pair, and is left alone. Per
// reporter, because the laptop's wifi.joined and the phone's are two
// different networks coming and going.
func Paired(lines []Line) map[Reporter][2]string {
	by := map[Reporter]map[string]bool{}
	for _, l := range lines {
		family, _, ok := strings.Cut(l.Kind, ".")
		if !ok {
			continue
		}
		r := Reporter{l.Source, l.Device, family}
		if by[r] == nil {
			by[r] = map[string]bool{}
		}
		by[r][l.Kind] = true
	}
	out := map[Reporter][2]string{}
	for r, kinds := range by {
		if len(kinds) != 2 {
			continue
		}
		two := make([]string, 0, 2)
		for k := range kinds {
			two = append(two, k)
		}
		sort.Strings(two)
		out[r] = [2]string{two[0], two[1]}
	}
	return out
}

// Pairs : Each paired family counted side by side, per value, from the
// per-value counts of its two actions. Values seen on one side only are
// counted against nothing on the other, which is exactly the fault this
// exists to show -- provided the two sides are mostly about the same
// values, which is what makes them a pair at all.
func Pairs(paired map[Reporter][2]string, values []Valued) []Pair {
	type key struct {
		r     Reporter
		value string
	}
	by := map[key]*Pair{}
	for _, v := range values {
		family, _, _ := strings.Cut(v.Kind, ".")
		r := Reporter{v.Source, v.Device, family}
		two, ok := paired[r]
		if !ok || (v.Kind != two[0] && v.Kind != two[1]) {
			continue
		}
		k := key{r, v.Value}
		p := by[k]
		if p == nil {
			p = &Pair{Source: r.Source, Device: r.Device, Family: family, A: two[0], B: two[1], Value: v.Value}
			by[k] = p
		}
		if v.Kind == two[0] {
			p.NA += v.Count
		} else {
			p.NB += v.Count
		}
	}
	// A pair is two actions about the same things: entered and exited
	// the same places, connected and disconnected the same devices. Two
	// actions that share a family and nothing else -- a battery level and
	// the battery being full -- are not, and counting one against the
	// other would call every reading a fault.
	shared := map[Reporter][2]int64{}
	total := map[Reporter][2]int64{}
	for k, p := range by {
		t := total[k.r]
		t[0] += p.NA
		t[1] += p.NB
		total[k.r] = t
		if p.NA > 0 && p.NB > 0 {
			sh := shared[k.r]
			sh[0] += p.NA
			sh[1] += p.NB
			shared[k.r] = sh
		}
	}
	out := make([]Pair, 0, len(by))
	for k, p := range by {
		t, sh := total[k.r], shared[k.r]
		if t[0] == 0 || t[1] == 0 || 2*sh[0] < t[0] || 2*sh[1] < t[1] {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Source+a.Device+a.Family != b.Source+b.Device+b.Family {
			return a.Source+a.Device+a.Family < b.Source+b.Device+b.Family
		}
		return a.Value < b.Value
	})
	return out
}

// Ledgers : The per-reporter accounts of what was sent, as a store keeps
// them.
type Ledgers interface {
	// Ledger : Each reporter's each action since a moment.
	Ledger(ctx context.Context, userID string, since time.Time) ([]Line, error)
	// Values : How many of each of the given kinds carried each value.
	Values(ctx context.Context, userID string, since time.Time, kinds []string) ([]Valued, error)
}

// LedgerDays, LedgerMostDays : How far back the ledger looks by default,
// and at most.
const (
	LedgerDays     = 7
	LedgerMostDays = 30
)

// Steady : How many of an action in the window before what it never does
// -- change its value, go this quiet -- says anything about it.
const Steady = 20

// Frequent : How often an action has to come, at least, for its silence to
// be a fault: often enough that it is reporting by itself and not because
// somebody did something.
const Frequent = 30 * time.Minute

// Fault : One way a reporter's books are wrong, as the ledger says it.
type Fault struct {
	Source, Device, Kind string
	Says                 string
}

// Faults : What is wrong in a ledger.
//
// Four things, and nothing here knows what any action means: a pair
// whose two sides do not come in step, the same report sent twice, an
// action whose value never changes or is never there, and a steady
// reporter silent for longer than it ever is.
func Faults(lines []Line, pairs []Pair, now time.Time, where *time.Location) []Fault {
	var out []Fault
	for _, p := range pairs {
		if !p.Off() {
			continue
		}
		about := p.Value
		if about == "" {
			about = "everything"
		}
		out = append(out, Fault{Source: p.Source, Device: p.Device, Kind: p.Family,
			Says: fmt.Sprintf("%s %d against %s %d for %q -- one side is missing reports",
				p.A, p.NA, p.B, p.NB, about)})
	}
	for _, l := range lines {
		if l.Repeats > 0 {
			out = append(out, Fault{Source: l.Source, Device: l.Device, Kind: l.Kind,
				Says: fmt.Sprintf("%d of %d were the same report sent again within %s", l.Repeats, l.Count, Twice)})
		}
		if l.Count >= Steady && l.Distinct == 1 {
			out = append(out, Fault{Source: l.Source, Device: l.Device, Kind: l.Kind,
				Says: fmt.Sprintf("all %d say %q -- a value that never changes is no value", l.Count, l.Only)})
		}
		if l.Count >= 5 && l.Valueless == l.Count {
			out = append(out, Fault{Source: l.Source, Device: l.Device, Kind: l.Kind,
				Says: fmt.Sprintf("none of %d carries a value", l.Count)})
		}
		// Silent for longer than it ever was in the window, and only for
		// what reports by itself every few minutes. Music stops for a day
		// when nobody plays any, which is a choice and not a fault; a
		// position that has never gone an hour without a fix has stopped.
		if quiet := now.Sub(l.Last); l.Count >= Steady && l.Every() > 0 && l.Every() <= Frequent &&
			quiet > time.Hour && quiet > l.Longest {
			out = append(out, Fault{Source: l.Source, Device: l.Device, Kind: l.Kind,
				Says: fmt.Sprintf("nothing since %s, longer than it has gone quiet all week (%s at most)",
					l.Last.In(where).Format("2 Jan 15:04"), Roughly(l.Longest))})
		}
	}
	return out
}

// Roughly : A gap as a person would say it.
func Roughly(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()+0.5))
	default:
		return fmt.Sprintf("%.1f h", d.Hours())
	}
}

// Ledgered : The whole ledger since a moment -- lines, pairs and faults --
// read from a store.
func Ledgered(ctx context.Context, books Ledgers, userID string, since, now time.Time, where *time.Location) ([]Line, []Pair, []Fault, error) {
	lines, err := books.Ledger(ctx, userID, since)
	if err != nil {
		return nil, nil, nil, err
	}
	paired := Paired(lines)
	var kinds []string
	for _, two := range paired {
		kinds = append(kinds, two[0], two[1])
	}
	values, err := books.Values(ctx, userID, since, kinds)
	if err != nil {
		return nil, nil, nil, err
	}
	pairs := Pairs(paired, values)
	return lines, pairs, Faults(lines, pairs, now, where), nil
}
