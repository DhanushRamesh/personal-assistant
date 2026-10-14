package event

import (
	"sort"
	"time"
)

// Retired, Silence, Enough : When a kind counts as having stopped
// rather than merely being quiet.
//
// Measured on the real table the day seven dead kinds were found. Each
// kind's own usual gap is what it is judged against, because the kinds
// differ by four orders of magnitude -- music.playing came every four
// minutes, call.unanswered every four days -- and one number in hours
// could not serve both.
//
// The separation is not close. Across every kind in the table, judged
// at the same moment:
//
//	the quietest living kind   wifi.left          2.82x its usual gap
//	the liveliest dead kind    bluetooth.changed   469x
//
// Twenty sits in a 166-fold gap between them. Silence stops a
// chatty kind being called dead after an afternoon, and Enough refuses
// to guess a usual gap from fewer than two intervals.
const (
	Retired = 20
	Silence = 72 * time.Hour
	Enough  = 3
)

// Stopped : A kind that reported for a while and then did not.
//
// Not the same as a kind that is quiet. Every kind is quiet most of
// the time; this is one that has been quiet for far longer than it
// ever used to be, judged against its own habit.
type Stopped struct {
	// Kind : The dotted name that has gone silent.
	Kind string
	// Count, Last : How many it ever sent, and when the last arrived.
	Count int64
	Last  time.Time
	// Silent : How long since that last one.
	Silent time.Duration
	// Usual : The gap it used to keep between reports.
	Usual time.Duration
}

// Times : How many of its own usual gaps it has now been silent for.
func (s Stopped) Times() float64 {
	if s.Usual <= 0 {
		return 0
	}
	return float64(s.Silent) / float64(s.Usual)
}

// Quit : The kinds that have stopped reporting.
//
// Written because nothing looked for an absence. The ledger reports on
// what arrived, so a kind that stopped entirely produced no line at
// all and was invisible: seven of them sat in the table for eleven
// days, and in the prompt, because event_kinds is built from the data
// and prefetched into every turn. A list meant to stop a model
// guessing was teaching it seven things that were no longer true.
//
// A kind with too few reports to have a habit is left alone rather
// than guessed at. Two reports give one interval, which is not a
// habit, and judging a kind wrongly dead is worse than missing one:
// the answer to this is to stop collecting something.
func Quit(kinds []Kind, now time.Time) []Stopped {
	var out []Stopped
	for _, k := range kinds {
		if k.Count < Enough || k.Last.IsZero() || k.First.IsZero() {
			continue
		}
		silent := now.Sub(k.Last)
		if silent < Silence {
			continue
		}
		usual := k.Last.Sub(k.First) / time.Duration(k.Count-1)
		if usual <= 0 {
			continue
		}
		s := Stopped{Kind: k.Kind, Count: k.Count, Last: k.Last, Silent: silent, Usual: usual}
		if s.Times() < Retired {
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Times() > out[j].Times() })
	return out
}
