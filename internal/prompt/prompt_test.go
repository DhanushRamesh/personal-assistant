package prompt_test

import (
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
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
