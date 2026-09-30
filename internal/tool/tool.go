// Package tool holds the things the assistant can do, as against say.
//
// A tool is described in a fixed shape rather than in a paragraph, because
// prose quality cannot be enforced and structure can. The descriptions that
// go wrong are the ones that say what a tool is without saying when to reach
// for it, and the repair is always the same: the description grows louder
// until it is shouting IMPORTANT at the model. A shape with a place for "use
// when" and a place for "do not" is what makes that unnecessary.
package tool

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Tool : One thing the assistant can do.
type Tool struct {
	// Name : What the model calls it. Lowercase with underscores, and named
	// for the action rather than the subject, so that a listing of names
	// reads as a list of verbs.
	Name string
	// Purpose : One line saying what it does.
	Purpose string
	// UseWhen : When to reach for it. The part a model gets wrong when it is
	// missing, since a description of what something is does not say when it
	// applies.
	UseWhen string
	// Avoid : When not to, and what to use instead. Optional, but the way to
	// separate two tools a model would otherwise confuse.
	Avoid string
	// Domain : What this tool is about -- calendar, conversation,
	// reminder, memory. Tools sharing a domain act on the same things, and
	// that is what lets a write insist on a read of its own domain first.
	Domain string
	// Lists : Whether this is the domain's way of seeing what is there.
	// Set on the fetch-all and on the search, since either puts real
	// identifiers in front of the model.
	Lists bool
	// Writes : Whether this creates, changes or removes something.
	//
	// A write is refused until its domain has been read in the same turn.
	// Not a hint: the model has been seen to delete from a list it never
	// fetched, with identifiers it invented, and then report the failures
	// as proof the things did not exist.
	Writes bool

	// Params : What it takes.
	Params Schema

	// Prefetch : Run before the model is asked anything, and given to
	// it as though it had called this itself.
	//
	// The generic form of a thing that was written by hand, per domain,
	// three times over: put the current state in front of the model
	// rather than hope it asks. Measured, a read tool the model must
	// decide to call is called once or twice in four; one the server
	// runs is called every turn.
	//
	// Only tools that need no arguments may do this, since there is
	// nobody to supply them. In practice that means the listings.
	Prefetch bool

	// WhenUnasked : What the model is told about this tool's answer
	// when it was fetched rather than asked for, written next to it.
	//
	// A listing nobody asked for reads as a list of things to raise.
	// Measured: with a bare listing and a general warning at the top
	// of the prompt, "I am tired" was answered with somebody's
	// tablets, two conversations out of three; with the warning
	// against the listing, none out of three.
	//
	// It belongs to the tool because only the tool knows what its
	// answer is for. That keeps the runner free of any domain: a
	// twentieth domain writes its own sentence here and nothing else
	// changes.
	WhenUnasked string

	// Fresh : How long a prefetched answer may be reused before it is
	// fetched again. Zero refetches every turn, which is the default
	// and the right one: accuracy comes before latency here.
	//
	// Present for a tool whose read is expensive enough that somebody
	// decides otherwise, deliberately and in one place.
	Fresh time.Duration
	// Channels : Which channels may reach it.
	//
	// The gate. A prompt that arrived as sound had no confirmation step: a
	// misheard sentence is the whole authorisation, so anything that cannot
	// be undone is left off the voice list.
	Channels []chat.Channel
	// Examples : What a call looks like. Written for the model, and checked
	// against the schema by a test, so an example that lies about its own
	// arguments is caught.
	Examples []Example
	// Run : What it does. Required.
	Run Run
}

// Example : A call worth showing the model.
type Example struct {
	// Ask : What the person said.
	Ask string
	// Args : What the tool should be called with, as JSON.
	Args string
}

// Run : What a tool does when called.
//
// It returns a Result rather than an error, because how a tool failed is
// something the model has to read: an error swallowed here becomes an
// invented explanation later.
type Run func(ctx context.Context, in Invocation) Result

// Invocation : One call to a tool.
type Invocation struct {
	// Caller : Who is asking, and from where.
	Caller Caller
	// Args : The arguments the model produced, as it wrote them.
	Args json.RawMessage
	// Ran : Which tools have already run in this turn, by name. What lets
	// a write know whether its domain was read first.
	Ran []string
}

// Caller : Who a tool is acting for.
//
// A tool never takes a user or a conversation as an argument. The model would
// have to supply them, which means it could supply the wrong one, and no
// description prevents that. They come from the request instead.
type Caller struct {
	// UserID : Whose assistant this is.
	UserID string
	// ClientID : Which client asked.
	ClientID string
	// ConversationID : The conversation the prompt landed in.
	ConversationID string
	// Channel : How the prompt arrived.
	Channel chat.Channel
}

// Result : What a tool gives back.
type Result struct {
	// Outcome : Whether it worked, did not, or did part of it.
	Outcome conversation.Outcome

	// MustSay : Facts the person has to be told, checked against the
	// answer once the turn is finished.
	//
	// A tool that verified a write knows something the person cannot
	// check by ear -- what a value was before, how many things are left
	// -- and telling them is the whole point of having looked. Asking
	// the model nicely does not work: measured, it was handed "the time
	// was 9:00 pm, is now 10:00 pm" and said "Moved to 10:00 pm, sir."
	//
	// So the obligation is carried out of the tool and enforced after.
	MustSay []string
	// Tally : How many there were and how many there are, when the thing
	// owed is a count. Lets several counts of the same thing in one turn
	// be collapsed into the net change rather than read out in a row.
	Tally *Tally

	// Else : The sentence appended when any of MustSay is missing from
	// the answer. Written to read as a continuation of it.
	Else string
	// Content : What it produced, or exactly what went wrong.
	//
	// A failure says why, in the words of whatever actually refused. A tool
	// reporting "could not do that" leaves the model to invent a reason, and
	// it will.
	Content string

	// Reveal : Tools to describe in full from the next round on.
	//
	// Only tool_describe sets this. The names it returns have to be
	// described on the next request or the model cannot call them: it
	// would be told the arguments in a tool result and then handed a
	// request that does not list the tool, and a call to something
	// absent from the list is refused before it reaches anything here.
	Reveal []string
}

// OK : A result that worked.
func OK(content string) Result {
	return Result{Outcome: conversation.OutcomeOK, Content: content}
}

// Failed : A result that did nothing, saying exactly why.
func Failed(why string) Result {
	return Result{Outcome: conversation.OutcomeFailed, Content: why}
}

// Partial : A result that did some of what was asked. The content says which
// parts, since "partly done" on its own tells a model nothing it can report.
func Partial(what string) Result {
	return Result{Outcome: conversation.OutcomePartial, Content: what}
}

// Reaches : Whether this tool may be called from the given channel.
func (t Tool) Reaches(c chat.Channel) bool {
	for _, allowed := range t.Channels {
		if allowed == c {
			return true
		}
	}
	return false
}

// Description : What the model is told about the tool.
//
// The parts are joined into the single string a service's schema allows, in a
// fixed order, so that every tool reads the same way and none of them has to
// shout to be noticed.
func (t Tool) Description() string {
	parts := []string{strings.TrimSpace(t.Purpose)}
	if use := strings.TrimSpace(t.UseWhen); use != "" {
		parts = append(parts, "Use when: "+use)
	}
	if avoid := strings.TrimSpace(t.Avoid); avoid != "" {
		parts = append(parts, "Do not use: "+avoid)
	}
	for _, e := range t.Examples {
		parts = append(parts, `Example — "`+e.Ask+`": `+e.Args)
	}
	return strings.Join(parts, " ")
}
