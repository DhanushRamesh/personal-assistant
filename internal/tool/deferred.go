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
// These twelve are chosen by use, not by judgement: from 570 recorded
// calls they are 77% of every tool call ever made here. The long tail
// is real and none of it is dead -- 34 of 36 tools have been called at
// least once -- which is why the answer is to describe them on demand
// rather than to remove any.
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
