// Package heard matches names that arrived through speech against names
// that were written down.
//
// Everything the assistant is told by voice has been through
// speech-to-text, and names are where it fails worst: an address is read
// out as sounds and written down as letters. "rjdanesh22rjmail.com" was
// what came back for rjdhanush22@gmail.com, and "javas" for Jarvis.
// Matching exactly refuses both, and a refusal reads as the assistant
// being stupid rather than as the microphone being imperfect.
//
// So the question this package answers is not "is this the name" but "is
// this near enough to be the name, and is anything else as near".
package heard

import (
	"errors"
	"strings"
	"unicode"
)

// Alike : How near a spoken name must be to a written one before it is
// taken to mean it.
const Alike = 0.6

// Clearer : How much better the nearest name must be than the next before
// it is treated as the one meant rather than as a choice to put to the
// person.
const Clearer = 0.15

// Shortest : The fewest characters a name may have before one name being
// inside another counts for anything, so a syllable does not match a word.
const Shortest = 4

var (
	// ErrNone : Nothing was near enough to what was said.
	ErrNone = errors.New("heard: nothing by that name")
	// ErrSeveral : More than one was equally near, so choosing would be a
	// guess.
	ErrSeveral = errors.New("heard: more than one could be meant")
)

// Best : Which of the names is the one that was said.
//
// Exact first, then one name inside another, then likeness. Returns the
// index into names, or ErrNone when nothing is near enough and ErrSeveral
// when two are too close together to choose between.
func Best(names []string, said string) (int, error) {
	want := Normalise(said)
	if want == "" || len(names) == 0 {
		return -1, ErrNone
	}

	for i, name := range names {
		if Normalise(name) == want {
			return i, nil
		}
	}

	// "holidays" for "Holidays in India". Checked before likeness, which
	// scores a short name against a long one badly however right it is.
	for i, name := range names {
		n := Normalise(name)
		if len(want) >= Shortest && len(n) >= Shortest &&
			(strings.Contains(n, want) || strings.Contains(want, n)) {
			return i, nil
		}
	}

	best, top, next := -1, 0.0, 0.0
	for i, name := range names {
		switch score := Likeness(want, Normalise(name)); {
		case score > top:
			next, top, best = top, score, i
		case score > next:
			next = score
		}
	}

	if best < 0 || top < Alike {
		return -1, ErrNone
	}
	if top-next < Clearer {
		return -1, ErrSeveral
	}
	return best, nil
}

// Exactly : Whether what was said is the name itself rather than something
// near it.
//
// Worth knowing before anything is changed or removed. A read of the wrong
// thing is a wasted sentence; a deletion of the wrong thing cannot be
// taken back, so a write on a name that was merely near should be put to
// the person first.
func Exactly(name, said string) bool {
	return Normalise(name) == Normalise(said)
}

// Normalise : A name reduced to what survives being spoken: lower case,
// letters and digits only.
//
// An address loses its at sign and its dots, which is where speech-to-text
// does most of its damage and where none of the meaning lives.
func Normalise(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Likeness : How alike two normalised names are, from 0 to 1.
//
// Edit distance over the longer of the two, so a short name cannot score
// well against a long one merely by sharing its beginning.
func Likeness(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	longest := len(a)
	if len(b) > longest {
		longest = len(b)
	}
	return 1 - float64(distance(a, b))/float64(longest)
}

// distance : The number of single-character edits between two strings.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
