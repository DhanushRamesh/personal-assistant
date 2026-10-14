package speech

import (
	"strings"
	"testing"
	"time"
)

// The seven sentences the constants were fitted to, with the audio
// Home Assistant actually produced for each. Kept as the test because a
// constant fitted to measurements and then never checked against them
// is a number nobody can argue with later.
func TestSayingMatchesWhatPiperActuallyDid(t *testing.T) {
	for _, c := range []struct {
		said  string
		spoke time.Duration
	}{
		{"Yes, sir.", 1510 * time.Millisecond},
		{"Good evening, sir.", 1590 * time.Millisecond},
		{"Nothing on your calendar for tomorrow, sir.", 2980 * time.Millisecond},
		{"The time is ten past ten on Tuesday the thirteenth of October.", 4130 * time.Millisecond},
		{"You left home at eleven-thirty this morning and arrived at the office, " +
			"staying nine hours and nineteen minutes.", 6840 * time.Millisecond},
		{"I am afraid I cannot reach your calendar at the moment, sir. The battery " +
			"has charged fully tonight, and your first meeting is at half past nine.", 9090 * time.Millisecond},
		{"Alekhya called twice while you were out, sir, and your phone has been " +
			"charging since eight.", 6060 * time.Millisecond},
	} {
		got := Saying(c.said)
		off := float64(got-c.spoke) / float64(c.spoke) * 100
		if off < 0 {
			off = -off
		}
		// A quarter is the shortest utterance's error and nothing else
		// comes close. Anything past it means the speech rate moved.
		if off > 25 {
			t.Errorf("%.0fs of %q: estimated %s, spoke %s, %.1f%% out",
				c.spoke.Seconds(), c.said[:min(20, len(c.said))], got, c.spoke, off)
		}
		t.Logf("%3d chars  spoke %5.2fs  estimated %5.2fs  %+5.1f%%",
			len([]rune(c.said)), c.spoke.Seconds(), got.Seconds(), float64(got-c.spoke)/float64(c.spoke)*100)
	}
}

// Nothing to say takes no time, which is how a caller tells it apart
// from speech of no length.
func TestSayingNothingTakesNoTime(t *testing.T) {
	for _, said := range []string{"", "   ", "\n\t "} {
		if got := Saying(said); got != 0 {
			t.Errorf("Saying(%q) = %s, want 0", said, got)
		}
	}
}

// And an answer nobody could still be listening to is capped rather
// than believed.
func TestAnEndlessAnswerIsCapped(t *testing.T) {
	if got := Saying(strings.Repeat("a", 100_000)); got != Longest {
		t.Errorf("Saying(100k characters) = %s, want %s", got, Longest)
	}
}
