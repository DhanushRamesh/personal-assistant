package prompt_test

import (
	"testing"

	"personal-assistant/internal/base/thinking/prompt"
)

// Sentences are joined by exactly one space, however they were written.
//
// The point of the package: spacing cannot be got wrong by forgetting
// a trailing space, or doubled by remembering it twice.
func TestSentencesGetOneSpace(t *testing.T) {
	got := prompt.Text("One. ", "  Two.", "Three.")
	if want := "One. Two. Three."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Paragraphs are separated by a blank line.
func TestParagraphsGetABlankLine(t *testing.T) {
	got := prompt.Block("First.", "Second.")
	if want := "First.\n\nSecond."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// An empty piece disappears rather than leaving a gap, so a caller may
// pass something conditional without guarding it.
func TestEmptyPiecesVanish(t *testing.T) {
	if got := prompt.Block("First.", "", "   ", "Second."); got != "First.\n\nSecond." {
		t.Errorf("got %q", got)
	}
	if got := prompt.Text(""); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

// TestWhatCountsAsSayingNothing : This decides whether the assistant
// speaks at all.
//
// A rule that has nothing to report answers with the word, and the
// greeting does the same. Read too strictly, "NOTHING." is spoken
// aloud as an answer. Read too loosely, a real sentence that happens
// to begin with the word is swallowed and the person is never told.
func TestWhatCountsAsSayingNothing(t *testing.T) {
	for _, silent := range []string{
		"NOTHING",
		"nothing",
		"Nothing",
		"NOTHING.",
		"NOTHING!",
		`"NOTHING"`,
		"'nothing'",
		"  NOTHING  ",
		"NOTHING\n",
		// A model that says the word and then explains itself is
		// still declining, and the explanation must not be spoken.
		"NOTHING\nThere is nothing in the diary that needs the phone.",
		"",
		"   ",
		"\n\n",
	} {
		if !prompt.Nothing(silent) {
			t.Errorf("%q was taken as something to say", silent)
		}
	}

	for _, said := range []string{
		"Nothing is in your diary today, sir.",
		"There is nothing to report.",
		"NOTHING much is happening, sir.",
		"The battery is nearly gone, sir.",
		"no",
		"nothings",
	} {
		if prompt.Nothing(said) {
			t.Errorf("%q was swallowed, and the person is never told", said)
		}
	}
}

// TestSilenceIsTheWordThatIsLookedFor : The constant and the check
// have to agree, or every rule quietly starts speaking.
func TestSilenceIsTheWordThatIsLookedFor(t *testing.T) {
	if !prompt.Nothing(prompt.Silence) {
		t.Errorf("Nothing(%q) is false, so the word the model is told to use is not the word that is read",
			prompt.Silence)
	}
}

// TestPiecesOnTheirOwnLines : Lines is what a list of conditions or
// rules is built with, and a dropped separator runs two of them
// together into one instruction.
func TestPiecesOnTheirOwnLines(t *testing.T) {
	got := prompt.Lines("first", "", "  second  ", "   ", "third")
	want := "first\nsecond\nthird"
	if got != want {
		t.Errorf("Lines = %q, want %q", got, want)
	}
	if prompt.Lines() != "" || prompt.Lines("", "  ") != "" {
		t.Error("nothing at all produced something")
	}
}
