// Package job runs a sequence of tasks on a schedule.
//
// Owner, 6 October 2026: *"I will give the AI when to do what, how
// frequently to do what, so a timer control I should be able to
// give, and the what might be an already available task function or
// hand it off to AI to do a custom task. The custom task will be
// just announce too, as it is also a task."* And: *"we don't need
// separate reminders, because it will also be a schedule timer with
// a task -- just to talk. To keep an alarm too will be a task,
// because not just a single task, a sequence of tasks can be
// done."*
//
// What this replaces is seven loops in Go, each watching one thing
// and each ending in the same place: say something. Announcing
// stops being the shape of the system and becomes one task among
// several, which is what it always was.
//
// The proof that this is the right shape was already in the code.
// `reminder_set` does two things -- says the words and sets a phone
// alarm -- which is a two step sequence with the steps written into
// a function. Here it is a row.
//
// # Where this stops
//
// Linear sequences. No branching, no nesting, no else. A step may
// stop the ones after it, which covers the conditional cases -- *do
// this only if the battery is low* is a step that checks and stops
// -- without the next question being loops and variables. That line
// is held on purpose: Tasker exists, Home Assistant exists, and a
// third automation language in this house would be a mistake.
package job

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// State : Where a job is in its life.
type State string

const (
	// Active : Due to run again.
	Active State = "active"
	// Done : A one-off that has run.
	Done State = "done"
	// Cancelled : Called off before it ran again.
	Cancelled State = "cancelled"
)

// Errors a caller can act on.
var (
	// ErrNoSchedule : Nothing says when to run it.
	ErrNoSchedule = errors.New("job: there is no schedule")
	// ErrNoSteps : A schedule with nothing to do.
	ErrNoSteps = errors.New("job: there is nothing to do")
	// ErrUnreadable : The schedule is not one of the forms.
	ErrUnreadable = errors.New("job: that schedule could not be read")
)

// Task : What one step does.
type Task string

const (
	// Announce : Say something. A task like any other, which is the
	// whole point of the rewrite.
	Announce Task = "announce"
	// Call : Run one of the tools the assistant already has.
	//
	// The verbs were never missing. There are fifty-two of them and
	// a rule needs a way to point at one, not a new vocabulary.
	Call Task = "tool"
	// Write : Ask the model for the sentence, then say it.
	//
	// What [Announce] cannot do. Most of what this assistant says
	// unasked is composed from whatever was just found, and a
	// sentence fixed when the rule was written can only ever be
	// the same sentence.
	Write Task = "write"
	// Relay : Say what the tool before it returned, as it stands.
	//
	// For a tool that has already settled the words. The diary
	// works out its own sentence when it judges an entry, months
	// ahead of saying it; handing that to a model to say again
	// would be a second model call that can only lose fidelity.
	//
	// [Write] is for when the words do not exist yet. This is for
	// when they do.
	Relay Task = "relay"
	// Check : Read a condition and stop the rest of the sequence
	// unless it holds.
	//
	// A check as the first step is a condition on the whole job; a
	// check in the middle is a condition on what follows. One kind
	// of step covers both.
	Check Task = "check"
)

// Step : One thing to do.
type Step struct {
	// Do : Which kind of task.
	Do Task `json:"do"`
	// Say : The words, for an announce.
	Say string `json:"say,omitempty"`
	// Tool : Which tool to run, for a call.
	Tool string `json:"tool,omitempty"`
	// Args : What to run it with, as the tool's own arguments.
	Args json.RawMessage `json:"args,omitempty"`
	// Ask : What to write about, for a write.
	//
	// An instruction rather than words: *say whether anything about
	// the battery is worth mentioning*. What the earlier steps of
	// the sequence found is given to the model with it.
	Ask string `json:"ask,omitempty"`
	// Tools : What the model may call for itself, on a write.
	//
	// Named here rather than left open, so that what a rule can
	// reach is readable off the row. The model decides whether to
	// call them and with what arguments, which is the half a rule
	// cannot know when it is written.
	Tools []string `json:"tools,omitempty"`
	// When : The condition, as it was written, for a check.
	//
	// Kept as written rather than parsed into fields, for the same
	// reason as [Job.Schedule]: it is read back to the person who
	// said it.
	//
	// May hold alternatives joined by "or", any one of which
	// satisfies it. Two check steps in a row are the and; this is
	// the or, and there is no third thing.
	When string `json:"when,omitempty"`
}

// Job : A schedule and the sequence it runs.
type Job struct {
	// ID : This job.
	ID string
	// UserID : Whose it is.
	UserID string
	// Name : What to call it when listing them.
	Name string
	// Kind : What it is in the person's own words.
	//
	// So that somebody asking what reminders they have is not told
	// they have fourteen jobs including a battery check. The engine
	// does not read this; the listing does.
	Kind string

	// Schedule : When to run, as it was written.
	Schedule string
	// Next : When it next runs.
	Next time.Time
	// Steps : What to do, in order.
	Steps []Step

	// State : Active, done or cancelled.
	State State
	// Last : When it last ran.
	Last time.Time
	// Why : What happened last run -- what went wrong, or which
	// check stopped it. Empty when it simply ran.
	Why string
	// Quiet : When this rule may speak again, or the zero time when
	// it may speak now.
	//
	// Set by somebody putting it aside for a week, and by the rule
	// itself after it has spoken when it has a [Job.Cool]. One
	// field because they are the same thing: a rule that is not to
	// run until a moment.
	Quiet time.Time
	// Cool : How long to stay quiet after speaking, as it was
	// written. Empty is a rule that may speak as often as it has
	// something to say.
	//
	// Blunt where [Job.Said] is sharp. The sentence comparison
	// catches the same news in the same words and lets the same
	// news in different words through; this catches everything and
	// knows nothing. They work together.
	Cool string
	// Keep : How long what this rule says stays worth saying, as it
	// was written. Empty leaves it to the kind.
	//
	// A rule knows something the kind cannot. Every announcement a
	// rule makes is the same kind, and a battery warning is worth
	// five minutes while a missed call nobody returned is worth the
	// afternoon.
	Keep string
	// Anywhere : Whether what this rule says is worth following them
	// out of the room. False keeps it to the room, which is where
	// almost everything belongs.
	//
	// The same kind of thing as [Job.Keep], and for the same reason:
	// a rule knows what its kind cannot. Every announcement a rule
	// makes fires the moment it has something to say, and that is
	// usually only worth hearing where somebody is standing -- but
	// the phone running out before they leave is worth most when
	// they are already out, which is the one case the room can never
	// cover.
	//
	// Costly enough to be asked for rather than assumed: it is the
	// words going out through a push service, which nothing else
	// here does.
	Anywhere bool
	// Said : The sentence this rule last said, so it does not say
	// it again.
	//
	// Cleared whenever a run says nothing, so a thing that goes
	// away and comes back is announced again.
	Said string
}

// Every, At, Once : How a schedule may be written.
//
//	every 5m          every five minutes, from now
//	at 08:00          every day at eight in the morning
//	once 2026-10-07 06:40   one time, then done
//	when battery.level < 20   not at a time at all
//
// Four forms and no more. A cron expression is unreadable to
// somebody dictating it out loud, which is how these are meant to
// be made.
//
// [When] is the odd one: it has no next time, so the clock never
// picks it up. It runs when a reading arrives that makes its
// condition true, which is the only way to say *when I get home*
// or *when the battery drops*, neither of which happens at an
// hour anybody knows in advance.
const (
	Every = "every"
	At    = "at"
	Once  = "once"
	When  = "when"
)

// Stale : How late a run may be and still happen.
//
// A server that was off over the moment comes back owing runs, and
// doing yesterday's eight o'clock at noon is worse than skipping
// it. The same judgement the outbox makes about announcements, for
// the same reason.
const Stale = time.Hour

// Resting : Whether this rule is to stay quiet for now.
func (j Job) Resting(now time.Time) bool {
	return !j.Quiet.IsZero() && now.Before(j.Quiet)
}

// Cooling : How long this rule stays quiet after speaking, or zero.
func (j Job) Cooling() time.Duration {
	if strings.TrimSpace(j.Cool) == "" {
		return 0
	}
	cool, err := Span(j.Cool)
	if err != nil {
		return 0
	}
	return cool
}

// Worth : How long this rule's announcements stay worth saying, or
// zero to leave it to the kind.
func (j Job) Worth() time.Duration {
	if strings.TrimSpace(j.Keep) == "" {
		return 0
	}
	keep, err := Span(j.Keep)
	if err != nil {
		return 0
	}
	return keep
}

// Kept : Whether a written length of time can be read, for
// refusing one that cannot in front of whoever wrote it.
func Kept(keep string) error {
	keep = strings.TrimSpace(keep)
	if keep == "" {
		return nil
	}
	if _, err := Span(keep); err != nil {
		return fmt.Errorf("%w: %q is not a length of time", ErrUnreadable, keep)
	}
	return nil
}

// New : A job, with its first run worked out.
func New(id, userID, name, kind, schedule string, steps []Step, from time.Time) (Job, error) {
	if strings.TrimSpace(schedule) == "" {
		return Job{}, ErrNoSchedule
	}
	if len(steps) == 0 {
		return Job{}, ErrNoSteps
	}
	for _, step := range steps {
		if err := step.Valid(); err != nil {
			return Job{}, err
		}
	}
	// A watching rule has no first run to work out. Its next_at
	// stays null, which is what keeps the clock from ever picking
	// it up.
	if err := joined(schedule); err != nil {
		return Job{}, err
	}

	// Both halves are read, and both must be readable. A rule with
	// a clock and a trigger is two things that set it off, not a
	// choice between them.
	var next time.Time
	if Watches(schedule) {
		if _, err := Watched(schedule); err != nil {
			return Job{}, err
		}
		if _, err := Waits(schedule); err != nil {
			return Job{}, err
		}
	}
	if Ticks(schedule) {
		following, err := Following(schedule, from, from.Location())
		if err != nil {
			return Job{}, err
		}
		next = following
	}
	if !Watches(schedule) && !Ticks(schedule) {
		return Job{}, fmt.Errorf("%w: %q", ErrUnreadable, schedule)
	}
	return Job{
		ID: id, UserID: userID, Name: name, Kind: kind,
		Schedule: schedule, Next: next, Steps: steps, State: Active,
	}, nil
}

// Speaks : Whether any of this rule's steps says anything.
//
// What tells a rule that found nothing worth saying from one that
// was never going to say anything. Only the first belongs in the
// record of what the assistant considered saying.
func (j Job) Speaks() bool {
	for _, s := range j.Steps {
		if s.Do == Announce || s.Do == Write {
			return true
		}
	}
	return false
}

// Also : What joins a clock to a trigger, for a rule that wants
// both.
//
//	every 30m also when battery.level < 20
//
// A rule had one mouth: it came round on a clock or it waited for
// something, never both. Which is right for almost everything and
// wrong for the one shape that keeps coming up -- tell me when
// this happens, and look anyway now and then in case the moment
// was missed while nothing was running.
//
// A plain word rather than "or", because they are not
// alternatives: both are live, and either sets the rule off.
const Also = " also "

// joined : Whether a schedule naming both halves names two of
// them.
//
// [Also] is spaced on both sides, so a schedule ending in the word
// does not split and the word is read as part of the clock:
// "every 30m also" parsed as a perfectly good half-hour. Caught
// here rather than left to mean something nobody wrote.
func joined(schedule string) error {
	word := strings.TrimSpace(Also)
	for _, field := range strings.Fields(strings.ToLower(strings.TrimSpace(schedule))) {
		if field != word {
			continue
		}
		before, after, ok := strings.Cut(strings.TrimSpace(schedule), Also)
		if !ok || strings.TrimSpace(before) == "" || strings.TrimSpace(after) == "" {
			return fmt.Errorf("%w: %q has a %q with nothing on one side of it",
				ErrUnreadable, schedule, word)
		}
	}
	return nil
}

// Clock : The part of a schedule that is a time, or empty.
//
// Everything before "also" when there is one, which is where a
// clock goes: the trigger is what reads strangely in the middle
// of a sentence, so it goes last.
func Clock(schedule string) string {
	before, _, found := strings.Cut(strings.TrimSpace(schedule), Also)
	if !found {
		if Watches(schedule) {
			return ""
		}
		return strings.TrimSpace(schedule)
	}
	return strings.TrimSpace(before)
}

// Trigger : The part of a schedule that waits for something, or
// empty.
func Trigger(schedule string) string {
	_, after, found := strings.Cut(strings.TrimSpace(schedule), Also)
	if !found {
		if Watches(schedule) {
			return strings.TrimSpace(schedule)
		}
		return ""
	}
	return strings.TrimSpace(after)
}

// Watches : Whether this schedule waits for something to happen as
// well as, or instead of, coming round on a clock.
func Watches(schedule string) bool {
	for _, part := range strings.Split(strings.TrimSpace(schedule), Also) {
		fields := strings.Fields(strings.ToLower(strings.TrimSpace(part)))
		if len(fields) > 0 && fields[0] == When {
			return true
		}
	}
	return false
}

// Ticks : Whether this schedule also comes round on a clock.
func Ticks(schedule string) bool { return Clock(schedule) != "" }

// Then : What separates a trigger from how long to wait after it.
//
//	when power.plugged is true then 15m
//
// Spaced on both sides so it cannot be mistaken for part of a
// value, and a plain word because these are dictated aloud.
const Then = " then "

// Day, Week : Lengths of time Go does not parse.
//
// time.ParseDuration stops at hours, because beyond that a day is
// not always twenty-four of them. Here it is: these are rules, and
// "quiet for a week" is the thing somebody actually says. A week
// of clock time is near enough, and the alternative is asking them
// for 168h.
const (
	oneDay  = 24 * time.Hour
	oneWeek = 7 * oneDay
)

// Span : A length of time as somebody would write it.
//
// Everything Go reads, plus days and weeks: "30m", "2h", "7d",
// "1w". One function so that every length on a rule -- how long to
// wait, how long to keep what it says, how long to stay quiet --
// is written the same way.
func Span(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("%w: no length of time", ErrUnreadable)
	}
	for suffix, unit := range map[string]time.Duration{"d": oneDay, "w": oneWeek} {
		number, found := strings.CutSuffix(s, suffix)
		if !found {
			continue
		}
		many, err := strconv.ParseFloat(number, 64)
		if err != nil || many <= 0 {
			return 0, fmt.Errorf("%w: %q is not a length of time", ErrUnreadable, s)
		}
		return time.Duration(many * float64(unit)), nil
	}
	long, err := time.ParseDuration(s)
	if err != nil || long <= 0 {
		return 0, fmt.Errorf("%w: %q is not a length of time", ErrUnreadable, s)
	}
	return long, nil
}

// Waits : How long a trigger waits before running, or zero.
//
// Owner, 7 October 2026, on what the engine could not do: *"it
// can't wait."*
//
// Some questions cannot be answered at the moment they are asked.
// Whether a phone is actually charging cannot be known when it is
// plugged in -- only fifteen minutes later, by whether the level
// moved. Whether a missed call mattered cannot be known as it
// comes in, only once nobody has rung back.
//
// Both were possible before only by asking every few minutes for
// ever, which is a loop that runs seven hundred times a day to
// catch the two occasions it is about.
//
// Nothing sleeps. A trigger with a wait on it does not run when it
// fires: it sets its own next time, and the clock that was always
// there picks it up. So a rule waiting a fortnight costs exactly
// what a rule waiting a minute costs, which is a row.
func Waits(schedule string) (time.Duration, error) {
	_, after, found := strings.Cut(strings.TrimSpace(schedule), Then)
	if !found {
		return 0, nil
	}
	wait, err := Span(after)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a length of time to wait",
			ErrUnreadable, strings.TrimSpace(after))
	}
	return wait, nil
}

// Watched : The condition a watching rule waits for.
//
// Narrower than a check can be. A trigger must name an event kind,
// because an arriving reading is what sets it off: presence has no
// arrival here, and a rule about how old a reading is could never
// fire on one landing, since landing is what makes it new.
func Watched(schedule string) (Gate, error) {
	// The trigger half, where a rule has a clock as well.
	schedule = Trigger(schedule)

	// Whatever follows "then" is how long to wait, not part of the
	// condition. Taken off before the fields are counted so that a
	// trigger reads the same with a wait on it and without.
	schedule, _, _ = strings.Cut(strings.TrimSpace(schedule), Then)

	fields := strings.Fields(strings.TrimSpace(schedule))
	if len(fields) < 2 || !strings.EqualFold(fields[0], When) {
		return Gate{}, fmt.Errorf("%w: %q", ErrUnreadableGate, schedule)
	}
	gate, err := ParseGate(strings.Join(fields[1:], " "))
	if err != nil {
		return Gate{}, err
	}
	if !gate.Arrives() {
		return Gate{}, fmt.Errorf(
			"%w: a rule can only wait for an event, and %q is not one",
			ErrUnreadableGate, gate.Source)
	}
	if gate.Op == Older || gate.Op == Within {
		return Gate{}, fmt.Errorf(
			"%w: a reading landing is what sets a rule off, so %q could never be true when it does",
			ErrUnreadableGate, gate.Op)
	}
	return gate, nil
}

// Following : When a schedule next comes round after a moment.
//
// In the person's own zone, because "at 08:00" means eight where
// they are standing and nowhere else.
func Following(schedule string, after time.Time, where *time.Location) (time.Time, error) {
	if where == nil {
		where = time.UTC
	}
	// The clock half, where a rule has a trigger as well.
	schedule = Clock(schedule)
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(schedule)))
	if len(fields) < 2 {
		return time.Time{}, ErrUnreadable
	}

	switch fields[0] {
	case Every:
		gap, err := time.ParseDuration(fields[1])
		if err != nil || gap <= 0 {
			return time.Time{}, ErrUnreadable
		}
		return after.Add(gap), nil

	case At:
		clock, err := time.Parse("15:04", fields[1])
		if err != nil {
			return time.Time{}, ErrUnreadable
		}
		here := after.In(where)
		next := time.Date(here.Year(), here.Month(), here.Day(),
			clock.Hour(), clock.Minute(), 0, 0, where)
		if !next.After(here) {
			next = next.AddDate(0, 0, 1)
		}
		return next, nil

	case Once:
		when := strings.Join(fields[1:], " ")
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02t15:04"} {
			if at, err := time.ParseInLocation(layout, when, where); err == nil {
				return at, nil
			}
		}
		return time.Time{}, ErrUnreadable
	}
	return time.Time{}, fmt.Errorf("%w: %q", ErrUnreadable, fields[0])
}

// Repeats : Whether this schedule comes round again.
func Repeats(schedule string) bool {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(Clock(schedule))))
	return len(fields) > 0 && (fields[0] == Every || fields[0] == At)
}

// Valid : Whether a step is one the runner can carry out.
//
// Checked when a job is written rather than when it runs. A rule
// that cannot work should fail in front of whoever wrote it, not
// silently at three in the morning.
func (s Step) Valid() error {
	switch s.Do {
	case Announce:
		if strings.TrimSpace(s.Say) == "" {
			return errors.New("job: an announcement with nothing to say")
		}
	case Call:
		if strings.TrimSpace(s.Tool) == "" {
			return errors.New("job: a tool step naming no tool")
		}
	case Relay:
		// Nothing of its own to check: what it says comes from the
		// step before it, and a relay with no tool in front of it
		// simply finds nothing and stops.
	case Write:
		if strings.TrimSpace(s.Ask) == "" {
			return errors.New("job: a write with nothing to write about")
		}
	case Check:
		if strings.TrimSpace(s.When) == "" {
			return errors.New("job: a check with no condition")
		}
		if _, err := ParseConditions(s.When); err != nil {
			return err
		}
	default:
		return fmt.Errorf("job: %q is not something a step can do", s.Do)
	}
	return nil
}
