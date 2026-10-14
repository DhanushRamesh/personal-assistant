//go:build evals

package evals

import "testing"

// TestAListTypedIntoAParagraphIsCaught : the answer that prompted the
// rule, and the same answer written properly.
//
// Owner, shown three paragraphs on screen: *"when typed it should give
// neat easily understandable formatted md content."* The rule already
// asked for markdown and for a list wherever more than two things are
// named; this is the shape it kept producing instead.
func TestAListTypedIntoAParagraphIsCaught(t *testing.T) {
	asProse := "Backups serve one purpose: to let you recover from loss. " +
		"You need three things. First, they must be automatic, so you cannot forget them. " +
		"Second, they must be kept somewhere else -- not on the same device. " +
		"Third, test them. A backup you have never restored is one you cannot trust."
	asList := "You need three things:\n\n" +
		"- **Automatic**, so you cannot forget them\n" +
		"- **Somewhere else**, never the same device\n" +
		"- **Tested**, because one you have never restored is one you cannot trust\n"

	if got := enumeratedIn(asProse); got == "" {
		t.Error("the answer that prompted the rule was not caught")
	} else {
		t.Logf("caught %q in the prose version", got)
	}
	if got := enumeratedIn(asList); got != "" {
		t.Errorf("the same thing written as a list was flagged: %q", got)
	}
	// A numbered markdown list begins a line and is not this.
	if got := enumeratedIn("1. First, do this\n2. Second, do that\n"); got != "" {
		t.Errorf("a markdown numbered list was flagged: %q", got)
	}
}
