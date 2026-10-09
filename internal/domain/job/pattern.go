package job

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Patterned : What every gate source that asks a pattern is called.
//
//	{"pattern": "reaching", "with": {"tries": 2}}
//
// Owner, on sizing the engine: *"I'll make the AI create rules
// based on the patterns, so it should not have limitations"* -- and
// on how: *predefined templates*. A template is a named shape
// worked out in Go, with blanks a rule fills in. The determinism
// stays where it can be tested; what moves to the engine is
// choosing a shape and filling it.
//
// It is a source rather than a new kind of condition, and that is
// the whole of why this was a small change. Conditions are already
// evaluated by naming what they need, fetching it, and judging the
// readings -- so a pattern names itself, something computes it, and
// the judging is the code that was already there. Nothing in the
// tree walking, the grouping, the operators or the describing had
// to learn a second kind of leaf.
//
// What it answers is yes or no. A pattern that wanted to return a
// sentence would be a second way of saying things, and there are
// two already.
const Patterned = "pattern."

// Yes, No : What a pattern answers.
//
// Words rather than a bool, because a reading is text and every
// other source is already compared as text. A rule may say so --
// `{"pattern": "reaching", "op": "==", "value": "no"}` asks for
// the absence -- and a rule that says nothing means yes, which is
// what somebody writing `{"pattern": "reaching"}` plainly intends.
const (
	Yes = "yes"
	No  = "no"
)

// Patterns : What works out a named shape.
//
// Supplied from outside for the same reason the readings are: the
// shapes need events, people and whatever else the application
// holds, and the engine knows about none of it.
type Patterns interface {
	// Match : Whether the named shape holds for this person.
	//
	// The blanks are as the rule wrote them. An unknown name is an
	// error rather than a no: a rule standing on a pattern that
	// does not exist is broken, not unsatisfied, and the two must
	// never read alike.
	Match(ctx context.Context, userID, name string, with map[string]any) (bool, error)
}

// source : A pattern leaf as one string, the same every time.
//
// Sorted, because a map has no order and two rules that ask the
// same thing must name the same source or each would be worked
// out twice and cached as two.
func patternSource(name string, with map[string]any) string {
	out := Patterned + strings.ToLower(strings.TrimSpace(name))
	if len(with) == 0 {
		return out
	}
	keys := make([]string, 0, len(with))
	for k := range with {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out += " " + strings.ToLower(k) + "=" + value(with[k])
	}
	return out
}

// Pattern : The name and blanks in a pattern source, read back.
//
// The other half of [patternSource], for whatever has to work the
// shape out from the source string the gate carries.
func Pattern(source string) (name string, with map[string]any, err error) {
	if !IsPattern(source) {
		return "", nil, fmt.Errorf("%w: %q is not a pattern", ErrUnreadableGate, source)
	}
	fields := strings.Fields(strings.TrimPrefix(source, Patterned))
	if len(fields) == 0 || fields[0] == "" {
		return "", nil, fmt.Errorf("%w: a pattern with no name", ErrUnreadableGate)
	}
	with = map[string]any{}
	for _, f := range fields[1:] {
		k, v, found := strings.Cut(f, "=")
		if !found || k == "" {
			return "", nil, fmt.Errorf("%w: %q is not a blank a pattern fills in", ErrUnreadableGate, f)
		}
		with[k] = v
	}
	return fields[0], with, nil
}

// IsPattern : Whether a gate source asks a pattern.
func IsPattern(source string) bool {
	return strings.HasPrefix(source, Patterned)
}
