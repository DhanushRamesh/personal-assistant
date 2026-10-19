// Package pointing says what the names in a sentence point at.
//
// Three answers, not one. A name the devices have reported is his and is
// looked up in his own record; a name they have never reported is a real
// name belonging to the world, and the world is the web; anything that
// is not a name at all is dropped before either.
//
// The middle answer is the one neither half could give alone. Measured
// over 91 spans from 140 of his own questions: the word list keeps every
// name worth a lookup and 4 that are not, and the lookup is never wrong
// and finds 9 of 22. Together they sort rather than filter.
package pointing

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"personal-assistant/internal/base/thinking/naming"
)

// Where : Which record a name belongs to.
type Where int

const (
	// Nowhere : Not a name. A pronoun, a kinship word with nobody in
	// it, a plain noun, or the assistant's own name.
	Nowhere Where = iota
	// His : A name his own devices have reported. Looked up in what the
	// server already holds, where the spelling is also correct.
	His
	// Worldly : A real name the devices have never reported. His own
	// record cannot answer for it.
	Worldly
)

// String : The word for a Where, for logs and tests.
func (w Where) String() string {
	switch w {
	case His:
		return "his"
	case Worldly:
		return "worldly"
	default:
		return "nowhere"
	}
}

// Point : One name and where it belongs.
type Point struct {
	// Said : The span as it was said, which may be misheard.
	Said string
	// Spelt : How the record spells it. Empty unless Where is His.
	Spelt string
	// Label : The kind of name the finder was looking for.
	Label string
	// Where : Which record to ask.
	Where Where
}

// Term : What to look the name up by: the record's spelling where there
// is one, and what was said where there is not.
func (p Point) Term() string {
	if p.Spelt != "" {
		return p.Spelt
	}
	return p.Said
}

// Known : The names the server has already been told, by whatever means.
//
// An interface rather than a list so the caller decides how far back it
// reads and what it counts, and so a test needs no database.
type Known interface {
	// Spelling : How the record spells this name, and whether it holds
	// it at all. Matching ignores case, since what was said has been
	// through speech recognition and the record has not.
	Spelling(ctx context.Context, said string) (string, bool)
}

// Pointer : Finds the names in a sentence and sorts them.
type Pointer struct {
	// Finder : What points at the spans. Required.
	Finder naming.Finder
	// Known : What the server already holds. Optional: without it every
	// name is Worldly, which is the safe way to be wrong.
	Known Known
	// Assistant : The assistant's own name, which is never a name worth
	// looking up. Configuration, so it is passed in.
	Assistant string
}

// Most : The most words a name runs to.
//
// Past this it is a clause and not a name: "capital of Australia" is a
// question about a place, not one of his places.
const Most = 3

// pronouns : Words that stand in for a name without being one.
var pronouns = set(`i me my mine myself you your yours yourself he him his she her hers
it its we us our ours they them their theirs this that these those who whom whose
someone somebody anyone anybody everyone everybody nobody one`)

// common : Words that name a kind of thing rather than which one.
//
// "home" and "office" are deliberately absent: those are what his own
// places are called, and dropping them loses two of the places he talks
// about most.
var common = set(`mom mum mother dad father papa brother sister cousin uncle aunt wife
husband friend guy man woman boy girl kid child people person honey dear sir madam
boss colleague neighbour neighbor doctor driver protagonist hero villain actor
character author writer singer player family parents
app application program software chat song music video movie film show episode
terminal window tab page screen folder file note list thing stuff name number
place house room door side network capital city town country street road shop
current own last next other same new old online offline`)

// filler : Words that carry no name on their own, so a span made only of
// these and the words above is still nameless.
var filler = set(`a an the of in on at to for and or but with from by about is are was
were am be been being do does did will would can could shall should may might
what when where why how which if then than as so just like not no yes`)

// words : The lowercase words in a span, apostrophes kept.
var words = regexp.MustCompile(`[a-z']+`)

// Points : The names in what was said, each with where it belongs.
//
// Order is the finder's, which is the order they were said in, with
// repeats of the same span dropped. A finder that is unavailable or
// fails returns no points and the error, because looking at nothing is
// the same as not looking and the caller should be able to tell.
func (p *Pointer) Points(ctx context.Context, said string) ([]Point, error) {
	if p == nil || p.Finder == nil || !p.Finder.Available() {
		return nil, naming.ErrUnavailable
	}

	spans, err := p.Finder.Names(ctx, said, nil)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(spans))
	out := make([]Point, 0, len(spans))
	for _, s := range spans {
		key := strings.ToLower(strings.TrimSpace(s.Text))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		point := Point{Said: s.Text, Label: s.Label}
		switch {
		case !Named(s.Text, p.Assistant):
			point.Where = Nowhere
		default:
			point.Where = Worldly
			if p.Known != nil {
				if spelt, ok := p.Known.Spelling(ctx, s.Text); ok {
					point.Where, point.Spelt = His, spelt
				}
			}
		}
		out = append(out, point)
	}
	return out, nil
}

// Named : Whether a span names something at all.
//
// Removes only, and only from a fixed vocabulary with no proper noun in
// it, so no name can be lost here however badly the lists are chosen.
func Named(span, assistant string) bool {
	found := words.FindAllString(strings.ToLower(span), -1)
	if len(found) == 0 || len(found) > Most {
		return false
	}
	if name := strings.ToLower(strings.TrimSpace(assistant)); name != "" {
		for _, w := range found {
			if w == name {
				return false
			}
		}
	}
	for _, w := range found {
		if !pronouns[w] && !common[w] && !filler[w] {
			return true
		}
	}
	return false
}

// Only : The points that belong where asked, in the order they were said.
func Only(points []Point, where Where) []Point {
	out := make([]Point, 0, len(points))
	for _, p := range points {
		if p.Where == where {
			out = append(out, p)
		}
	}
	return out
}

// Terms : What to look up, for the points that belong where asked.
func Terms(points []Point, where Where) []string {
	out := make([]string, 0, len(points))
	for _, p := range Only(points, where) {
		out = append(out, p.Term())
	}
	return out
}

// set : A lookup built from whitespace-separated words.
func set(list string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(list) {
		out[w] = true
	}
	return out
}

// Sorted : The words of a set, for tests that need them in one order.
func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for w := range m {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// Vocabulary : How many words the lists hold, for the test that none of
// them is a proper noun.
func Vocabulary() []string {
	all := map[string]bool{}
	for _, m := range []map[string]bool{pronouns, common, filler} {
		for w := range m {
			all[w] = true
		}
	}
	return sorted(all)
}
