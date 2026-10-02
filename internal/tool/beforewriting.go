package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// readFirst : Whether a write may go ahead, given what has run this turn,
// and the reading it needs if it may not. Nil when it may proceed.
//
// A write is preceded by a read of its own domain, every time. The reason
// is not tidiness: without one the model writes from what it remembers or
// from an identifier it made up, and both look exactly like working. Four
// events were nearly deleted by calls carrying
// "<id_for_first_single_day_event>", and when they were refused the
// answer was that the events did not exist.
//
// Refused rather than warned, because a warning is a thing that can be
// read past.
//
// The reading is then done here rather than asked for. Asking cost two
// rounds of a turn that has thirty seconds -- guess, be refused, read,
// write -- and it happened four times in one conversation about a cinema
// booking. The protection is unchanged either way: what it guards is that
// a real listing was in front of the model when it chose an identifier,
// not which call fetched it.
func (r *Registry) readFirst(ctx context.Context, t Tool, in Invocation) *Result {
	if !t.Writes || t.Domain == "" {
		return nil
	}

	var reads []Tool
	for _, name := range r.order {
		x := r.tools[name]
		if x.Domain != t.Domain || !x.Lists {
			continue
		}
		if slices.Contains(in.Ran, x.Name) {
			return nil
		}
		reads = append(reads, x)
	}
	// A domain with nothing to read cannot insist on a read.
	if len(reads) == 0 {
		return nil
	}

	listing, ok := unprompted(reads, in.Caller.Channel)
	if !ok {
		// Every way of reading this domain needs arguments only the
		// model can supply, so it is asked for after all.
		return refusal(prompt.Text(
			"Nothing has been changed.",
			fmt.Sprintf("Before writing to the %s, read what is actually there: call %s in "+
				"this same turn and work from what it returns.", t.Domain, orSomething(named(reads))),
			identifiersComeFrom,
			fmt.Sprintf("Read first, then call %s again.", t.Name)), nil)
	}

	// Through Call rather than Run, so the channel gate and the
	// validator apply to a call the server made exactly as they would
	// to one the model made.
	got := r.Call(ctx, listing.Name, Invocation{
		Caller: in.Caller, Args: json.RawMessage("{}"), Ran: in.Ran,
	})
	if got.Outcome == conversation.OutcomeFailed {
		// Not marked as read, because it was not read. Passed on all
		// the same: the model would have called it next and met the
		// same failure a round later, and this way it can say so now.
		return refusal(prompt.Text(
			fmt.Sprintf("Nothing has been changed. The %s is read before every write, and "+
				"this time it could not be read.", t.Domain),
			got.Content,
			"Tell them that, in your own words, and do not say anything was changed."), nil)
	}

	return refusal(prompt.Block(
		prompt.Text(
			fmt.Sprintf("Nothing has been changed yet. A write is read first, every time, so "+
				"the %s was read for you:", t.Domain)),
		got.Content,
		prompt.Text(
			fmt.Sprintf("Now call %s again, with an identifier from that listing.", t.Name),
			identifiersComeFrom,
			fmt.Sprintf("If what you want is not above, call %s yourself for the right ones "+
				"before writing.", listing.Name)),
	), []string{listing.Name})
}

// identifiersComeFrom : The rule the whole mechanism exists for, said
// the same way wherever a write is sent back.
const identifiersComeFrom = "An identifier has to come from that listing, never from memory, " +
	"never from earlier in the conversation, and never made up to fit."

// refusal : A write that did nothing, and what the server read on its
// behalf while refusing it.
func refusal(why string, read []string) *Result {
	return &Result{Outcome: conversation.OutcomeFailed, Content: why, Read: read}
}

// unprompted : A way of reading this domain that the server can call
// itself, which means one with no required arguments.
//
// Hot ones first. A domain often has more than one listing -- the
// diary has its events and its calendars -- and the hot list is the
// measured record of which one people actually call, which is as good
// an answer as exists to which is the domain's main listing. Falling
// back to registration order, since a domain whose listings are all
// deferred still has a first one.
func unprompted(reads []Tool, c chat.Channel) (Tool, bool) {
	var fallback Tool
	var found bool
	for _, x := range reads {
		if !x.Reaches(c) || len(x.Params.Required) > 0 {
			continue
		}
		if Hot(x.Name) {
			return x, true
		}
		if !found {
			fallback, found = x, true
		}
	}
	return fallback, found
}

// named : The tools' names.
func named(reads []Tool) []string {
	out := make([]string, 0, len(reads))
	for _, x := range reads {
		out = append(out, x.Name)
	}
	return out
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
