package job

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	// Waits : Stops a rule part-way through and says when to carry
	// on and where from.
	//
	// Its own call for the same reason as [Arm]: the rule has not
	// finished, so writing it down as a run would put a line in
	// the record for something that is still happening.
	Waits(ctx context.Context, id string, at time.Time, step int) error
	// Forgets : Clears what a rule last said, so the next time it
	// has something to say it is heard.
	Forgets(ctx context.Context, id string) error
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
	// JobID : Which rule. The name is what it was called at the
	// time and a rule may be renamed or rewritten; this is what
	// joins the two halves of its life.
	JobID string
	// Name : What the rule is called.
	Name string
	// Schedule : How it was written at the time, for the same
	// reason as the name.
	Schedule string
	// Took : How long the whole sequence took, so a rule that got
	// slower can be found rather than noticed.
	Took time.Duration
	// Steps : What each step did, in order. What makes "why did
	// this behave differently" answerable.
	Steps []Tried
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

// Tried : What one step of a run did.
//
// Says what the step was rather than where it sat, because a
// sequence whose third step changed is not the same as one with a
// step inserted second, and comparing two shapes of one rule is
// the whole reason this is written down.
type Tried struct {
	// Do : Which kind of step.
	Do Task `json:"do"`
	// Use : The tool it called, where it called one.
	Use string `json:"use,omitempty"`
	// When : The condition it read, where it read one.
	When string `json:"when,omitempty"`
	// Came : What came of it -- [Went], [Halted] or [Errored].
	Came string `json:"came"`
	// Took : How long it took, in milliseconds.
	Took int64 `json:"took_ms"`
	// Why : What it decided or what went wrong. Short: the whole
	// of a tool's answer belongs in the conversation, not here.
	Why string `json:"why,omitempty"`
}

// Went, Halted, Errored : What one step came to.
const (
	Went    = "ran"
	Halted  = "stopped"
	Errored = "failed"
)

// most : How much of a reason is kept beside a step.
//
// Enough to tell two outcomes apart and not enough to be a
// transcript. What a tool actually returned belongs in the
// conversation; this column is read a thousand rows at a time.
const most = 200

// shorten : A reason, cut to something a column can hold.
func shorten(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= most {
		return s
	}
	return s[:most] + "..."
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
// ErrBlind : A step that was meant to find something failed, so
// everything after it would be deciding without what it was given
// to decide on.
//
// The other half of the rule that a failed check stops a sequence.
// That one was learned from a rule whose third condition could not
// be read and which said its words aloud anyway: the step meant to
// withhold failed, and failing was taken for permission. This is
// the same mistake one move further on. A lookup that fails does
// not withhold anything -- it simply is not there -- and the write
// step after it still has an instruction, still has a model, and
// still produces a sentence, composed from a picture with a hole
// in it that nothing in the sentence will mention.
//
// Only a read. A tool that was meant to *do* something and failed
// must not stop the sequence, which is the older rule and still
// right: a reminder whose phone alarm could not be set must still
// say its words aloud. The two are the same step kind and want
// opposite things, and what separates them is whether anything
// after them was relying on what they returned.
var ErrBlind = errors.New("job: a step that was meant to find something failed")

type Doer interface {
	// Holds : Whether a rule's conditions hold, and which one did
	// not when they do not.
	//
	// On the doer rather than the runner because answering it
	// needs the readings *and* whether anybody is in the room, and
	// the one place that has both is the thing that was already
	// carrying out check steps.
	Holds(ctx context.Context, userID string, when Occasion) (bool, string, error)

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
	Do(ctx context.Context, t Turn, a Act) (Outcome, error)
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
		hooks, gates, err := j.When.Watching()
		if err != nil {
			r.note("a rule's trigger can no longer be read", j, err.Error())
			continue
		}
		// Several hooks mean any one of them fires it, so the
		// first that both landed and crossed wins and the rest are
		// not asked. One arrival is one occasion however many
		// hooks it satisfies.
		fired, wait := false, time.Duration(0)
		for i, gate := range gates {
			if !landed[gate.Needs()] {
				continue
			}
			crossed, err := r.crossed(ctx, userID, gate, now)
			if err != nil {
				r.log("cannot read what a rule is waiting on", err)
				continue
			}
			if !crossed {
				// Not crossing is two different things, and one of
				// them is the end of an occasion.
				//
				// A rule woken by a crossing is told what it said
				// last time, so that the same news is not
				// announced twice. For a rule that watches a
				// *line* that memory must not outlive the time
				// spent on the wrong side of it: the battery
				// passing fifteen per cent today is not the same
				// news as it passing fifteen per cent yesterday,
				// however alike the two sentences are.
				//
				// It was. Measured 9 October 2026: the battery
				// fell from 52% to 13% over six hours, every one
				// of "battery at 25", "battery at 20" and
				// "battery at 15" fired at exactly the right
				// reading, and all three were declined with
				// "there was nothing new to say" against
				// sentences from a previous discharge. Four runs
				// in the journal, four declines, not one word
				// spoken -- and because a decline deliberately
				// keeps the sentence, each one guaranteed the
				// next.
				//
				// So the memory is let go the moment the reading
				// comes back over the line. Nothing else can do
				// it: a rule that is not firing is not running,
				// and this is the only place that sees the
				// readings of a rule that stayed quiet.
				if gate.Measures() && !r.holding(ctx, userID, gate, now) {
					if err := r.Store.Forgets(ctx, j.ID); err != nil {
						r.log("cannot let go of what a rule said", err)
					}
				}
				continue
			}
			fired = true
			if held, err := hooks[i].Waits(); err == nil {
				wait = held
			}
			break
		}
		if !fired {
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
		if wait > 0 {
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

// holding : Whether this gate passes on the newest reading.
//
// The other half of [Runner.crossed], which asks whether a reading
// has just come over a line. This asks only whether it is over it
// now, and is how a rule learns that the thing it spoke about has
// gone away.
//
// False when nothing can be read. Nothing known is not proof that
// a condition holds, and the cost of being wrong here is one
// sentence said twice rather than a warning never said at all.
func (r *Runner) holding(ctx context.Context, userID string, g Gate, now time.Time) bool {
	got, err := r.Readings.Latest(ctx, userID, g.Needs(), 1)
	if err != nil || len(got) == 0 {
		return false
	}
	return g.Passes(got[0], false, now)
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
	// Acts : What each step did, in order, as far as it got.
	Acts []Act
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

	// The conditions first, as a real run asks them, so a trial
	// says "it would not have held, because this" rather than
	// walking steps a run would never have reached.
	if len(j.When.All) > 0 || len(j.When.Any) > 0 {
		held, says, err := r.Doer.Holds(ctx, j.UserID, j.When)
		switch {
		case err != nil:
			out.Why = err.Error()
			out.Did = append(out.Did, err.Error())
			return out
		case !held:
			out.Why = says
			out.Did = append(out.Did, says)
			return out
		}
		out.Did = append(out.Did, "the conditions held")
	}

	var already []Ran
	for _, act := range j.Perform {
		done, err := r.Doer.Do(ctx,
			Turn{UserID: j.UserID, Already: already, Before: j.Said,
				Worth: j.Worth(), Anywhere: j.Anywhere, Trying: true}, act)
		out.Acts = append(out.Acts, act)

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

		if act.Do == DoTool && act.Use != "" {
			already = append(already, Ran{Tool: act.Use, Got: done.Got})
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
	// What each step did, collected as it goes. A run is written
	// down once at the end, so this is the only record of the
	// steps that came before whichever one ended it.
	tried := make([]Tried, 0, len(j.Perform)+1)
	began := r.clock()

	// Whether it holds at all, before anything is done. Every
	// condition is a precondition -- a condition can only read a
	// source, never what a step returned, so it would give the
	// same answer wherever it sat -- and asking them all first is
	// what stops a rule fetching a diary to find out it had
	// nothing to say.
	// Not asked again when a rule is carrying on from a wait. They
	// held when the sequence began and that is what started it;
	// asking again would make "wait five minutes" quietly mean
	// "wait five minutes and check nothing has changed", which is
	// a different rule and not the one anybody wrote.
	if resuming := j.Resume > 0 && j.Resume < len(j.Perform); !resuming &&
		(len(j.When.All) > 0 || len(j.When.Any) > 0) {
		at := r.clock()
		held, says, err := r.Doer.Holds(ctx, j.UserID, j.When)
		note := Tried{Do: "when", When: j.When.Describe(), Came: Went,
			Took: r.clock().Sub(at).Milliseconds()}
		switch {
		case err != nil:
			note.Came, note.Why = Errored, shorten(err.Error())
		case !held:
			note.Came, note.Why = Halted, shorten(says)
		}
		tried = append(tried, note)
		if err != nil {
			pass.Failed++
			pass.Stopped++
			r.note("a rule's conditions could not be read, so it said nothing", j, err.Error())
			r.wrote(ctx, Record{
				UserID: j.UserID, JobID: j.ID, Name: j.Name,
				Schedule: j.Schedule, At: now, Took: r.clock().Sub(began),
				Steps: tried, Why: err.Error(), Stopped: true, Failed: true,
			})
			r.settle(ctx, j, now, err.Error(), j.Said)
			return
		}
		if !held {
			pass.Stopped++
			r.wrote(ctx, Record{
				UserID: j.UserID, JobID: j.ID, Name: j.Name,
				Schedule: j.Schedule, At: now, Took: r.clock().Sub(began),
				Steps: tried, Why: says, Stopped: true,
			})
			r.settle(ctx, j, now, says, j.Said)
			return
		}
	}

	from := 0
	if j.Resume > 0 && j.Resume < len(j.Perform) {
		from = j.Resume
	}
	for i := from; i < len(j.Perform); i++ {
		act := j.Perform[i]
		// Stop here and come back. Not a sleep: one pass runs every
		// due rule on one goroutine, so a rule that slept would
		// stop all of them.
		//
		// Nothing is recorded as a run, because the run has not
		// finished. What it said so far is kept the same way a
		// finished run keeps it, so the half that already spoke is
		// not said again when the other half arrives.
		if act.Do == DoWait {
			d, err := Span(act.For)
			if err != nil {
				pass.Failed++
				failed, why = true, err.Error()
				r.note("a rule could not read how long to wait", j, why)
				break
			}
			tried = append(tried, Tried{Do: Task(DoWait), When: act.For, Came: Went})
			if err := r.Store.Waits(ctx, j.ID, now.Add(d), i+1); err != nil {
				r.log("cannot set a rule to carry on", err)
			}
			if j.Speaks() {
				r.wrote(ctx, Record{
					UserID: j.UserID, JobID: j.ID, Name: j.Name,
					Schedule: j.Schedule, At: now, Took: r.clock().Sub(began),
					Steps: tried, Said: spoke, Why: "waiting " + act.For,
				})
			}
			return
		}
		at := r.clock()
		done, err := r.Doer.Do(ctx,
			Turn{UserID: j.UserID, Already: already, Before: j.Said,
				Worth: j.Worth(), Anywhere: j.Anywhere}, act)
		note := Tried{
			Do: Task(act.Do), Use: act.Use, Came: Went,
			Took: r.clock().Sub(at).Milliseconds(),
		}
		switch {
		case err != nil:
			note.Came, note.Why = Errored, shorten(err.Error())
		case done.Stop:
			note.Came, note.Why = Halted, shorten(done.Why)
		}
		tried = append(tried, note)
		if act.Do == DoTool && act.Use != "" {
			already = append(already, Ran{Tool: act.Use, Got: done.Got})
		}
		if err != nil {
			pass.Failed++
			failed = true
			why = err.Error()

			// A read that failed stops the sequence.
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
			// it less to go on; a condition that fails leaves them
			// unguarded -- and that one is answered before any of
			// this, where a rule whose conditions cannot be read
			// does nothing at all.
			//
			// A lookup that failed. Everything after it would be
			// working from what it did not return, and a write
			// step cannot tell the difference between a thing
			// that is not there and a thing it was never handed.
			if errors.Is(err, ErrBlind) {
				pass.Stopped++
				stopped = true
				r.note("a rule could not find what it needed, so it said nothing", j, why)
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
			UserID: j.UserID, JobID: j.ID, Name: j.Name,
			Schedule: j.Schedule, At: now, Took: r.clock().Sub(began),
			Steps: tried,
			Said:  spoke, Why: why, Stopped: stopped, Failed: failed,
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
	hooks, _, _ := j.When.Watching()
	if len(hooks) > 0 && len(j.When.Ticking()) == 0 {
		if err := r.Store.Ran(ctx, j.ID, time.Time{}, Active, why, said, now); err != nil {
			r.log("cannot record that a job ran", err)
		}
		return
	}

	state, next := Done, time.Time{}
	if j.When.Repeats() {
		// The soonest of its clocks, not the first written. Several
		// hooks mean any one of them fires it.
		following, err := j.When.Next(now, r.where())
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
