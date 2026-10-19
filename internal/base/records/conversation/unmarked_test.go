package conversation

import "testing"

// TestTheServersOwnNotesAreTakenOut : They are written for the model
// to read in the history and never to be said.
//
// It said two in a row on 19 October 2026, each beginning "[Said
// unasked at 8:09 pm, news from their devices -- a record of what you
// said then, never a way to begin an answer]" -- with the words "never
// a way to begin an answer" inside the thing it had begun its answer
// with. That wording was itself the fix, made earlier the same day,
// for the model copying the marker before it.
func TestTheServersOwnNotesAreTakenOut(t *testing.T) {
	said := "[Said unasked at 8:09 pm, news from their devices -- a record of what you said then, " +
		"never a way to begin an answer] Claude Code in Server has finished, sir."
	if got := Unmarked(said); got != "Claude Code in Server has finished, sir." {
		t.Errorf("the note was not taken out:\n%q", got)
	}

	// Every shape the server writes.
	for _, one := range []string{
		"[Announced aloud at 9 pm, as they came into the room.] Good evening, sir.",
		"[Reminder announced aloud at 7:30 pm.] Time to leave, sir.",
		"[You named this conversation at 2 pm, from what had been said in it so far.] Done, sir.",
		"[Said unasked at 8 am, ahead of something in their diary] The dentist at nine, sir.",
	} {
		if got := Unmarked(one); got == one {
			t.Errorf("a note was left in: %q", got)
		}
	}

	// Several in a row, which is how he saw it.
	two := "Done, sir.\n\n[Said unasked at 8:09 pm, news from their devices -- a record] One finished.\n\n" +
		"[Said unasked at 8:10 pm, news from their devices -- a record] The other finished."
	got := Unmarked(two)
	if got != "Done, sir.\n\nOne finished.\n\nThe other finished." {
		t.Errorf("two notes in a row were not both taken out:\n%q", got)
	}

	// A bracket of his own is left alone.
	for _, one := range []string{
		"The log says [INFO] on every line, sir.",
		"He wrote [sic] after it, sir.",
		"Nothing bracketed here at all, sir.",
	} {
		if got := Unmarked(one); got != one {
			t.Errorf("a bracket that was not ours was taken out: %q became %q", one, got)
		}
	}
}
