package greet

import (
	"strings"
	"time"
)

// Salutations : What is said first, by part of the day.
//
// Several of each, because one fixed line per part of the day is the
// same sentence every morning for ever, and a greeting somebody can
// recite along with is not a greeting.
//
// Here rather than with the presence handler that picks one, so that
// the half of this package which strips a salutation the model wrote
// for itself is reading the same list the server writes from. Two
// lists would agree until one of them was edited.
var Salutations = map[string][]string{
	"night":     {"Hello, sir.", "Still up, sir."},
	"morning":   {"Good morning, sir.", "Morning, sir."},
	"afternoon": {"Good afternoon, sir.", "Afternoon, sir."},
	"evening":   {"Good evening, sir.", "Evening, sir."},
}

// Back : The welcome, said after the salutation.
//
// Always, and not as one of the alternatives above. Owner, 5 October
// 2026: *"it should say the good morning / afternoon / evening and
// welcome backs at the beginning."* It used to be a third choice in
// each band, which meant a greeting had one or the other and most
// often neither, because the model wrote the opening itself and
// frequently did not write one at all.
//
// Nothing is said at this door that is not an arrival -- presence
// sleeps when the laptop does, so an absence it never saw produces
// no greeting -- so somebody hearing this has in fact come back.
const Back = "Welcome back."

// Band : Which salutations suit an hour, in the person's own zone.
func Band(at time.Time) string {
	switch {
	case at.Hour() < 5:
		return "night"
	case at.Hour() < 12:
		return "morning"
	case at.Hour() < 17:
		return "afternoon"
	case at.Hour() < 22:
		return "evening"
	default:
		return "night"
	}
}

// Opening : A salutation with the welcome behind it.
func Opening(salutation string) string {
	salutation = strings.TrimSpace(salutation)
	if salutation == "" {
		return Back
	}
	return salutation + " " + Back
}

// Without : What the model wrote, with any opening it wrote for
// itself taken off the front.
//
// The prompt tells it the opening has been said already and asks for
// what comes after. It mostly obeys; this is here for when it does
// not, because the cost of being wrong is "Good afternoon, sir.
// Welcome back. Good afternoon, sir." out loud.
//
// Only the phrases this server itself says are recognised. A list of
// English pleasantries invented here would be a keyword match that
// fails silently -- and worse, would eat a real first sentence that
// happened to begin with one of them.
func Without(answer string) string {
	answer = strings.TrimSpace(answer)
	for cut := true; cut; {
		cut = false
		for _, phrase := range openings() {
			rest, took := after(answer, phrase)
			if !took {
				continue
			}
			answer, cut = rest, true
			break
		}
	}
	return answer
}

// openings : Every fixed phrase an opening can be made of.
func openings() []string {
	out := make([]string, 0, 9)
	for _, band := range Salutations {
		out = append(out, band...)
	}
	return append(out, Back)
}

// after : What follows a phrase at the start of a sentence, and
// whether it was there at all.
//
// The trailing full stop is not required and what follows it may be
// a comma or a dash, because "Good afternoon, sir -- Kliffy rang" is
// the same mistake as "Good afternoon, sir. Kliffy rang".
func after(answer, phrase string) (string, bool) {
	phrase = strings.TrimRight(phrase, ".")
	if len(answer) < len(phrase) || !strings.EqualFold(answer[:len(phrase)], phrase) {
		return answer, false
	}
	rest := strings.TrimLeft(answer[len(phrase):], ".,;:!?-— \t")
	if rest == "" {
		return "", true
	}
	return rest, true
}
