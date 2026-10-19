// Package naming finds the names in a sentence.
//
// A name is a span cut out of the text, never a word written fresh, so
// what comes back was certainly said. That is the whole reason this is
// not a model that writes: asked for the names in a sentence, a writing
// model can answer with one that was never there.
package naming

import (
	"context"
	"errors"
	"strings"
)

// Span : One name, as it was said.
type Span struct {
	// Text : The words themselves, cut from the sentence.
	Text string
	// Label : Which kind of name it was looked for as.
	Label string
	// Score : How sure the finder is, from 0 to 1.
	Score float64
}

// Labels : The kinds of name looked for when a caller names none.
//
// Given at call time rather than trained in, so a new kind costs a line
// here and nothing else.
var Labels = []string{"person", "place", "app", "song"}

// Threshold : How sure the finder must be before a span is reported.
//
// Low on purpose. A span that is wrong is looked up and found to be
// nothing, which costs one query; a span that is never reported is a
// lookup that never happens at all.
const Threshold = 0.4

// Finder : Something that can point at the names in a sentence.
type Finder interface {
	// Names : The spans in text that look like one of labels. An empty
	// labels selects [Labels].
	Names(ctx context.Context, text string, labels []string) ([]Span, error)

	// Available : Whether names can actually be found.
	Available() bool
}

// ErrUnavailable : Returned when there is nothing to find names with.
var ErrUnavailable = errors.New("naming: nothing configured to find names with")

// Fake : A Finder for tests, which reports the capitalised words it is
// given as people. Enough to exercise a caller without a service.
type Fake struct {
	// Off : Makes Available report false, for the path where nothing is
	// configured.
	Off bool
}

// Names : Every capitalised word in text, as a person.
func (f Fake) Names(_ context.Context, text string, _ []string) ([]Span, error) {
	if f.Off {
		return nil, ErrUnavailable
	}
	var out []Span
	for _, word := range strings.Fields(text) {
		trimmed := strings.Trim(word, ".,?!;:'\"")
		if trimmed == "" || !isUpper(rune(trimmed[0])) {
			continue
		}
		out = append(out, Span{Text: trimmed, Label: "person", Score: 0.9})
	}
	return out, nil
}

// Available : True unless Off is set.
func (f Fake) Available() bool { return !f.Off }

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
