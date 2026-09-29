package tool

import (
	"fmt"
	"slices"
	"strings"
)

// readFirst : Whether a write may go ahead, given what has run this turn.
//
// A write is preceded by a read of its own domain, every time. The reason
// is not tidiness: without one the model writes from what it remembers or
// from an identifier it made up, and both look exactly like working. Four
// events were nearly deleted by calls carrying
// "<id_for_first_single_day_event>", and when they were refused the
// answer was that the events did not exist.
//
// Refused rather than warned, because a warning is a thing that can be
// read past. The model lists, then calls again, inside the same turn.
//
// Returns the empty string when the write may proceed.
func (r *Registry) readFirst(t Tool, ran []string) string {
	if !t.Writes || t.Domain == "" {
		return ""
	}

	var reads []string
	for _, name := range r.order {
		if x := r.tools[name]; x.Domain == t.Domain && x.Lists {
			reads = append(reads, x.Name)
			if slices.Contains(ran, x.Name) {
				return ""
			}
		}
	}
	// A domain with nothing to read cannot insist on a read.
	if len(reads) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"Nothing has been changed. Before writing to the %s, read what is actually there: "+
			"call %s in this same turn and work from what it returns. "+
			"An identifier has to come from that listing, never from memory, never from "+
			"earlier in the conversation, and never made up to fit. "+
			"Read first, then call %s again.",
		t.Domain, orSomething(reads), t.Name)
}

// orSomething : The reads a domain offers, as "a or b".
func orSomething(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
	}
}
