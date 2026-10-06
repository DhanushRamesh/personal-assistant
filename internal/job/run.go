package job

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Tick : How often the runner looks for work.
//
// Close to the reminder loop, because a job saying something at
// eight should say it at eight and not at five past.
const Tick = 2 * time.Second

// Limit : How many jobs are taken in one look.
const Limit = 50

// Backlog : How many arrivals wait to be looked at.
//
// Small, because the readings behind them keep coming. A runner
// so far behind that this is full is better off dropping a batch
// and acting on the next one than working through a queue of
// readings that have all since been overtaken.
const Backlog = 64

// Store : Where jobs are kept.
type Store interface {
	// Due : Jobs whose time has come, soonest first.
	Due(ctx context.Context, at time.Time, limit int) ([]Job, error)
	// Watching : This person's active rules that wait for
	// something to happen rather than for a time.
	Watching(ctx context.Context, userID string, limit int) ([]Job, error)
	// Ran : Records that a job ran, when it next runs, and what
	// went wrong if anything.
	Ran(ctx context.Context, id string, next time.Time, state State, why string, at time.Time) error
}

// Ran : What a step earlier in the same sequence did.
//
// Carried forward because a sequence is one turn. The tool names
// are what the tools themselves need, to satisfy their rule about
// reading before writing; what the tools returned is what a
// [Write] step needs, because the sentence it composes is about
// whatever the sequence just found.
//
// This is a transcript of the turn, not variables passed between
// steps. Nothing here is compared or branched on -- only read by a
// model, and only ever appended to.
type Ran struct {
	// Tool : Which tool ran.
	Tool string
	// Got : What it returned, as the prose it returns.
	Got string
}

// Tools : The names of the tools that ran, in order.
func Tools(already []Ran) []string {
	names := make([]string, 0, len(already))
	for _, r := range already {
		names = append(names, r.Tool)
	}
	return names
}

// Readings : The values an event kind has lately had.
//
// What the runner needs to tell a crossing from a reading that
// was already past the line. Held here rather than inside a step,
// because deciding whether a rule should run at all is the
// runner's business.
type Readings interface {
	// Latest : The most recent readings of a kind, newest first.
	Latest(ctx context.Context, userID, kind string, n int) ([]Reading, error)
}

// Outcome : What carrying out one step came to.
type Outcome struct {
	// Stop : Whether the rest of the sequence should not run.
	Stop bool
	// Why : Why it stopped, for the record. Not a failure -- a
	// check that did not pass is the engine working.
	Why string
	// Got : What a tool returned, for the steps after it to read.
	Got string
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
	Do(ctx context.Context, userID string, s Step, already []Ran) (Outcome, error)
}

// Runner : Runs jobs when their time comes.
type Runner struct {
	// Store : Where jobs are kept. Required.
	Store Store
	// Doer : What carries out a step. Required.
	Doer Doer
	// Readings : What an event kind has lately said, for rules
	// that wait on one. Nil means no rule ever fires.
	Readings Readings
	// Where : The person's zone, for a schedule written as a clock
	// time. Nil is UTC.
	Where *time.Location
	// Tick : How often to look. Zero selects Tick.
	Tick time.Duration
	// Now : The clock, replaceable in tests.
	Now func() time.Time
	// Logger : Where failures go. Nil is silent.
	Logger *slog.Logger

	// The arrivals waiting to be looked at, made on first use so a
	// Runner stays usable as a plain struct literal.
	once    sync.Once
	landed  chan landing
	dropped atomic.Int64
}

// landing : A batch of readings that have just been stored.
type landing struct {
	userID string
	kinds  []string
}

// Arrived : Tells the runner that events of these kinds have just
// been stored for this person.
//
// Never blocks and never fails. Whatever calls this is in the
// middle of answering a device, and a phone handing over its
// readings must not be kept waiting on a rule -- still less on the
// model a rule might ask for a sentence.
func (r *Runner) Arrived(userID string, kinds []string) {
	if r == nil || userID == "" || len(kinds) == 0 {
		return
	}
	select {
	case r.inbox() <- landing{userID: userID, kinds: kinds}:
	default:
		// Counted rather than logged. A runner this far behind
		// would otherwise write a line per batch about being
		// behind, which is the thing making it behind.
		r.dropped.Add(1)
	}
}

// inbox : The arrivals channel, made once.
func (r *Runner) inbox() chan landing {
	r.once.Do(func() { r.landed = make(chan landing, Backlog) })
	return r.landed
}

// Dropped : How many arrivals were thrown away because the runner
// was too far behind to look at them.
func (r *Runner) Dropped() int64 { return r.dropped.Load() }

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

	in := r.inbox()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.Once(ctx)
		case l := <-in:
			r.Landed(ctx, l.userID, l.kinds)
		}
	}
}

// Landed : Runs the rules that were waiting for one of these kinds.
//
// A rule fires on the **crossing**, not on the condition being
// true. A phone sitting at fifteen per cent reports itself every
// few minutes, and a rule that ran each time would say the same
// thing all afternoon. So the reading before this one is read
// too, and the rule runs only where the condition was false then
// and is true now.
//
// It follows that a rule can never fire on the first reading of a
// kind the server has never seen. That is the same answer a check
// gives when it has nothing to read, for the same reason: nothing
// is known, so no crossing is proved.
func (r *Runner) Landed(ctx context.Context, userID string, kinds []string) Pass {
	var pass Pass
	if r == nil || r.Store == nil || r.Doer == nil || r.Readings == nil {
		return pass
	}

	watching, err := r.Store.Watching(ctx, userID, Limit)
	if err != nil {
		r.log("cannot read which rules are waiting", err)
		return pass
	}
	if len(watching) == 0 {
		return pass
	}

	landed := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		landed[k] = true
	}

	now := r.clock()
	for _, j := range watching {
		gate, err := Watched(j.Schedule)
		if err != nil {
			r.note("a rule's condition can no longer be read", j, err.Error())
			continue
		}
		if !landed[gate.Needs()] {
			continue
		}
		crossed, err := r.crossed(ctx, userID, gate, now)
		if err != nil {
			r.log("cannot read what a rule is waiting on", err)
			continue
		}
		if !crossed {
			continue
		}
		r.one(ctx, j, now, &pass)
	}
	return pass
}

// crossed : Whether the condition has just become true.
func (r *Runner) crossed(ctx context.Context, userID string, g Gate, now time.Time) (bool, error) {
	got, err := r.Readings.Latest(ctx, userID, g.Needs(), 2)
	if err != nil {
		return false, err
	}
	if len(got) == 0 {
		return false, nil
	}
	// away is false throughout: a watching rule may not ask about
	// presence, because presence does not arrive.
	if !g.Passes(got[0], false, now) {
		return false, nil
	}
	if len(got) < 2 {
		return false, nil
	}
	return !g.Passes(got[1], false, now), nil
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
	//
	// Only ever asked of a rule that had a time. A watching rule
	// has none, and the reading that set it off arrived a moment
	// ago by definition.
	if !Watches(j.Schedule) && now.Sub(j.Next) > Stale {
		pass.Skipped++
		r.note("a job's moment passed while nothing was running", j, "")
		r.settle(ctx, j, now, "its moment passed before anything ran")
		return
	}

	// What went wrong, if anything did. Collected rather than
	// recorded as it happens, so that one run leaves one record
	// however many of its steps failed.
	var why string
	var already []Ran
	for _, step := range j.Steps {
		done, err := r.Doer.Do(ctx, j.UserID, step, already)
		if step.Do == Call && step.Tool != "" {
			already = append(already, Ran{Tool: step.Tool, Got: done.Got})
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
	// A watching rule has no next time and does not end. It waits
	// for the next reading that crosses its line.
	if Watches(j.Schedule) {
		if err := r.Store.Ran(ctx, j.ID, time.Time{}, Active, why, now); err != nil {
			r.log("cannot record that a job ran", err)
		}
		return
	}

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
