// Package prompt puts the pieces of a prompt together.
//
// Everything the model is told was written as one long expression of
// string literals joined by +, with the spacing carried inside the
// literals: "...given. " + "Nothing else...". That reads badly and it
// is fragile in a way the compiler cannot see. A missing trailing
// space joins two words silently, a doubled one is invisible in the
// source, and where a paragraph begins is a matter of remembering to
// end a literal with \n\n.
//
// Here the separator is the function and the pieces are a list, so the
// spacing cannot be got wrong by forgetting something.
package prompt

import "strings"

// Text : Sentences of one paragraph, joined by a single space.
//
// For prose that flows. Each piece is one thought, written without
// worrying where the line ends.
func Text(parts ...string) string { return join(" ", parts) }

// Block : Paragraphs, separated by a blank line.
//
// For pieces the model should read as separate things rather than one
// run-on instruction.
func Block(parts ...string) string { return join("\n\n", parts) }

// Lines : Pieces on consecutive lines, for a list.
func Lines(parts ...string) string { return join("\n", parts) }

// join : The non-empty pieces, trimmed, with the given separator.
//
// Empties are dropped so a caller can pass something conditional
// without guarding it, which is what the hand-written version of this
// spent several lines doing at each use.
func join(sep string, parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
