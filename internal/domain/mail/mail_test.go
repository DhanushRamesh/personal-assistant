package mail

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestALongMessageIsCutOnACharacter : Longest is a byte count
// because a prompt is budgeted in bytes, and slicing there lands
// inside a character in any script that is not Latin.
//
// Found by trying it: four thousand bytes of Tamil cut at the
// four-thousandth byte is not text, and what is not text goes into a
// prompt and from there to whatever is listening.
func TestALongMessageIsCutOnACharacter(t *testing.T) {
	for _, c := range []struct{ what, letter string }{
		{"Tamil", "அ"},     // three bytes
		{"an emoji", "😀"},  // four bytes
		{"an accent", "é"}, // two bytes
		{"plain Latin", "a"},
	} {
		t.Run(c.what, func(t *testing.T) {
			// Long enough that the cut must land inside a character
			// for every width above.
			body := strings.Repeat(c.letter, Longest+1)
			got := shorten(body)

			if !utf8.ValidString(got) {
				t.Errorf("what came out is not text")
			}
			if !strings.HasSuffix(got, "[the rest of this message was not read]") {
				t.Error("it was cut without saying so")
			}
			if len(got) > Longest+len("\n\n[the rest of this message was not read]") {
				t.Errorf("it came to %d bytes, which is more than was asked for", len(got))
			}
		})
	}
}

// TestAShortMessageIsLeftAlone : And is not told it was cut.
func TestAShortMessageIsLeftAlone(t *testing.T) {
	for _, body := range []string{"", "  ", "  a short note  ", strings.Repeat("a", Longest)} {
		got := shorten(body)
		if strings.Contains(got, "not read") {
			t.Errorf("a message of %d bytes was said to be cut", len(body))
		}
		if got != strings.TrimSpace(body) {
			t.Errorf("shorten(%q) = %q, want it only trimmed", body, got)
		}
	}
}

// TestHTMLBecomesSomethingSpeakable : A mail read aloud must not have
// its markup in it, and the tags have to leave a gap behind them --
// without one, "<p>one</p><p>two</p>" is read as "onetwo".
func TestHTMLBecomesSomethingSpeakable(t *testing.T) {
	for _, c := range []struct{ html, want string }{
		{"<p>one</p><p>two</p>", "one two"},
		{"<b>bold</b> and plain", "bold and plain"},
		{"<a href=\"http://example.com\">a link</a>", "a link"},
		{"no markup at all", "no markup at all"},
		{"  spaced   out  ", "spaced out"},
		{"", ""},
		{"<p>", ""},
		// An unclosed tag swallows the rest rather than printing it,
		// which is the right way round: half a tag is not words.
		{"before <broken", "before"},
	} {
		if got := stripTags(c.html); got != c.want {
			t.Errorf("stripTags(%q) = %q, want %q", c.html, got, c.want)
		}
	}
}
