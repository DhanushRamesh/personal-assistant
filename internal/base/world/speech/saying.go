package speech

import (
	"strings"
	"time"
)

// How long Piper takes to say a thing, measured rather than assumed.
//
// Seven of the assistant's own sentences were rendered through Home
// Assistant's tts.piper at the speech rate it is set to, and their audio
// measured:
//
//	chars   said    estimated
//	    9   1.51s   1.20s   -20.2%
//	   18   1.59s   1.72s    +8.2%
//	   43   2.98s   3.15s    +5.8%
//	   62   4.13s   4.24s    +2.7%
//	  111   6.84s   7.05s    +3.0%
//	  144   9.09s   8.94s    -1.7%
//	   91   6.06s   5.90s    -2.6%
//
// Characters and not words. Words looked the obvious unit and are not:
// seconds per word ranged from 0.337 to 0.757 across those same sentences,
// because a word is anything from "a" to "eleven-thirty", while seconds
// per character held between 0.0617 and 0.0693 for everything that was a
// real sentence. The short ones are high because of what Opening costs,
// which is why that is a term of its own rather than folded in.
//
// Worst case is the shortest utterance, "Yes, sir.", estimated 0.3s
// short of its 1.5s. For deciding how long a screen should show the
// assistant speaking, that is nothing.
const (
	// Opening : What an utterance costs before any of it is said -- the
	// lead-in and the tail Piper leaves around the words.
	Opening = 690 * time.Millisecond
	// PerRune : How long one character of it takes after that.
	PerRune = 57270 * time.Microsecond
)

// Longest : A cap on what Saying will return.
//
// Not a measurement, a refusal to be silly. An answer long enough to
// exceed this is one nobody is still listening to, and a screen left
// showing the assistant speaking for minutes because of a miscount is a
// worse fault than one that stops early.
const Longest = 90 * time.Second

// Saying : How long said will take to speak aloud.
//
// Zero for nothing to say, so a caller can tell "no speech" from "speech
// of no length" without a second field.
//
// This is an estimate and is named as one. It is wanted where the exact
// figure cannot be had: the server knows the words it is about to hand
// over long before anything has played them, and nothing ever tells it
// when the speaking finished. Where the audio itself can be measured,
// measure it.
func Saying(said string) time.Duration {
	runes := len([]rune(strings.TrimSpace(said)))
	if runes == 0 {
		return 0
	}
	d := Opening + time.Duration(runes)*PerRune
	if d > Longest {
		return Longest
	}
	return d
}
