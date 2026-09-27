package tool

import (
	"fmt"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Change : One thing that moved, and what it moved between.
type Change struct {
	// What : The name of the thing that changed, as a person would say
	// it: "the time", "what it says", "the name".
	What string
	// From, To : Its value before and after, already written the way it
	// should be read out.
	From string
	To   string
}

// Moved : Whether this is a change at all.
func (c Change) Moved() bool { return strings.TrimSpace(c.From) != strings.TrimSpace(c.To) }

// Changed : A write that was read back and found to have taken.
//
// The before and the after both go in the result, because the person is
// listening rather than looking and has no screen to compare against. A
// bare "changed it" leaves them to trust that the right thing moved in
// the right direction, which is exactly what they cannot check.
//
// [did] is what happened, in a few words: "Renamed the conversation".
func Changed(did string, changes ...Change) Result {
	var moved []Change
	for _, c := range changes {
		if c.Moved() {
			moved = append(moved, c)
		}
	}

	var b strings.Builder
	b.WriteString(did)
	if len(moved) == 0 {
		b.WriteString(". Read back afterwards and nothing had actually moved: it already " +
			"held those values. Say so rather than reporting a change.")
		return Result{Outcome: conversation.OutcomeOK, Content: b.String()}
	}

	b.WriteString(", and read it back to be sure. ")
	for i, c := range moved {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s was %s, is now %s.", upperFirst(c.What), c.From, c.To)
	}
	b.WriteString(" Tell them what moved and what it moved from, in those terms: they are " +
		"listening and have nothing to check it against.")

	// The "from" is what gets dropped, every time. It is also the half
	// they cannot reconstruct, so it is the half worth enforcing.
	owed := make([]string, 0, len(moved))
	var say strings.Builder
	for i, c := range moved {
		owed = append(owed, c.From)
		if i > 0 {
			say.WriteString(" ")
		}
		fmt.Fprintf(&say, "%s was %s before.", upperFirst(c.What), c.From)
	}
	return Result{
		Outcome: conversation.OutcomeOK, Content: b.String(),
		MustSay: owed, Else: say.String(),
	}
}

// Removed : A deletion that was read back and found to have taken.
//
// The count before and after is the point. "Deleted" is unverifiable by
// ear; "you had four, you now have three" is the person's own check
// that the right number of things went, and that it was one and not
// all of them.
func Removed(did string, before, after int, noun string) Result {
	var b strings.Builder
	b.WriteString(did)
	b.WriteString(", and counted afterwards to be sure. ")
	fmt.Fprintf(&b, "There %s %d %s before, and %s now.",
		were(before), before, plural(noun, before), remaining(after, noun))

	if gone := before - after; gone != 1 {
		fmt.Fprintf(&b, " That is %d fewer, not one: say so plainly, because it is not what "+
			"they will have expected.", gone)
		return Result{Outcome: conversation.OutcomePartial, Content: b.String()}
	}
	b.WriteString(" Tell them both numbers: they are listening and cannot see the list.")
	return Result{
		Outcome: conversation.OutcomeOK, Content: b.String(),
		MustSay: []string{fmt.Sprint(before), fmt.Sprint(after)},
		Else: fmt.Sprintf("There %s %d %s before, and %s now.",
			were(before), before, plural(noun, before), remaining(after, noun)),
	}
}

// Unverified : A write that reported success and did not survive being
// read back.
//
// The worst case there is, and the reason any of this exists. Reported
// as a failure, because from the person's side nothing happened and
// being told otherwise is how a thing gets lost.
func Unverified(did, found string) Result {
	return Result{
		Outcome: conversation.OutcomeFailed,
		Content: did + " appeared to work, but reading it back afterwards shows " + found +
			". Do not say it is done. Tell them it did not take and that they should check.",
	}
}

// remaining : How many are left, said the way it would be spoken.
func remaining(n int, noun string) string {
	if n == 0 {
		return "there are none"
	}
	if n == 1 {
		return "there is 1 " + noun
	}
	return fmt.Sprintf("there are %d %s", n, plural(noun, n))
}

// were : "was" or "were", for a count.
func were(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// plural : A noun for a count, by the ordinary rule.
func plural(noun string, n int) string {
	if n == 1 {
		return noun
	}
	if strings.HasSuffix(noun, "y") {
		return strings.TrimSuffix(noun, "y") + "ies"
	}
	return noun + "s"
}

// upperFirst : The first letter in upper case.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Owed : What a turn still has to say, gathered from the tools it ran.
type Owed struct {
	Facts []string
	Else  string
}

// Owing : The obligations among a turn's results.
func Owing(results []Result) []Owed {
	var out []Owed
	for _, r := range results {
		if len(r.MustSay) > 0 && strings.TrimSpace(r.Else) != "" {
			out = append(out, Owed{Facts: r.MustSay, Else: r.Else})
		}
	}
	return out
}

// Ensure : The answer, with anything it failed to mention added.
//
// Appended rather than rewritten. What the model said is its own and
// usually reads better; this only adds back the facts it dropped, and
// only the ones actually missing.
//
// Matching is on the fact appearing anywhere in the answer, which is
// crude and deliberately so: a false negative costs one redundant
// clause, and a false positive costs the person the thing they asked
// to be told.
func Ensure(answer string, owed []Owed) string {
	lower := strings.ToLower(answer)

	for _, o := range owed {
		missing := false
		for _, fact := range o.Facts {
			if f := strings.TrimSpace(strings.ToLower(fact)); f != "" && !strings.Contains(lower, f) {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		if strings.TrimSpace(answer) != "" {
			answer = strings.TrimRight(answer, " ") + " "
		}
		answer += o.Else
		lower = strings.ToLower(answer)
	}
	return answer
}
