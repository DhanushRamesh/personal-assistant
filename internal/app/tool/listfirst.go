package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/prompt"
)

// listFirst : Whether a call that names something by identifier may go
// ahead, given what has run this turn, and the listing it needs if it
// may not. Nil when it may proceed.
//
// An identifier comes from a listing in the same turn, every time. The
// reason is not tidiness: without one the model works from what it
// remembers or from an identifier it made up, and both look exactly
// like working. Four events were nearly deleted by calls carrying
// "<id_for_first_single_day_event>", and when they were refused the
// answer was that the events did not exist.
//
// Refused rather than warned, because a warning is a thing that can be
// read past.
//
// The listing is then done here rather than asked for. Asking cost two
// rounds of a turn that has thirty seconds -- guess, be refused, read,
// write -- and it happened four times in one conversation about a cinema
// booking. The protection is unchanged either way: what it guards is that
// a real listing was in front of the model when it chose an identifier,
// not which call fetched it.
//
// # Reads need it as much as writes
//
// This began as a guard on writing, and for a month "writes" was the
// condition. On 19 October 2026 the owner asked what the new film was,
// having just been told a Netflix message had arrived about one. The
// model called mail_read with id "netflix_new_film" -- invented, in the
// shape of the subject line -- and when that failed it searched the web
// and answered about Netflix's October releases in general.
//
// mail_read was the one tool in the server that required an identifier
// and did not write, so it was the one the guard never looked at. Its
// own description forbids guessing an identifier twice over; the model
// guessed anyway, which is the ordinary fate of a rule written in prose.
//
// The harm is not smaller for a read. A failed write is refused and
// says so; a failed read leaves the model with a question it cannot
// answer from the domain it is in, and it goes somewhere else for an
// answer rather than looking again.
//
// So the condition is now what the mechanism was always about: a call
// that names something by identifier, whoever else it also changes.
func (r *Registry) listFirst(ctx context.Context, t Tool, in Invocation) *Result {
	if t.Domain == "" || !(t.Writes || NamesSomething(t)) {
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
			nothingHappened(t),
			fmt.Sprintf("The %s is listed first, so that an identifier is one that exists: call %s in "+
				"this same turn and work from what it returns.", t.Domain, orSomething(named(reads))),
			identifiersComeFrom,
			fmt.Sprintf("List first, then call %s again.", t.Name)), nil)
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
			fmt.Sprintf("%s The %s is listed before an identifier is used, and "+
				"this time it could not be listed.", nothingHappened(t), t.Domain),
			got.Content,
			"Tell them that, in your own words, and do not say anything was changed."), nil)
	}

	return refusal(prompt.Block(
		prompt.Text(
			fmt.Sprintf("%s An identifier comes from a listing, every time, so "+
				"the %s was listed for you:", nothingHappened(t), t.Domain)),
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

// nothingHappened : What to open a refusal with, which depends on what
// the refused call would have done.
//
// "Nothing has been changed" is a reassurance to pass on, and on a read
// it is noise about a danger that was never there. Worse, it invites the
// model to tell them nothing was changed when they asked to be told
// something.
func nothingHappened(t Tool) string {
	if t.Writes {
		return "Nothing has been changed."
	}
	return "Nothing has been read."
}

// NamesSomething : Whether this tool requires the caller to name a thing
// by identifier.
//
// Derived from the schema rather than declared on the tool. A flag is a
// thing somebody forgets to set on the one tool that needed it, and the
// guard then sits dead for exactly that tool -- which is how mail_read
// went a month unguarded while every write beside it was covered.
//
// Required is what matters. An optional identifier narrows a listing
// that works without it, and that call is how a model finds identifiers
// rather than a place it can invent one.
func NamesSomething(t Tool) bool {
	for _, name := range t.Params.Required {
		if anIdentifier(name) {
			return true
		}
		if p, ok := t.Params.Properties[name]; ok && namesWithin(p) {
			return true
		}
	}
	return false
}

// namesWithin : Whether the parts of a property are identifiers --
// an array of them, or objects carrying one.
//
// memory_forget takes an identifier paired with the subject it is
// expected to have, several at a time. The identifiers are a level down
// and are no less invented for it.
func namesWithin(p Property) bool {
	if p.Items != nil {
		if namesWithin(*p.Items) {
			return true
		}
	}
	for _, name := range p.Required {
		if anIdentifier(name) {
			return true
		}
	}
	return false
}

// anIdentifier : Whether an argument of this name is one.
//
// By name, because that is what the schema says out loud and what the
// model fills in. The set is closed deliberately: a tool that calls its
// identifier something else is a tool this will not guard, and the test
// that walks the registry is what catches it.
func anIdentifier(name string) bool {
	switch name {
	case "id", "ids":
		return true
	}
	return false
}

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
