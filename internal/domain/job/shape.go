package job

import (
	"encoding/json"
	"fmt"
	"math"
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

// known : Every operator, by the word the parser reads it as.
var known = map[string]bool{
	Is: true, IsNot: true, Less: true, More: true,
	AtMost: true, AtLeast: true, Any: true, Older: true, Within: true,
}

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

// Gate : One condition, as the machinery that reads conditions
// already understands it.
//
// The whole of how a structured rule is evaluated, and the reason
// it was safe to change the shape at all: every operator, every
// source, the counting and the Home Assistant reading are already
// written and tested against [Gate], and none of them had to know
// that a rule now arrives as a tree. The tree walking is new; what
// is at the bottom of it is not.
//
// Refuses a group, which has no single condition to be.
func (w Where) Gate() (Gate, error) {
	if w.Group() {
		return Gate{}, fmt.Errorf("%w: a group is not one condition", ErrUnreadableGate)
	}
	gate := Gate{Op: w.Op}
	// Stored as a symbol, read as the word the parser knows.
	if word, found := words[w.Op]; found {
		gate.Op = word
	}
	// Checked here and nowhere else, because this is the only door
	// an unknown one can come through. Written as a sentence it was
	// impossible -- the parser recognised the operators and refused
	// everything else -- but a structured rule hands the operator
	// over as a string, and "op": "nonsense" was accepted and made
	// into a rule that could never hold.
	if !known[gate.Op] {
		return Gate{}, fmt.Errorf("%w: there is nothing a condition does called %q",
			ErrUnreadableGate, w.Op)
	}

	switch {
	case w.Count != nil:
		gate.Source = w.Count.Source()
	case w.Pattern != "":
		return Gate{}, fmt.Errorf("%w: a pattern is not a condition yet", ErrUnreadableGate)
	default:
		gate.Source = strings.ToLower(strings.TrimSpace(w.Source))
	}
	if gate.Source == "" {
		return Gate{}, fmt.Errorf("%w: a condition with nothing to read", ErrUnreadableGate)
	}

	switch {
	case gate.Op == Any:
	case w.Ago != "":
		gate.Value = w.Ago + Ago
	case w.Value != nil:
		gate.Value = value(w.Value)
	default:
		return Gate{}, fmt.Errorf("%w: %q has nothing to compare with",
			ErrUnreadableGate, gate.Source)
	}
	if err := gate.sound(); err != nil {
		return Gate{}, err
	}
	return gate, nil
}

// Source : A count as the one line the resolver reads.
//
// Rendered rather than stored, so that the string nothing had ever
// seen exists only for the moment it takes to answer, and never
// anywhere a model could be asked to write it.
func (c Counted) Source() string {
	out := Counting + " " + c.Kind
	if c.Is != "" {
		out += " " + Is + " " + c.Is
	}
	if c.By != "" {
		out += " " + By + " " + c.By
	}
	return out + " " + Within + " " + c.Within
}

// value : A number or a word, as the comparison reads it.
//
// JSON gives a number back as a float, and 20 that renders as "20"
// and not "20.000000" is the whole of what this is for.
func value(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case string:
		return n
	case float64:
		if n == math.Trunc(n) {
			return strconv.FormatInt(int64(n), 10)
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	case int:
		return strconv.Itoa(n)
	case bool:
		return strconv.FormatBool(n)
	}
	return fmt.Sprint(v)
}

// Needs : Every source the conditions read, once each.
func (o Occasion) Needs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, w := range o.where() {
		w.needs(seen, &out)
	}
	return out
}

// needs : This condition's source, and its children's.
func (w Where) needs(seen map[string]bool, out *[]string) {
	if w.Group() {
		for _, in := range append(append([]Where{}, w.All...), w.Any...) {
			in.needs(seen, out)
		}
		return
	}
	gate, err := w.Gate()
	if err != nil {
		return
	}
	kind := gate.Needs()
	if kind == "" || seen[kind] {
		return
	}
	seen[kind] = true
	*out = append(*out, kind)
}

// Deep : How far back any condition looks, for the reader to fetch
// enough readings to answer it.
func (o Occasion) Deep() time.Duration {
	var most time.Duration
	for _, w := range o.where() {
		if back := w.deep(); back > most {
			most = back
		}
	}
	return most
}

// deep : How far back this condition looks, and its children's.
func (w Where) deep() time.Duration {
	if w.Group() {
		var most time.Duration
		for _, in := range append(append([]Where{}, w.All...), w.Any...) {
			if back := in.deep(); back > most {
				most = back
			}
		}
		return most
	}
	if w.Ago == "" {
		return 0
	}
	back, err := time.ParseDuration(w.Ago)
	if err != nil {
		return 0
	}
	return back
}

// Holds : Whether the conditions hold, and the first that did not.
//
// The reason why, not just the answer. A rule that said nothing
// has to be able to say which condition stopped it, and a tree
// that answered only yes or no would make the page that shows the
// engine working useless at exactly the moment somebody reads it.
//
// An Occasion with no conditions holds: it is a rule that runs
// whenever it wakes.
func (o Occasion) Holds(got map[string][]Reading, away bool, now time.Time) (bool, string) {
	if len(o.Any) > 0 {
		for _, w := range o.Any {
			if ok, _ := w.holds(got, away, now); ok {
				return true, ""
			}
		}
		return false, "none of the conditions held"
	}
	for _, w := range o.All {
		if ok, why := w.holds(got, away, now); !ok {
			return false, why
		}
	}
	return true, ""
}

// holds : Whether this condition holds, and why not.
func (w Where) holds(got map[string][]Reading, away bool, now time.Time) (bool, string) {
	if len(w.All) > 0 {
		for _, in := range w.All {
			if ok, why := in.holds(got, away, now); !ok {
				return false, why
			}
		}
		return true, ""
	}
	if len(w.Any) > 0 {
		for _, in := range w.Any {
			if ok, _ := in.holds(got, away, now); ok {
				return true, ""
			}
		}
		return false, "none of " + w.Describe() + " held"
	}

	gate, err := w.Gate()
	if err != nil {
		// Unreadable is not the same as not holding, and the
		// difference matters: one is a rule that is waiting and
		// the other is a rule that can never work. Both withhold.
		return false, "a condition could not be read: " + err.Error()
	}
	if gate.Holds(got[gate.Needs()], away, now) {
		return true, ""
	}
	return false, "the condition " + w.Describe() + " did not hold"
}

// Describe : A condition in the words somebody would say, for
// reading a rule back and for saying which one stopped it.
func (w Where) Describe() string {
	if w.Group() {
		parts := make([]string, 0, len(w.All)+len(w.Any))
		join := " " + strings.TrimSpace(Or) + " "
		in := w.Any
		if len(w.All) > 0 {
			in, join = w.All, " and "
		}
		for _, one := range in {
			parts = append(parts, one.Describe())
		}
		return "(" + strings.Join(parts, join) + ")"
	}
	gate, err := w.Gate()
	if err != nil {
		return "something unreadable"
	}
	out := gate.Source + " " + gate.Op
	if gate.Op == Any {
		return out
	}
	return out + " " + gate.Value
}

// where : Whichever list of conditions this Occasion has.
func (o Occasion) where() []Where {
	if len(o.Any) > 0 {
		return o.Any
	}
	return o.All
}

// Schedule : A hook as the clock already reads it, or empty when
// it is not a clock.
func (h Hook) Schedule() string {
	switch {
	case h.Every != "":
		return Every + " " + h.Every
	case h.At != "":
		return At + " " + h.At
	case h.Once != "":
		return Once + " " + h.Once
	case h.Cron != "":
		return Cron + " " + h.Cron
	}
	return ""
}

// Waits : How long this hook holds off before running, or zero.
func (h Hook) Waits() (time.Duration, error) {
	if strings.TrimSpace(h.After) == "" {
		return 0, nil
	}
	return Span(h.After)
}

// Ticking : The clock hooks, in the order they were written.
func (o Occasion) Ticking() []Hook {
	out := make([]Hook, 0, len(o.Wake))
	for _, h := range o.Wake {
		if h.Schedule() != "" {
			out = append(out, h)
		}
	}
	return out
}

// Watching : The event hooks, as gates, with the wait each carries.
//
// Several, where the old shape could hold one. "when the door
// opens or the window opens" was two rules because a schedule was
// one sentence and a sentence has one trigger in it; here it is
// one rule with two hooks, and either fires it.
func (o Occasion) Watching() ([]Hook, []Gate, error) {
	hooks := make([]Hook, 0, len(o.Wake))
	gates := make([]Gate, 0, len(o.Wake))
	for _, h := range o.Wake {
		if h.On == nil {
			continue
		}
		gate, err := h.On.Gate()
		if err != nil {
			return nil, nil, err
		}
		if !gate.Arrives() {
			return nil, nil, fmt.Errorf(
				"%w: a rule can only wait for an event, and %q is not one",
				ErrUnreadableGate, gate.Source)
		}
		if gate.Op == Older || gate.Op == Within {
			return nil, nil, fmt.Errorf(
				"%w: a reading landing is what sets a rule off, so %q could never be true when it does",
				ErrUnreadableGate, gate.Op)
		}
		hooks = append(hooks, h)
		gates = append(gates, gate)
	}
	return hooks, gates, nil
}

// Next : When this rule next comes round, or the zero time when no
// clock sets it off.
//
// The earliest of its clocks. Several hooks mean any one of them
// fires the rule, so the next moment is the soonest of them, not
// the first one written.
func (o Occasion) Next(after time.Time, where *time.Location) (time.Time, error) {
	var soonest time.Time
	for _, h := range o.Ticking() {
		at, err := Following(h.Schedule(), after, where)
		if err != nil {
			return time.Time{}, err
		}
		if soonest.IsZero() || at.Before(soonest) {
			soonest = at
		}
	}
	return soonest, nil
}

// Sound : Whether this is a rule that could ever run, and why not.
//
// Asked when a rule is written rather than when it runs, because
// the failure this codebase has least patience with is a rule that
// never fires and never says why. A tree with no clock and nothing
// that arrives is exactly that: every condition in it is asked, so
// nothing would ever think to ask them.
func (o Occasion) Sound() error {
	if len(o.All) > 0 && len(o.Any) > 0 {
		return fmt.Errorf("%w: a rule is all of its conditions or any of them, not both",
			ErrUnreadableGate)
	}
	for _, w := range o.where() {
		if err := w.sound(); err != nil {
			return err
		}
	}
	hooks, _, err := o.Watching()
	if err != nil {
		return err
	}
	if len(o.Ticking()) == 0 && len(hooks) == 0 {
		return fmt.Errorf("%w: nothing would ever wake this rule", ErrUnreadable)
	}
	for _, h := range o.Wake {
		if _, err := h.Waits(); err != nil {
			return err
		}
		if h.Schedule() != "" && h.On != nil {
			return fmt.Errorf("%w: a hook is a clock or an event, not both", ErrUnreadable)
		}
	}
	return nil
}

// sound : Whether this condition could ever hold.
func (w Where) sound() error {
	if len(w.All) > 0 && len(w.Any) > 0 {
		return fmt.Errorf("%w: a group is all of its conditions or any of them, not both",
			ErrUnreadableGate)
	}
	if w.Group() {
		for _, in := range append(append([]Where{}, w.All...), w.Any...) {
			if err := in.sound(); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := w.Gate()
	return err
}

// Describe : The conditions in the words somebody would say, for
// reading a rule back and for the record of a run that said
// nothing.
func (o Occasion) Describe() string {
	where := o.where()
	if len(where) == 0 {
		return "nothing to check"
	}
	join := " and "
	if len(o.Any) > 0 {
		join = " " + strings.TrimSpace(Or) + " "
	}
	parts := make([]string, 0, len(where))
	for _, w := range where {
		parts = append(parts, w.Describe())
	}
	return strings.Join(parts, join)
}

// Repeats : Whether any of this rule's clocks comes round again.
//
// A once is finished when it has run; an every, an at or a cron is
// not. A rule with both is not finished until the once is the only
// one left, which is why this asks whether any of them repeats
// rather than whether the first does.
func (o Occasion) Repeats() bool {
	for _, h := range o.Ticking() {
		if Repeats(h.Schedule()) {
			return true
		}
	}
	return false
}

// Written : The hooks as the sentence they are read back as.
//
// The structure is what is stored and what a model writes; this is
// what a person hears when they ask what rules they have. "every
// 30m also when battery.level < 20" is what they said, and it is
// what they should be told.
func (o Occasion) Written() string {
	parts := make([]string, 0, len(o.Wake))
	for _, h := range o.Wake {
		if clock := h.Schedule(); clock != "" {
			parts = append(parts, clock)
			continue
		}
		if h.On == nil {
			continue
		}
		said := When + " " + h.On.Describe()
		if strings.TrimSpace(h.After) != "" {
			said += Then + h.After
		}
		parts = append(parts, said)
	}
	return strings.Join(parts, Also)
}

// Steps : The acts as the older shape held them.
//
// Kept so the column that held a rule before today goes on holding
// one, and so the page that lists rules and the record of a run
// read the same whichever shape a rule arrived in.
func (o Occasion) Steps(acts []Act) []Step {
	out := make([]Step, 0, len(acts)+len(o.where()))
	for _, w := range o.where() {
		out = append(out, Step{Do: Check, When: w.Describe()})
	}
	for _, a := range acts {
		switch a.Do {
		case DoTool:
			out = append(out, Step{Do: Call, Tool: a.Use, Args: a.With})
		case DoWrite:
			out = append(out, Step{Do: Write, Ask: a.Ask, Tools: a.Tools})
		case DoSay:
			out = append(out, Step{Do: Announce, Say: a.Text})
		case DoRelay:
			out = append(out, Step{Do: Relay})
		}
	}
	return out
}

// Speaks : Whether any of these acts says anything.
func Speaks(acts []Act) bool {
	for _, a := range acts {
		switch a.Do {
		case DoWrite, DoSay, DoRelay:
			return true
		}
	}
	return false
}

// Make : A rule from its two halves.
//
// What [New] is for a rule written as sentences. The structured
// halves are the truth; the schedule and the steps are rendered
// from them, so a rule written either way sits in the table the
// same and reads back the same.
func Make(id, userID, name, kind string, when Occasion, acts []Act, from time.Time) (Job, error) {
	if strings.TrimSpace(name) == "" {
		return Job{}, fmt.Errorf("%w: a rule needs a name", ErrUnreadable)
	}
	if len(acts) == 0 {
		return Job{}, ErrNoSteps
	}
	if err := when.Sound(); err != nil {
		return Job{}, err
	}
	for _, a := range acts {
		switch a.Do {
		case DoTool, DoPattern, DoWrite, DoSay, DoRelay:
		default:
			return Job{}, fmt.Errorf("job: nothing carries out %q", a.Do)
		}
	}

	j := Job{
		ID: id, UserID: userID, Name: name, Kind: kind,
		Schedule: when.Written(), Steps: when.Steps(acts),
		When: when, Perform: acts, State: Active,
	}
	// A watching rule has no first moment: it waits for a reading.
	if len(when.Ticking()) > 0 {
		next, err := when.Next(from, from.Location())
		if err != nil {
			return Job{}, err
		}
		j.Next = next
	}
	return j, nil
}
