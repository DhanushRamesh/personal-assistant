package job

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A rule in two halves: when to look and whether it holds, and what
// to do when it does.
//
// Owner, 9 October 2026: *"a rule object should be like {"when":
// ..., "perform": ...}"*, and on a mini-language inside one of its
// fields: *"it's more string and an AI can make a mistake."*
//
// The old shape kept the same grammar in two places for no reason
// -- a trigger in the schedule, conditions in check steps -- and
// stored both as text to be parsed each time. This stores the
// structure and renders the text, which is the way round that
// suits the thing that will be writing most of these: a model can
// fill in keys, and cannot typo a grammar that is not there.
//
// A condition can only ever read a source, never what the step
// before it returned, so every condition gave the same answer
// wherever it sat. That is why they all move here, and why
// `perform` has no check in it: nothing is left for one to do.

// Occasion : When a rule looks, and whether it holds when it
// does. The "when" half of a rule; named for the occasion rather
// than the word because [When] is already the schedule keyword.
type Occasion struct {
	// Wake : What sets the rule off. Several means several, and
	// any one of them fires it.
	//
	// May be left out when a condition reads something that
	// arrives, in which case those are the hooks. A When with
	// neither is refused: nothing would ever wake it.
	Wake []Hook `json:"wake,omitempty"`
	// All, Any : The conditions. Exactly one of the two.
	All []Where `json:"all,omitempty"`
	Any []Where `json:"any,omitempty"`
}

// Hook : One thing that wakes a rule.
//
// Exactly one of the clock forms, or On. After is the wait before
// it runs, and belongs to the hook rather than the rule because
// two hooks on one rule may want different ones.
type Hook struct {
	Every string `json:"every,omitempty"`
	At    string `json:"at,omitempty"`
	Once  string `json:"once,omitempty"`
	Cron  string `json:"cron,omitempty"`
	On    *Where `json:"on,omitempty"`
	After string `json:"after,omitempty"`
}

// Where : A condition, or a group of them.
//
// One struct for both because a group is a condition as far as
// anything reading the tree is concerned, and a Go union of two
// shapes costs more at the JSON boundary than it saves. Which of
// the two it is, is decided by [Where.Group].
type Where struct {
	// All, Any : A group. Nested freely.
	All []Where `json:"all,omitempty"`
	Any []Where `json:"any,omitempty"`

	// Source : An event kind, or one of the derived sources --
	// presence, hour, day, usually.*, link.*, ha.*.
	Source string `json:"source,omitempty"`
	// Count : How often a thing happened, instead of a source.
	Count *Counted `json:"count,omitempty"`
	// Pattern, With : A named shape worked out in Go, instead of
	// a source. Only its yes or no is used here.
	Pattern string         `json:"pattern,omitempty"`
	With    map[string]any `json:"with,omitempty"`

	// Op : ==, !=, <, >, <=, >=, arrives, older_than, within.
	Op string `json:"op,omitempty"`
	// Value : A number or a word. Left out for arrives.
	Value any `json:"value,omitempty"`
	// Ago : Compare the reading with its own earlier self, this
	// far back.
	//
	// Its own key and not a value. "15m ago" in a value field
	// looked like an ordinary value, so writing "15m" by mistake
	// gave a rule that parsed, ran, and compared the battery
	// against the number fifteen.
	Ago string `json:"ago,omitempty"`
}

// Counted : How often a thing happened, as a rule holds it.
//
// The written form of [Count], which is what the resolver works
// in. Two shapes of one idea: this one is keys, that one is a
// duration and a flag.
type Counted struct {
	// Kind : The event kind to count.
	Kind string `json:"kind"`
	// Is : Only those whose value is this. Empty counts them all.
	Is string `json:"is,omitempty"`
	// By : Count each distinct value separately and answer with
	// the largest. The only thing it may be is "value", because
	// the value is the only part of a payload the engine reads.
	By string `json:"by,omitempty"`
	// Within : How far back to look.
	Within string `json:"within"`
}

// Act : One thing to do, once a rule holds.
//
// Linear, with no names and no references. A branch here would be
// a second rule wearing a disguise, and two rules with opposite
// conditions can be read off a screen where a nested sequence
// cannot.
type Act struct {
	// Do : tool, pattern, write, say or relay.
	Do string `json:"do"`
	// Use : Which tool or which pattern.
	Use string `json:"use,omitempty"`
	// With : What to run it with.
	With json.RawMessage `json:"with,omitempty"`
	// Ask : What to write about, for a write.
	Ask string `json:"ask,omitempty"`
	// Tools : What the model may call for itself, on a write.
	Tools []string `json:"tools,omitempty"`
	// Text : The words, for a say.
	Text string `json:"text,omitempty"`
}

// What an Act may be.
const (
	DoTool    = "tool"
	DoPattern = "pattern"
	DoWrite   = "write"
	DoSay     = "say"
	DoRelay   = "relay"
)

// The operators as a structured rule writes them. The word forms
// stay readable and are what a dictated rule is rendered back as;
// these are what is stored.
const (
	Equals    = "=="
	NotEquals = "!="
	OlderThan = "older_than"
)

// symbols : The word form of each operator, as it is stored.
var symbols = map[string]string{
	Is:    Equals,
	IsNot: NotEquals,
	Older: OlderThan,
}

// words : The spoken form of each stored operator, for reading a
// rule back to the person who dictated it.
var words = map[string]string{
	Equals:    Is,
	NotEquals: IsNot,
	OlderThan: Older,
}

// Group : Whether this is a group rather than a condition.
func (w Where) Group() bool { return len(w.All) > 0 || len(w.Any) > 0 }

// Reshape : A rule written the old way, as a When and a sequence.
//
// The whole of the conversion, in one function that can be run
// against every live rule and its answer read before anything is
// written down. A check step becomes a condition; everything else
// stays a step in the order it was in.
func Reshape(j Job) (Occasion, []Act, error) {
	var when Occasion

	if clock := Clock(j.Schedule); clock != "" {
		hook, err := hookOf(clock)
		if err != nil {
			return Occasion{}, nil, err
		}
		when.Wake = append(when.Wake, hook)
	}
	if Watches(j.Schedule) {
		gate, err := Watched(j.Schedule)
		if err != nil {
			return Occasion{}, nil, err
		}
		leaf, err := leafOf(gate)
		if err != nil {
			return Occasion{}, nil, err
		}
		hook := Hook{On: &leaf}
		if wait, err := Waits(j.Schedule); err == nil && wait > 0 {
			_, after, _ := strings.Cut(j.Schedule, Then)
			hook.After = strings.TrimSpace(after)
		}
		when.Wake = append(when.Wake, hook)
	}

	acts := make([]Act, 0, len(j.Steps))
	for _, step := range j.Steps {
		if step.Do == Check {
			conditions, err := ParseConditions(step.When)
			if err != nil {
				return Occasion{}, nil, err
			}
			where, err := whereOf(conditions)
			if err != nil {
				return Occasion{}, nil, err
			}
			when.All = append(when.All, where)
			continue
		}
		act, err := actOf(step)
		if err != nil {
			return Occasion{}, nil, err
		}
		acts = append(acts, act)
	}
	return when, acts, nil
}

// hookOf : A clock, as a hook.
func hookOf(clock string) (Hook, error) {
	word, rest, found := strings.Cut(strings.TrimSpace(clock), " ")
	if !found {
		return Hook{}, fmt.Errorf("%w: %q", ErrUnreadable, clock)
	}
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(word) {
	case Every:
		return Hook{Every: rest}, nil
	case At:
		return Hook{At: rest}, nil
	case Once:
		return Hook{Once: rest}, nil
	case Cron:
		return Hook{Cron: rest}, nil
	}
	return Hook{}, fmt.Errorf("%w: %q", ErrUnreadable, clock)
}

// whereOf : One check step's condition, as a leaf or a group.
//
// Alternatives joined by "or" become an any; a single condition
// stays a condition, because wrapping one thing in a group is a
// layer for a reader to see through and nothing else.
func whereOf(c Conditions) (Where, error) {
	if len(c) == 1 {
		return leafOf(c[0])
	}
	group := Where{Any: make([]Where, 0, len(c))}
	for _, gate := range c {
		leaf, err := leafOf(gate)
		if err != nil {
			return Where{}, err
		}
		group.Any = append(group.Any, leaf)
	}
	return group, nil
}

// leafOf : One gate, as a condition.
func leafOf(g Gate) (Where, error) {
	where := Where{Op: g.Op}
	if sym, found := symbols[g.Op]; found {
		where.Op = sym
	}

	switch {
	case Counts(g.Source):
		count, err := ReadCount(g.Source)
		if err != nil {
			return Where{}, err
		}
		where.Count = &Counted{Kind: count.Kind, Is: count.Match, Within: short(count.Window)}
		if count.Group {
			where.Count.By = Value
		}
	default:
		where.Source = g.Source
	}

	if g.Op == Any {
		return where, nil
	}
	// A comparison with its own past is a key, not a value.
	if back, against := g.Against(); against {
		where.Ago = short(back)
		return where, nil
	}
	if n, err := strconv.Atoi(strings.TrimSpace(g.Value)); err == nil {
		where.Value = n
		return where, nil
	}
	where.Value = g.Value
	return where, nil
}

// actOf : One step, as an act.
func actOf(s Step) (Act, error) {
	switch s.Do {
	case Call:
		return Act{Do: DoTool, Use: s.Tool, With: s.Args}, nil
	case Write:
		return Act{Do: DoWrite, Ask: s.Ask, Tools: s.Tools}, nil
	case Announce:
		return Act{Do: DoSay, Text: s.Say}, nil
	case Relay:
		return Act{Do: DoRelay}, nil
	}
	return Act{}, fmt.Errorf("job: nothing carries out %q", s.Do)
}

// short : A length of time as somebody would write it, so that a
// converted rule reads as the one it came from.
//
// Go writes twenty minutes as "20m0s" and two hours as "2h0m0s",
// and trimming the tails off those is how "20m" first came out as
// "20" -- the string ends in "0m" and so does the unit. Each unit
// is taken on its own instead, which cannot do that.
func short(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d >= time.Minute && d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d >= time.Second && d%time.Second == 0:
		return strconv.Itoa(int(d/time.Second)) + "s"
	}
	return d.String()
}
