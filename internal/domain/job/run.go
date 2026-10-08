package job

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"personal-assistant/internal/base/platform/logging"
)

// Tick : How often the runner looks for work.
//
// Close to the reminder loop, because a job saying something at
// eight should say it at eight and not at five past.
const Tick = 2 * time.Second

// Limit : How many jobs are taken in one look.
const Limit = 50

// Over : The most readings fetched when a condition compares
// something with its own past.
//
// A cap rather than a window. A phone reports its battery every
// few minutes, so this reaches back hours; something reporting
// every second would not reach back far, and the comparison
// honestly says it cannot be made rather than pretending.
const Over = 240

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
	// Ran : Records that a job ran, when it next runs, what went
	// wrong if anything, and what it said.
	Ran(ctx context.Context, id string, next time.Time, state State, why, said string, at time.Time) error
	// Quiet : Puts a rule aside until a moment, or lets it run
	// again. The zero time clears it.
	Quiet(ctx context.Context, userID, id string, until time.Time) error
	// Arm : Sets when a waiting rule should run, without
	// recording a run.
	//
	// Its own call rather than [Ran] with a time on it, because
	// nothing has happened yet: a rule that has been set off and
	// is waiting has not run, said anything, or failed, and
	// writing it down as a run would put a line in the record for
	// something nobody did.
	Arm(ctx context.Context, id string, at time.Time) error
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

// Turn : Everything a step is given besides itself.
//
// A struct rather than a fourth parameter, because a sequence is
// one turn and this is what a turn knows: whose it is, what has
// happened in it so far, and what this rule said last time.
type Turn struct {
	// UserID : Whose rule it is.
	UserID string
	// Already : What the earlier steps of this sequence did.
	Already []Ran
	// Before : The sentence this rule last said.
	//
	// What stops a rule saying the same thing every ten minutes
	// for as long as the thing it noticed stays true. Only a
	// written sentence is held to it: words fixed when the rule
	// was made are meant to repeat.
	Before string
	// Anywhere : Whether what this rule says should follow them out
	// of the room, from [Job.Anywhere].
	//
	// Carried on the turn with Worth, and for the same reason: by
	// the time there are words to publish the job is behind us, and
	// what the publisher needs to know about it has to have come
	// with them.
	Anywhere bool
	// Trying : Whether this is a trial rather than the rule
	// running.
	//
	// Everything happens except the saying. A rule is written and
	// then nothing can be done but wait for the world to set it
	// off, which is how a rule that was wrong stayed wrong for a
	// day and how two rules written tonight cannot be shown to
	// work at all.
	//
	// The same steps, through the same code, reading the same
	// readings -- a trial that took a different path would be
	// testing the path rather than the rule. Only the announcing
	// is held back, because a trial that spoke in the room is a
	// side effect nobody asked for.
	Trying bool
	// Worth : How long what this rule says stays worth saying, or
	// zero to leave it to the kind of announcement it is.
	//
	// Carried on the turn because the step that publishes does not
	// see the rule, and because the rule is the only thing that
	// knows: every announcement a rule makes is the same kind, and
	// a battery warning is worth five minutes while a missed call
	// nobody returned is worth the afternoon.
	Worth time.Duration
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
	// Said : What was spoken, for the record of the run.
	Said string
}

// Record : One run of one rule, as somebody reading back would want
// it.
//
// The ordinary output of most rules is silence, and silence has
// several causes: it never came round, its condition did not hold,
// or the model found nothing worth saying. From outside those look
// identical, and so does a runner that has stopped. The row tells
// them apart.
type Record struct {
	// UserID : Whose rule it was.
	UserID string
	// Name : What the rule is called.
	Name string
	// At : When it ran.
	At time.Time
	// Said : What was spoken, empty when nothing was.
	Said string
	// Why : What stopped it or went wrong, empty when it simply ran.
	Why string
	// Stopped : Whether a step ended the sequence early.
	Stopped bool
	// Failed : Whether a step failed.
	Failed bool
}

// Journal : Where each run is written down.
//
// Optional. Nil keeps the engine silent about itself, which is
// what the tests want and nothing else should.
type Journal interface {
	Wrote(ctx context.Context, r Record) error
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
	// The turn carries what the earlier steps have run. A sequence
	// is one turn, not several: the tools refuse to write
	// something they have not read in this turn, and a sequence
	// that forgot its own first step could never satisfy that. It
	// is also what makes [list, add] mean anything, which is the
	// reason sequences exist.
	Do(ctx context.Context, t Turn, s Step) (Outcome, error)
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
	// Journal : Where each run is written down. Optional.
	Journal Journal
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
	// Armed : Triggers that fired and are now waiting out their
	// delay rather than running. Counted apart from Ran, because a
	// rule that has been set off and has not run yet is neither.
	Armed int
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
			// One rule that panics costs one pass. Ending the loop
			// would stop every rule there is, including the ones
			// that work.
			logging.Surviving(ctx, r.Logger, "a rule pass", func() { r.Once(ctx) })
		case l := <-in:
			logging.Surviving(ctx, r.Logger, "a reading arriving", func() {
				r.Landed(ctx, l.userID, l.kinds)
			})
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

		// A trigger that waits does not run when it fires. It sets
		// its own next time and the clock takes it from there,
		// which is how nothing has to sleep.
		//
		// Re-armed if it fires again while waiting, which is the
		// right way round: plugged in, pulled out, plugged in
		// again is a fresh fifteen minutes and not the tail of the
		// old one.
		if wait, err := Waits(j.Schedule); err == nil && wait > 0 {
			if err := r.Store.Arm(ctx, j.ID, now.Add(wait)); err != nil {
				r.log("cannot set when a waiting rule runs", err)
				continue
			}
			pass.Armed++
			continue
		}
		r.one(ctx, j, now, &pass)
	}
	return pass
}

// crossed : Whether what just arrived should set the rule off.
//
// Two questions, depending on what the gate compares.
//
// A measurement is a line to cross. The battery falls 12, 11, 10,
// 9 and the rule speaks once, at 9, because before that it was
// not under ten; at 8 it already was. Without this a phone
// discharging would say something every reading until it died.
//
// A thing that happened is not a line and has no before. Leaving
// home is leaving home whether or not the last place left was
// also home, and asking whether it "changed" threw the second one
// away: there are two consecutive place.exited "home" in this
// person's own week, and the second was silent. So any arrival
// that satisfies the gate sets it off.
//
// The same sentence is still only said once, whichever this is --
// a rule remembers what it last said -- so the cost of a thing
// that reports the same word over and over is work and not noise.
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
	if !g.Measures() {
		return true, nil
	}
	if len(got) < 2 {
		return false, nil
	}
	return !g.Passes(got[1], false, now), nil
}

// Trial : What a rule did when it was tried.
type Trial struct {
	// Said : What it would have said, empty when it would have
	// said nothing.
	Said string
	// Stopped : Whether a step ended the sequence early.
	Stopped bool
	// Why : Which step stopped it, or what went wrong.
	Why string
	// Steps : What each step did, in order, as far as it got.
	Steps []Step
	// Did : What came of each step, beside it.
	Did []string
}

// Try : Runs a rule now and says what it did, without saying
// anything aloud.
//
// Nothing is written down: not the run, not what it said, not
// when it next runs. A trial that moved the rule on would be a
// trial that changed the thing being tried, and one that
// remembered its sentence would silence the real run that
// followed it.
//
// What it does carry is what the rule last said, so a trial
// answers the question actually being asked -- would this speak,
// now, as things stand. A rule holding a sentence it has already
// said will say so, which is the truth and is worth seeing.
func (r *Runner) Try(ctx context.Context, j Job) Trial {
	var out Trial
	if r == nil || r.Doer == nil {
		return out
	}

	var already []Ran
	for _, step := range j.Steps {
		done, err := r.Doer.Do(ctx,
			Turn{UserID: j.UserID, Already: already, Before: j.Said,
				Worth: j.Worth(), Anywhere: j.Anywhere, Trying: true}, step)
		out.Steps = append(out.Steps, step)

		switch {
		case err != nil:
			out.Did = append(out.Did, err.Error())
			out.Why = err.Error()
		case done.Stop:
			out.Did = append(out.Did, done.Why)
		case done.Said != "":
			out.Did = append(out.Did, done.Said)
		case done.Got != "":
			out.Did = append(out.Did, done.Got)
		default:
			out.Did = append(out.Did, "")
		}

		if step.Do == Call && step.Tool != "" {
			already = append(already, Ran{Tool: step.Tool, Got: done.Got})
		}
		if err != nil {
			continue
		}
		if done.Said != "" && !done.Stop {
			out.Said = done.Said
		}
		if done.Stop {
			out.Stopped = true
			if out.Why == "" {
				out.Why = done.Why
			}
			break
		}
	}
	return out
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
	// Asked of anything with a time, which a watching rule now has
	// when it is waiting out a delay. One that is not waiting has
	// no time at all, and the reading that set it off arrived a
	// moment ago by definition.
	if !j.Next.IsZero() && now.Sub(j.Next) > Stale {
		pass.Skipped++
		r.note("a job's moment passed while nothing was running", j, "")
		// The memory is left alone: nothing was said, and nothing
		// was decided against saying either.
		r.settle(ctx, j, now, "its moment passed before anything ran", j.Said)
		return
	}

	// What went wrong, if anything did. Collected rather than
	// recorded as it happens, so that one run leaves one record
	// however many of its steps failed.
	//
	// spoke and remember are two different things and were one
	// variable, which is how the record of what the assistant
	// considered saying came to log "said" thirty times an hour
	// for a rule that had been silent all afternoon.
	//
	// A step that stops the sequence hands back the sentence this
	// rule said last time, so the memory that stops it repeating
	// itself survives a run that found nothing. That is a thing to
	// carry, never a thing that was spoken. A step that does not
	// stop and hands back a sentence has just said it.
	var why, spoke, remember string
	var stopped, failed bool
	var already []Ran
	for _, step := range j.Steps {
		done, err := r.Doer.Do(ctx,
			Turn{UserID: j.UserID, Already: already, Before: j.Said,
				Worth: j.Worth(), Anywhere: j.Anywhere}, step)
		if step.Do == Call && step.Tool != "" {
			already = append(already, Ran{Tool: step.Tool, Got: done.Got})
		}
		if err != nil {
			pass.Failed++
			failed = true
			why = err.Error()

			// A check that could not be read stops the sequence.
			// Everything else carries on: a phone alarm that could
			// not be set must not swallow the words that go with
			// it, which is the rule the reminder and its alarm
			// already follow.
			//
			// A check is the opposite of that. Its whole purpose is
			// to withhold, so a broken one must withhold -- and it
			// did not. A probe written this evening had three
			// conditions, the third unreadable, and the rule said
			// its words aloud in the room: the step that was meant
			// to stop it failed, and failing was taken for
			// permission.
			//
			// Nothing else in the sequence can be wrong in this
			// direction. A lookup that fails leaves the steps after
			// it less to go on; a check that fails leaves them
			// unguarded.
			if step.Do == Check {
				pass.Stopped++
				stopped = true
				r.note("a rule's condition could not be read, so it said nothing", j, why)
				break
			}
			r.note("a step failed and the sequence went on", j, why)
			continue
		}
		if done.Said != "" {
			if done.Stop {
				// Only where nothing has been said in this run.
				// A sequence that spoke and was then stopped must
				// remember what it just said, not what it said
				// before.
				if spoke == "" {
					remember = done.Said
				}
			} else {
				spoke, remember = done.Said, done.Said
			}
		}
		if done.Stop {
			pass.Stopped++
			stopped = true
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
	if j.Speaks() {
		// Only a rule that might have spoken belongs in the record
		// of what was considered saying. One that only ever runs a
		// tool considered nothing.
		r.wrote(ctx, Record{
			UserID: j.UserID, Name: j.Name, At: now,
			Said: spoke, Why: why, Stopped: stopped, Failed: failed,
		})
	}
	// Quiet for a while, where the rule asked for it and there was
	// something to be quiet about. Blunt where the sentence
	// comparison is sharp: that catches the same news in the same
	// words and lets a rewording through, and this catches
	// everything for an hour and knows nothing about any of it.
	//
	// Only after speaking. A rule that found nothing has nothing
	// to hold off from, and resting it then would turn every quiet
	// minute into a quieter one.
	if cool := j.Cooling(); cool > 0 && spoke != "" {
		if err := r.Store.Quiet(ctx, j.UserID, j.ID, now.Add(cool)); err != nil {
			r.log("cannot quieten a rule after it spoke", err)
		}
	}

	r.settle(ctx, j, now, why, remember)
}

// wrote : Writes down one run, if anybody is keeping the record.
func (r *Runner) wrote(ctx context.Context, rec Record) {
	if r.Journal == nil {
		return
	}
	if err := r.Journal.Wrote(ctx, rec); err != nil {
		r.log("cannot write down that a rule ran", err)
	}
}

// settle : Works out when the job runs next and records it.
func (r *Runner) settle(ctx context.Context, j Job, now time.Time, why, said string) {
	// A watching rule has no next time and does not end. It waits
	// for the next reading that crosses its line -- unless it has
	// a clock as well, in which case the clock decides when it
	// next comes round and the trigger goes on watching between.
	if Watches(j.Schedule) && !Ticks(j.Schedule) {
		if err := r.Store.Ran(ctx, j.ID, time.Time{}, Active, why, said, now); err != nil {
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
	if err := r.Store.Ran(ctx, j.ID, next, state, why, said, now); err != nil {
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
