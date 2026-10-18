// Package doubting weighs whether an answer is worth a second look.
//
// When the model answers in prose and tools were on offer, the runner
// sends that answer back and asks it to look first. It is the guard that
// stops an answer about his diary, his laptop or the world being given
// from memory, and it works: it has caught a hundred answers that should
// have read something.
//
// It is also the most expensive thing in a turn. Every send-back is a
// whole round through the gateway -- a median of 4.5 seconds, of which
// 2.9 is spent before the first word arrives -- and across 642 of them
// recorded since 26 September 2026, 542 came back having changed
// nothing. Eighty-four per cent of the cost buys nothing, and about
// forty minutes of waiting has been spent on second thoughts that
// arrived at the same answer.
//
// So this package is not the guard. It is the question of whether the
// guard is owed on this turn, asked somewhere cheaper than a round: a
// judge that says look or do not look, scored against what the real
// send-back actually found.
//
// Nothing here calls a model. A judge may; the scoring does not care.
package doubting

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Case : One answer that was sent back, and what came of it.
//
// Built from the record a chat already keeps of every request it made,
// so the whole of the past is evidence without anything being collected
// specially.
type Case struct {
	// ChatID : Which turn this was, for chasing one back.
	ChatID string
	// Prompt : What the person said.
	Prompt string
	// Draft : The answer that was thrown away.
	//
	// Empty for every case recorded before drafts were kept, which is
	// all of them up to 18 October 2026. A judge that needs it says so
	// by refusing a case without one; one that works from the question
	// alone is measurable on the whole history.
	Draft string
	// Channel : Spoken or typed, since what is said aloud is shorter.
	Channel string
	// At : When, for splitting a set by date rather than at random.
	At time.Time
	// Looked : What the send-back found. True where the round that
	// followed asked for tools, which is the only evidence that the
	// doubt was owed.
	Looked bool
}

// Judge : Something that decides whether an answer is worth a second
// look.
type Judge interface {
	// Name : What to call it in a table.
	Name() string
	// Doubt : Whether to send this answer back.
	Doubt(ctx context.Context, c Case) (bool, error)
}

// Always, Never : The two ends, and the baselines every judge is read
// against.
//
// Always is what the runner does today: every prose answer goes back,
// so every doubt is caught and every needless one is paid for. Never is
// the other end, free and wrong a hundred times. A judge is only worth
// having between them.
type Always struct{}

func (Always) Name() string                              { return "always (today)" }
func (Always) Doubt(context.Context, Case) (bool, error) { return true, nil }

type Never struct{}

func (Never) Name() string                              { return "never" }
func (Never) Doubt(context.Context, Case) (bool, error) { return false, nil }

// Scored : How a judge did.
//
// Four outcomes and not two, because the two mistakes cost entirely
// different things. A needless round costs four and a half seconds. A
// missed one costs an answer given from memory about something that
// changes -- which is the whole reason the send-back was built.
type Scored struct {
	// Judge : Whose score this is.
	Judge string
	// Cases : How many it was shown.
	Cases int
	// Caught : Doubted, and the look found a tool. Right, and the point.
	Caught int
	// Missed : Not doubted, and a tool was owed. Wrong, and the costly
	// kind.
	Missed int
	// Wasted : Doubted, and the look found nothing. A round for nothing.
	Wasted int
	// Spared : Not doubted, and there was nothing to find. Right, and
	// the saving.
	Spared int
	// Each : The median time the judge itself took on one case.
	Each time.Duration
	// Failed : Cases it could not judge at all, which are counted as
	// doubted: a judge that cannot answer must not be the reason a look
	// was skipped.
	Failed int
}

// Kept : The share of real doubts it still catches, 0 to 1.
//
// The number to hold. Every point given up here is an answer about his
// own things, or about the world, given from memory.
func (s Scored) Kept() float64 {
	if s.Caught+s.Missed == 0 {
		return 1
	}
	return float64(s.Caught) / float64(s.Caught+s.Missed)
}

// Cut : The share of needless rounds it removes, 0 to 1.
func (s Scored) Cut() float64 {
	if s.Spared+s.Wasted == 0 {
		return 0
	}
	return float64(s.Spared) / float64(s.Spared+s.Wasted)
}

// Saved : How much waiting it takes off, at a given price for a round.
func (s Scored) Saved(round time.Duration) time.Duration {
	return time.Duration(s.Spared) * round
}

// String : One line of a table.
func (s Scored) String() string {
	return fmt.Sprintf("%-22s  caught %3d/%3d (%3.0f%%)  needless rounds cut %3d/%3d (%3.0f%%)  saves %6s  judged in %v",
		s.Judge, s.Caught, s.Caught+s.Missed, 100*s.Kept(),
		s.Spared, s.Spared+s.Wasted, 100*s.Cut(),
		s.Saved(Round).Round(time.Second), s.Each.Round(time.Millisecond))
}

// Round : What one needless send-back actually costs.
//
// A tenth of a second, and this number is the whole finding. A round
// through the gateway is 4.5 seconds, so a second look looked like the
// most expensive thing in a turn -- 542 of them wasted since 26
// September, about forty minutes of waiting. It is not. The runner
// starts the look-first round speculatively, while the first answer is
// still being written: 628 of 642 send-backs reused a request that was
// already in flight, so what they added was only whatever was left of
// it when the first answer arrived.
//
// Measured over all 542: a median of 0.11s, a p90 of 0.92s, and 3.6
// minutes in total across three weeks. The ones that caught a tool cost
// 0.32s.
//
// Kept here so that nothing built on top of this package can state a
// saving in rounds, which nobody waits in, or in a round's price, which
// this one does not have.
const Round = 110 * time.Millisecond

// Score : Runs a judge over every case and counts what it did.
//
// A judge that errors on a case is counted as having doubted it, and
// the error recorded in Failed. Failing closed is the only safe
// direction: the cost of a needless round is seconds, and the cost of a
// missed one is an answer that was made up.
func Score(ctx context.Context, j Judge, cases []Case) (Scored, error) {
	s := Scored{Judge: j.Name(), Cases: len(cases)}
	took := make([]time.Duration, 0, len(cases))
	for _, c := range cases {
		began := time.Now()
		doubt, err := j.Doubt(ctx, c)
		took = append(took, time.Since(began))
		if err != nil {
			s.Failed++
			doubt = true
		}
		switch {
		case doubt && c.Looked:
			s.Caught++
		case doubt && !c.Looked:
			s.Wasted++
		case !doubt && c.Looked:
			s.Missed++
		default:
			s.Spared++
		}
		if err := ctx.Err(); err != nil {
			return s, err
		}
	}
	sort.Slice(took, func(i, k int) bool { return took[i] < took[k] })
	if len(took) > 0 {
		s.Each = took[len(took)/2]
	}
	return s, nil
}

// Only : The cases a judge can actually be read on, since one that
// needs the thrown-away answer cannot be scored on a case recorded
// before those were kept.
func Only(cases []Case, withDraft bool) []Case {
	out := make([]Case, 0, len(cases))
	for _, c := range cases {
		if withDraft && strings.TrimSpace(c.Draft) == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}
