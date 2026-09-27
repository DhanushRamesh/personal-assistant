// Package vocabulary composes the word list speech-to-text is primed with.
//
// Whisper decodes with a language model of its own, and an initial prompt
// biases it towards words it would otherwise not reach for. Left to itself
// it produces a clean, well-formed, confident wrong word: "GitLab MRs"
// came back as "Kitla BMRs" and "Alekhya" as "Alekia", and both were
// written into reminders, because a mangled name and a name never heard
// before look exactly alike.
//
// The list was maintained by hand and went stale. What the owner actually
// talks about is already written down -- in their reminders, in what their
// conversations were named, in what has been remembered about them -- so
// that is where the words come from now, and correcting a wrongly heard
// reminder takes the wrong word back out.
package vocabulary

import (
	"strings"
	"unicode"
)

// Core : The words primed by hand, kept in the order they were written.
//
// These are not derivable from anything stored: they are what the assistant
// is asked to do rather than what it is asked about, and somebody who has
// never said "unarchive" still needs it heard correctly the first time.
//
// It goes in front of everything found, so the words that were chosen
// deliberately survive the budget and the ones merely observed are what get
// dropped.
const Core = "archive, unarchive, unarchived, archived, conversation, " +
	"conversations, chat, chats, switch, rename, delete, create, active, " +
	"untitled, list, grocery list, items, total, repeat, rupees, auto, " +
	"vegetables, flowers, dry fruits, cutting board, containers, soap, " +
	"lyrics, song, captain, visa, air conditioner, birthday, Alekhya, " +
	"Chintada, remind, reminder, reminders, timer, timers, minutes, " +
	"seconds, cancel, snooze, snoozed, time, what is the time, date, " +
	"what is the date, day, today, tomorrow, morning, evening"

// Budget : How long the whole prompt may be, in characters.
//
// Whisper allows an initial prompt of half its 448-token context, so 224
// tokens. Counted in characters rather than tokens because the tokeniser
// lives in Python and this does not, and kept well under four characters a
// token because the words most worth priming are names, which tokenise
// worse than ordinary English. Overshooting is silently truncated by
// Whisper at the wrong end -- it keeps the last tokens, so it is the
// deliberate words at the front that would be lost.
const Budget = 800

// Near : How far apart two words may be and still be treated as the same
// one misheard.
//
// Two edits catches "Alekia" for "Alekhya". Three would start merging
// words that differ honestly.
const Near = 2

// Prompt : The word list to prime speech-to-text with.
//
// found is in order of preference, most worth keeping first. Anything
// already in the core, or close enough to a core word to be that word
// misheard, is left out: the core is what somebody chose, and a word
// observed once is more likely to be the mistake than the correction.
func Prompt(core string, found []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(core))

	seen := map[string]bool{}
	for _, w := range words(core) {
		seen[strings.ToLower(w)] = true
	}
	known := keys(seen)

	for _, term := range found {
		term = strings.TrimSpace(term)
		lower := strings.ToLower(term)
		if term == "" || seen[lower] || nearAny(lower, known) {
			continue
		}
		if b.Len()+2+len(term) > Budget {
			// Full. Kept going rather than stopped, because a long term
			// near the front should not shut out the short ones behind
			// it.
			continue
		}
		b.WriteString(", ")
		b.WriteString(term)
		seen[lower] = true
	}
	return b.String()
}

// Found : The terms worth priming, drawn from what the person has written
// and had written about them, in the order given.
//
// A term is a word that looks like a name rather than ordinary English:
// one with a capital inside it, one in capitals throughout, or a
// capitalised word that is not the first of its phrase. The first word is
// excluded because every reminder title starts with one -- Call, Check,
// Watch -- and priming those buys nothing.
func Found(phrases []string) []string {
	var out []string
	seen := map[string]bool{}

	for _, phrase := range phrases {
		for i, w := range words(phrase) {
			w = strings.Trim(w, ".,;:!?'\"()")
			if len(w) < 3 || !name(w, i) {
				continue
			}
			lower := strings.ToLower(w)
			if seen[lower] {
				continue
			}
			seen[lower] = true
			out = append(out, w)
		}
	}
	return out
}

// name : Whether this word, at this position in its phrase, looks like a
// name rather than ordinary English.
func name(w string, position int) bool {
	r := []rune(w)
	if !unicode.IsUpper(r[0]) {
		return false
	}

	// A capital anywhere but the front, so GitLab and BMRs qualify
	// wherever they stand and an ordinary capitalised word does not.
	for _, c := range r[1:] {
		if unicode.IsUpper(c) || unicode.IsDigit(c) {
			return true
		}
	}
	return position > 0
}

// words : A phrase split on whitespace, empties dropped.
func words(phrase string) []string {
	return strings.FieldsFunc(phrase, func(r rune) bool {
		return unicode.IsSpace(r) || r == ','
	})
}

// keys : The words of a set, for comparing against.
func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// nearAny : Whether the word is close enough to any of them to be one of
// them misheard.
//
// Only for words of a length where the distance means something: at four
// characters, two edits is half the word.
func nearAny(w string, against []string) bool {
	if len([]rune(w)) < 5 {
		return false
	}
	for _, other := range against {
		if len([]rune(other)) < 5 {
			continue
		}
		if distance(w, other) <= Near {
			return true
		}
	}
	return false
}

// distance : How many single-character edits turn one word into the other.
func distance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// Words : The comma-separated entries of a list, trimmed.
//
// Entries rather than words: "grocery list" and "what is the time" are one
// each, and splitting them would show the core as something nobody wrote.
func Words(list string) []string {
	var out []string
	for _, part := range strings.Split(list, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Kept : The entries a prompt gained over its core, in order.
//
// Read back off the finished prompt rather than reported as it is built,
// so that what is shown is what is being primed and not what was offered.
func Kept(prompt, core string) []string {
	all := Words(prompt)
	n := len(Words(core))
	if n >= len(all) {
		return nil
	}
	return all[n:]
}
