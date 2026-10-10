// Package noticing lets the assistant write his own rules.
//
// Owner, on what the rule engine is for: *"the main purpose of the
// rule engine is not for me to create rules. He should understand
// the pattern and create his own rules, so that next time a server
// event arrives the rule is triggered, then Jarvis is hit with a
// trigger and answers to me."*
//
// Everything on both sides of that already worked. The engine wakes
// him when something happens; the description tells him what is
// ordinary and what has moved. What was missing was the middle: all
// twenty-five rules on the system existed because somebody asked
// for one out loud, and nothing ever decided by itself that a thing
// was worth being told about.
//
// This is that decision and nothing else. It reads what he knows
// about the person and what rules he already keeps, and either asks
// for one rule in a sentence or says there is nothing worth having.
// The sentence then goes to the writer that already exists, which
// turns it into a rule and refuses it if it cannot.
//
// Two models and not one, deliberately. Judging whether a pattern
// deserves a rule and rendering a rule as valid JSON are different
// jobs, and the writer is already good at the second.
package noticing

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/domain/job"
)

// Ask : One question put to a model.
type Ask func(ctx context.Context, system, ask string) (string, error)

// Most : How many rules he may write himself in a day.
//
// One. A model asked every hour whether anything deserves a rule
// will find something every hour, and four hundred rules by
// Christmas is not a companion paying attention -- it is a machine
// filling a table. One a day is more than the person would ever
// write themselves, and still leaves a week looking like
// consideration rather than output.
const Most = 1

// Rest : How long after writing one before he may write another.
//
// Separate from the daily count so that a day's allowance is not
// spent in the first two minutes after a restart.
const Rest = 6 * time.Hour

// Purpose : What one pass of the watcher is looking for.
//
// Two rules can both be worth having for opposite reasons. One
// catches something going wrong and is judged on whether hearing it
// would change what they do. The other says something about them
// and is judged on whether it is true and worth hearing, which is a
// different question and cannot share a bar with the first: the
// test that keeps the useful ones scarce -- that a remark they can
// do nothing with is not worth an interruption -- forbids every
// remark there is.
//
// So the bar, the kind and the allowance all belong to the purpose,
// and the machinery around them does not change.
type Purpose struct {
	// Judge : What the model is told when deciding.
	Judge string
	// Kind : How a rule written for this purpose is known.
	Kind string
	// Most : How many of this kind he may write in a day.
	Most int
	// Rest : How long after writing one before another.
	Rest time.Duration
	// Grounded : Whether a rule of this kind must read something
	// before it speaks.
	Grounded bool
}

// Useful, Companion : The two things he watches for.
//
// Companion is rarer by a wide margin and the reason is not
// caution about cost. A rule that catches the phone failing to
// charge may fire weekly and be welcome every time, because each
// firing is a different fact. A companion remarking on the same
// person's taste weekly is a machine performing attention, and the
// fourth one lands as noise however good the first was.
var (
	Useful = Purpose{
		Judge: judging, Kind: Kind,
		Most: Most, Rest: Rest,
	}
	Companion = Purpose{
		Judge: remarking, Kind: Remarked,
		Most: 1, Rest: 7 * 24 * time.Hour, Grounded: true,
	}
)

// Purposes : Every purpose, for the passes that treat his rules
// alike -- retiring them, listing them, counting what he holds.
func Purposes() []Purpose { return []Purpose{Useful, Companion} }

// Mine : Whether a rule is one he wrote himself, of any purpose.
func Mine(j job.Job) bool {
	for _, p := range Purposes() {
		if j.Kind == p.Kind {
			return true
		}
	}
	return false
}

// Watcher : What decides whether a pattern is worth a rule.
type Watcher struct {
	// Ask : The model that judges. Nil writes nothing, which is
	// the right behaviour for a server with no model configured.
	Ask Ask
	// Describe : What he knows about the person -- the standing
	// description, rhythms and trends.
	Describe func(ctx context.Context, userID string) string
	// Holding : The rules he already keeps, as he would read them.
	Holding func(ctx context.Context, userID string) (string, int, error)
	// Now : The clock. Nil is time.Now.
	Now func() time.Time
}

// Thought : What one pass decided.
type Thought struct {
	// Asked : The rule to write, in a sentence, or empty.
	Asked string
	// Name : What to call it.
	Name string
	// Why : What in the data prompted it, for the record.
	Why string
}

// Wondered : Whether anything is worth a rule, and what.
//
// Empty Asked is the ordinary answer and is not a failure. Most
// hours contain nothing worth being woken for, and a pass that felt
// obliged to produce something would be the whole problem.
func (w Watcher) Wondered(ctx context.Context, userID string, p Purpose) (Thought, error) {
	if w.Ask == nil || w.Describe == nil {
		return Thought{}, nil
	}
	known := strings.TrimSpace(w.Describe(ctx, userID))
	if known == "" {
		// Nothing is known about them yet, so nothing about them
		// is worth a standing rule.
		return Thought{}, nil
	}

	holding, count, err := "", 0, error(nil)
	if w.Holding != nil {
		holding, count, err = w.Holding(ctx, userID)
		if err != nil {
			return Thought{}, err
		}
	}

	answer, err := w.Ask(ctx, p.Judge, prompt.Block(
		prompt.Text(fmt.Sprintf("What you know about them (%d rules already kept):", count)),
		known,
		prompt.Text("The rules you already keep:"),
		holding,
	))
	if err != nil {
		return Thought{}, err
	}
	return read(answer)
}

// read : The model's answer, as a thought.
func read(answer string) (Thought, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return Thought{}, nil
	}
	// Fenced JSON is what models return when asked for JSON, and
	// refusing it would be refusing the commonest correct answer.
	answer = strings.TrimPrefix(answer, "```json")
	answer = strings.TrimPrefix(answer, "```")
	answer = strings.TrimSuffix(answer, "```")

	var out struct {
		Nothing string `json:"nothing"`
		Rule    string `json:"rule"`
		Name    string `json:"name"`
		Because string `json:"because"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(answer)), &out); err != nil {
		// A pass that cannot be read wrote nothing, which is the
		// safe way for this to fail: the cost of a missed rule is
		// one unremarked pattern, and the cost of a misread one is
		// a rule nobody asked for running for ever.
		return Thought{}, nil
	}
	if strings.TrimSpace(out.Rule) == "" {
		return Thought{}, nil
	}
	return Thought{
		Asked: strings.TrimSpace(out.Rule),
		Name:  strings.TrimSpace(out.Name),
		Why:   strings.TrimSpace(out.Because),
	}, nil
}

// Allowed : Whether he may write one now, given what he has written.
//
// Taken from the rules themselves rather than from a counter, so a
// restart does not hand him a fresh allowance.
func Allowed(mine []job.Job, p Purpose, now time.Time) bool {
	today, newest := 0, time.Time{}
	for _, j := range mine {
		// This purpose's own, and nobody else's. The two
		// allowances are separate so that a week of useful rules
		// cannot silence every remark, and a talkative week of
		// remarks cannot keep a rule that matters from being
		// written.
		//
		// Keyed on the kind alone. Why is overwritten on every
		// run, so an allowance that read it would hand him a
		// fresh one the moment his last rule fired.
		if j.Kind != p.Kind {
			continue
		}
		if j.Made.After(newest) {
			newest = j.Made
		}
		if now.Sub(j.Made) < 24*time.Hour {
			today++
		}
	}
	if today >= p.Most {
		return false
	}
	return newest.IsZero() || now.Sub(newest) >= p.Rest
}

// Kind, Mark : How a rule he wrote himself is known for what it is.
//
// Kept apart from the ones the person asked for, because the two are
// not the same thing to read in a list and are not the same thing to
// delete. Somebody deleting a rule they asked for is changing their
// mind; somebody deleting one he wrote is telling him he was wrong.
const (
	Kind = "noticed"
	Mark = "noticed: "

	// Remarked, RemarkedMark : The same, for a rule written to say
	// something about them rather than to catch something.
	//
	// Apart from the useful ones for the same reason those are
	// apart from the person's: retiring every remark is a decision
	// somebody might make in one go, and it should not take the
	// rule watching their battery with it.
	Remarked = "remarked"
)

// Settled : How long one of his rules must have stood before it can
// be reconsidered.
//
// A week. A rule written on Tuesday and retired on Wednesday is not
// a companion changing his mind, it is a machine thrashing -- and
// the thing it was written to catch may simply not have happened
// yet. Anything rarer than weekly is exactly what a standing rule
// is for.
const Settled = 7 * 24 * time.Hour

// Spoke : What one of his rules has actually said, for judging it.
type Spoke struct {
	// Job : The rule.
	Job job.Job
	// Ran, Said : How many times it ran, and how many of those it
	// had something to say.
	Ran, Said int
}

// Retired : Which of his own rules has stopped earning its place.
//
// Owner: *"he should create his own rules."* The other half of
// writing one is being able to take it back. A rule he wrote from a
// pattern that has since stopped should be retired by him and not
// by somebody finding it in a list months later -- which is the
// same gap as a memory that can never become wrong, in a third
// place.
//
// Only his own, and only after a week. Rules the person asked for
// are theirs to keep or drop, and he has no business tidying them
// away.
func (w Watcher) Retired(ctx context.Context, userID string, spoke []Spoke) (string, string, error) {
	if w.Ask == nil || len(spoke) == 0 {
		return "", "", nil
	}
	now := time.Now()
	if w.Now != nil {
		now = w.Now()
	}

	var lines []string
	byID := map[string]bool{}
	for _, s := range spoke {
		if !Mine(s.Job) || now.Sub(s.Job.Made) < Settled {
			continue
		}
		byID[s.Job.ID] = true
		lines = append(lines, fmt.Sprintf(
			"%s | %s | written %.0f days ago because %s | ran %d times, said something %d times",
			s.Job.ID, s.Job.Name, now.Sub(s.Job.Made).Hours()/24,
			s.Job.Because, s.Ran, s.Said))
	}
	if len(lines) == 0 {
		return "", "", nil
	}

	answer, err := w.Ask(ctx, retiring, prompt.Block(
		prompt.Text("What you know about them now:"),
		strings.TrimSpace(w.Describe(ctx, userID)),
		prompt.Text("The rules you wrote yourself:"),
		prompt.Lines(lines...),
	))
	if err != nil {
		return "", "", err
	}

	answer = strings.TrimSpace(answer)
	answer = strings.TrimPrefix(answer, "```json")
	answer = strings.TrimPrefix(answer, "```")
	answer = strings.TrimSuffix(answer, "```")
	var out struct {
		Keep   string `json:"keep"`
		Retire string `json:"retire"`
		Why    string `json:"because"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(answer)), &out); err != nil {
		return "", "", nil
	}
	id := strings.TrimSpace(out.Retire)
	// Only one of his own, and only one he was actually shown. A
	// model that names a rule it was not given has made one up, and
	// acting on that would let an unreadable answer delete
	// somebody's reminder.
	if id == "" || !byID[id] {
		return "", "", nil
	}
	return id, strings.TrimSpace(out.Why), nil
}
