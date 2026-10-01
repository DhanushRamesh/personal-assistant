package tool

import (
	"sort"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// hot : The tools described in full on every request.
//
// Everything else is named in the prompt and described only when asked
// for, because describing all of them costs more than the rest of the
// request put together. Measured on 30 September 2026, one turn:
//
//	tools (34)    72,343 bytes   72% of the request
//	context       20,819 bytes   21%
//	messages       7,846 bytes    8%
//	                             26,608 input tokens
//
// About 2,128 bytes a tool, sent again on every call -- the endpoint
// offers no prompt caching, so nothing about this is paid once.
//
// Most are chosen by use, not by judgement: from 570 recorded calls
// the twelve below them are 77% of every tool call ever made here. The
// long tail is real and none of it is dead -- 34 of 36 tools have been
// called at least once -- which is why the answer is to describe them
// on demand rather than to remove any.
//
// The two mail tools are the exception, and it is worth being honest
// about that. They have no history at all, having existed for an
// afternoon, so no measurement put them here. They are here because
// of shape: they are the two ways into the domain, mail_read cannot
// be called without an identifier one of them returned, and
// mail_unread is a question people ask rarely. Deferred, the first
// mail question of every chat spent a round learning the arguments --
// measured at 17.2s against 11.4s for one that needed no round.
//
// mail_thread joined them for a reason that was measured rather than
// reasoned. Deferred, asked "is there any email I have not replied
// to", the model used mail_search instead and then answered that it
// had "no tool to compare incoming mail against your replies" -- with
// mail_thread named in its catalogue one line above. A name and a
// sentence were not enough to reach for; the arguments were what it
// needed to see.
//
// If the numbers later say otherwise, the numbers win.
//
// Worth revisiting when the numbers move. The list is here, in one
// place, rather than a flag on each tool, so that revisiting it means
// reading one measurement and editing one list.
var hot = []string{
	"reminder_set",
	"calendar_events",
	"reminder_list",
	"calendar_add",
	"calendar_cancel",
	"task_list",
	"conversation_list",
	"task_lists",
	"conversation_archive",
	"task_add",
	"conversation_new",
	"memory_remember",

	// By shape rather than by measurement. See above.
	"mail_inbox",
	"mail_search",
	"mail_thread",

	// By evidence, 1 October 2026. Named in the catalogue with its own
	// one-line purpose, and asked "what have I been up to today" the
	// model twice answered that it had no tool for it -- once after
	// being sent back to look, with the catalogue in front of it both
	// times.
	//
	// A tool named but not described is evidently a tool the model does
	// not believe it has, at least for a question it has never been
	// able to answer before. The others in the catalogue are variations
	// on things it can already do; this one is a kind of knowing it has
	// never had, and it reasons from what it has always been rather
	// than from the list.
	//
	// Costing every turn its description is worth not denying, twice,
	// something the person can see is there.
	"event_recent",
}

// hotSet : hot, for looking up.
var hotSet = func() map[string]bool {
	out := make(map[string]bool, len(hot))
	for _, name := range hot {
		out[name] = true
	}
	return out
}()

// Hot : Whether a tool is described in full on every request.
func Hot(name string) bool { return hotSet[name] }

// Offered : The tools to describe in full: the hot ones, plus any that
// have been asked about during this chat.
//
// Channel-filtered like For, and for the same reason. Revealing a tool
// does not widen what a channel may reach: a name that was never
// reachable is not describable either.
func (r *Registry) Offered(c chat.Channel, revealed map[string]bool) []Tool {
	if !r.defers(c) {
		// Everything, minus the tool that describes the others: with
		// nothing held back it can only be called and fail, and a tool
		// in the list that cannot succeed is worse than one absent.
		out := make([]Tool, 0, len(r.order))
		for _, t := range r.For(c) {
			if t.Name != DescribeName {
				out = append(out, t)
			}
		}
		return out
	}

	out := make([]Tool, 0, len(hot)+len(revealed)+1)
	for _, name := range r.order {
		t := r.tools[name]
		if !t.Reaches(c) {
			continue
		}
		// tool_describe is always offered, and this is the whole
		// mechanism rather than a detail. It is not in the hot list --
		// it is not one of the twelve anybody uses -- so on the first
		// pass it was deferred along with everything else, which left
		// the deferred tools named in the prompt and reachable by
		// nothing. The tests caught it; the mechanism would have been
		// dead on arrival.
		if Hot(name) || revealed[name] || name == DescribeName {
			out = append(out, t)
		}
	}
	return out
}

// defers : Whether this registry holds anything back for this channel.
//
// Two ways it does not. A registry built without tool_describe cannot
// defer anything, because a tool named and not described would be
// reachable by nothing at all -- so it offers everything, and a
// registry assembled without the mechanism behaves as it did before
// the mechanism existed. And when everything reachable is hot there is
// nothing to hold back, so tool_describe is left out rather than
// offered with nothing to describe.
func (r *Registry) defers(c chat.Channel) bool {
	if _, ok := r.tools[DescribeName]; !ok {
		return false
	}
	for _, name := range r.order {
		if name == DescribeName {
			continue
		}
		if t := r.tools[name]; t.Reaches(c) && !Hot(name) {
			return true
		}
	}
	return false
}

// Deferred : The tools named but not described, so the model knows they
// exist and can ask.
func (r *Registry) Deferred(c chat.Channel, revealed map[string]bool) []Tool {
	if !r.defers(c) {
		return nil
	}
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		if !t.Reaches(c) || Hot(name) || revealed[name] || name == DescribeName {
			continue
		}
		out = append(out, t)
	}
	return out
}

// Catalogue : What the prompt says about the tools it is not describing.
//
// A name and one line each. The line is the tool's own Purpose rather
// than anything written twice: two descriptions of one tool drift, and
// the short one is the one nobody would notice had gone stale.
//
// Empty when nothing is deferred, so a small installation carries no
// paragraph about a mechanism it is not using.
func (r *Registry) Catalogue(c chat.Channel, revealed map[string]bool) string {
	deferred := r.Deferred(c, revealed)
	if len(deferred) == 0 {
		return ""
	}

	lines := make([]string, 0, len(deferred))
	for _, t := range deferred {
		lines = append(lines, "- "+t.Name+": "+oneLine(t.Purpose))
	}
	sort.Strings(lines)

	return prompt.Block(
		prompt.Text(
			"There are more tools than the ones described above.",
			"These exist and can be used, but you have not been given their arguments yet:",
		),
		prompt.Lines(lines...),
		prompt.Text(
			"To use one, call tool_describe with its name and you will be given how to call it, and may then call it.",
			"That costs a round, so ask for every one you expect to need at once rather than one at a time.",
			"Do not guess a tool's arguments, and do not call one of these before asking: a call made up from the name alone fails, and the failure is indistinguishable to the person from the thing being impossible.",
		),
	)
}

// oneLine : The first sentence, on one line, for the catalogue.
func oneLine(purpose string) string {
	purpose = strings.Join(strings.Fields(purpose), " ")
	if at := strings.Index(purpose, ". "); at > 0 {
		purpose = purpose[:at+1]
	}
	return purpose
}

// HotCount : How many tools are described on every request, for a log
// line that says how much of the list is being sent.
func HotCount() int { return len(hot) }

// Withheld : Whether a tool exists and is reachable but was not
// described to the model on this round.
//
// Needed because being left out of the request is not a gate. The
// endpoint forwards a call for a tool it was never given -- measured
// on 30 September 2026: thirteen tools were offered, the model read
// mail_search in the catalogue, called it anyway with the one argument
// it could guess, and the call arrived here. So the runner has to
// notice and hand over the arguments rather than run something built
// from a name.
//
// Not a security boundary either way. Call checks the channel, and
// that is the boundary; this is about answering correctly instead of
// failing on a guess.
func (r *Registry) Withheld(c chat.Channel, revealed map[string]bool, name string) bool {
	if !r.defers(c) {
		return false
	}
	t, ok := r.tools[name]
	if !ok || !t.Reaches(c) {
		return false
	}
	return !Hot(name) && !revealed[name] && name != DescribeName
}
