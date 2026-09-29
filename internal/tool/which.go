package tool

import (
	"fmt"
	"strings"
)

// WhichOne : What a tool returns when what the person named did not
// settle on one thing.
//
// Never a bare refusal. The name reached the assistant through
// speech-to-text and a mangled one looks exactly like a wrong one, so
// "there is no such thing" is usually false and always unhelpful. The
// tool has the list, so it hands the list over and says what to do with
// it: name the likeliest and ask.
//
// A yes-or-no question is the right shape for it. Spoken, "yes" and "no"
// survive being transcribed where a name does not, and a question mark
// keeps the microphone open for the answer.
//
// [kind] is the singular of what was being looked for -- conversation,
// event, reminder -- and appears in the sentence the model reads.
func WhichOne(kind, said string, candidates []string) Result {
	if len(candidates) == 0 {
		return Failed(fmt.Sprintf("There are no %ss at all yet, so there is none called %q.",
			kind, said))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "No %s is called %q, though that was heard rather than read and may have "+
		"come out wrong. ", kind, said)
	fmt.Fprintf(&b, "These are the %d %ss there are: %s. ", len(candidates), kind,
		strings.Join(candidates, ", "))
	b.WriteString("Ask which was meant. Name the one likeliest to be it and put it as a question " +
		"they can answer yes or no, then act on what they say. Do not tell them there is no such " +
		kind + ", and do not pick one on their behalf.")
	return Failed(b.String())
}

// WhichOfThese : What a tool returns when several things are equally near
// what was said.
//
// Separate from WhichOne because the sentence has to be different: there
// is nothing to propose, so the model must offer the choice rather than a
// yes-or-no.
func WhichOfThese(kind, said string, candidates []string) Result {
	var b strings.Builder
	fmt.Fprintf(&b, "More than one %s could be what was meant by %q: %s. ",
		kind, said, strings.Join(candidates, ", "))
	b.WriteString("Ask which of them, reading the names out, and act on what they say. " +
		"Do not choose for them.")
	return Failed(b.String())
}

// BySound : The note a tool adds when what it found was not what was
// said, only near it.
//
// The match is an assumption, and an assumption the model has to be able
// to see in order to know whether to act on it or ask about it. Reading
// the wrong thing costs a sentence; changing or removing it cannot be
// taken back.
func BySound(kind, said, found string) string {
	return fmt.Sprintf("Matched by sound, not exactly: they said %q and this is %q. "+
		"Safe to read on that basis. Before creating, changing or removing anything on it, "+
		"put it to them as a yes-or-no question first.", said, found)
}

// Confirm : What a tool returns when it found the thing but only by
// likeness, and what happens next cannot be undone.
//
// A read of the wrong thing costs a sentence. A change or a deletion of
// the wrong thing cannot be taken back, so a name that was merely near is
// put to the person before anything happens to it.
func Confirm(kind, said, found, doing string) Result {
	return Failed(fmt.Sprintf(
		"They said %q and the nearest %s is %q, which is not the same words. "+
			"Before %s, ask whether that is the one they mean, as a question they can answer "+
			"yes or no. Nothing has been changed.", said, kind, found, doing))
}
