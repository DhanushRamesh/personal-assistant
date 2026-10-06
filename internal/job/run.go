package job

import (
	"context"
	"log/slog"
	"time"
)

// Tick : How often the runner looks for work.
//
// Close to the reminder loop, because a job saying something at
// eight should say it at eight and not at five past.
const Tick = 2 * time.Second

// Limit : How many jobs are taken in one look.
const Limit = 50

// Store : Where jobs are kept.
type Store interface {
	// Due : Jobs whose time has come, soonest first.
	Due(ctx context.Context, at time.Time, limit int) ([]Job, error)
	// Ran : Records that a job ran, when it next runs, and what
	// went wrong if anything.
	Ran(ctx context.Context, id string, next time.Time, state State, why string, at time.Time) error
}

// Outcome : What carrying out one step came to.
type Outcome struct {
	// Stop : Whether the rest of the sequence should not run.
	Stop bool
	// Why : Why it stopped, for the record. Not a failure -- a
	// check that did not pass is the engine working.
	Why string
}

// Doer : Carries out one step.
//
// One interface rather than one per task, so that adding a kind of
// task is a case in the server's own switch and not a change here.
// The runner's business is order, lateness and failure; what a step
// actually does is somebody else's.
type Doer interface {
	// Do : Carries out one step for one person. Returns a stop when
	// the rest of the sequence should not run.
	//
	// Stop is not failure. It is how a sequence says *only if*: a
	// check step whose condition does not hold. That is the whole
	// of the conditional in this engine, and deliberately the whole
	// of it.
	//
	// already names the tools earlier steps have run. A sequence
	// is one turn, not several: the tools refuse to write
	// something they have not read in this turn, and a sequence
	// that forgot its own first step could never satisfy that. It
	// is also what makes [list, add] mean anything, which is the
	// reason sequences exist.
	Do(ctx context.Context, userID string, s Step, already []string) (Outcome, error)
}

// Runner : Runs jobs when their time comes.
type Runner struct {
	// Store : Where jobs are kept. Required.
	Store Store
	// Doer : What carries out a step. Required.
	Doer Doer
	// Where : The person's zone, for a schedule written as a clock
	// time. Nil is UTC.
	Where *time.Location
	// Tick : How often to look. Zero selects Tick.
	Tick time.Duration
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger
}

// Pass : What one look did.
type Pass struct {
	Ran     int
	Stopped int
	Failed  int
	Skipped int
}

// Run : Runs jobs until the context ends.
func (r *Runner) Run(ctx context.Context) {
	if r == nil || r.Store == nil || r.Doer == nil {
		return
	}
	every := r.Tick
	if every <= 0 {
		every = Tick
	}
	tick := time.NewTicker(every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.Once(ctx)
		}
	}
}

// Once : One look for work.
func (r *Runner) Once(ctx context.Context) Pass {
	var pass Pass
	if r == nil || r.Store == nil || r.Doer == nil {
		return pass
	}

	now := r.clock()
	due, err := r.Store.Due(ctx, now, Limit)
	if err != nil {
		r.log("cannot read which jobs are due", err)
		return pass
	}
	for _, j := range due {
		r.one(ctx, j, now, &pass)
	}
	return pass
}

// one : Runs a single job's sequence.
func (r *Runner) one(ctx context.Context, j Job, now time.Time, pass *Pass) {
	// Too late to be worth doing. The server was off over the
	// moment, and yesterday's eight o'clock done at noon is worse
	// than not done at all.
	if now.Sub(j.Next) > Stale {
		pass.Skipped++
		r.note("a job's moment passed while nothing was running", j, "")
		r.settle(ctx, j, now, "its moment passed before anything ran")
		return
	}

	// What went wrong, if anything did. Collected rather than
	// recorded as it happens, so that one run leaves one record
	// however many of its steps failed.
	var why string
	var already []string
	for _, step := range j.Steps {
		done, err := r.Doer.Do(ctx, j.UserID, step, already)
		if step.Do == Call && step.Tool != "" {
			already = append(already, step.Tool)
		}
		if err != nil {
			// The rest still runs. A phone alarm that could not be
			// set must not swallow the words that go with it,
			// which is the rule the reminder and its alarm already
			// follow.
			pass.Failed++
			why = err.Error()
			r.note("a step failed and the sequence went on", j, why)
			continue
		}
		if done.Stop {
			pass.Stopped++
			// A failure earlier in the sequence is the more
			// important thing to have recorded, so it keeps the
			// field.
			if why == "" {
				why = done.Why
			}
			break
		}
	}
	pass.Ran++
	r.settle(ctx, j, now, why)
}

// settle : Works out when the job runs next and records it.
func (r *Runner) settle(ctx context.Context, j Job, now time.Time, why string) {
	state, next := Done, time.Time{}
	if Repeats(j.Schedule) {
		following, err := Following(j.Schedule, now, r.where())
		if err != nil {
			// A schedule that parsed when it was written and does
			// not now is a job that would be read and skipped for
			// ever. Ended, loudly.
			r.note("a job's schedule can no longer be read", j, err.Error())
			why = err.Error()
		} else {
			state, next = Active, following
		}
	}
	if err := r.Store.Ran(ctx, j.ID, next, state, why, now); err != nil {
		r.log("cannot record that a job ran", err)
	}
}

// where : The person's zone, or UTC.
func (r *Runner) where() *time.Location {
	if r.Where == nil {
		return time.UTC
	}
	return r.Where
}

// clock : Now, in UTC.
func (r *Runner) clock() time.Time {
	if r.Now == nil {
		return time.Now().UTC()
	}
	return r.Now().UTC()
}

// log : A failure of the machinery rather than of a job.
func (r *Runner) log(msg string, err error) {
	if r.Logger != nil {
		r.Logger.Error(msg, slog.Any("error", err))
	}
}

// note : Something that happened to one job.
func (r *Runner) note(msg string, j Job, why string) {
	if r.Logger == nil {
		return
	}
	with := []any{slog.String("job", j.Name), slog.String("schedule", j.Schedule)}
	if why != "" {
		with = append(with, slog.String("why", why))
	}
	r.Logger.Warn(msg, with...)
}
