package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/domain/tasks"
)

// closeEnough : How much of a name has to match for it to be offered
// as the one they meant.
//
// Low, because the name arrived through speech. "Jarvis improvements"
// came back as "jars and groovements" in this very conversation, and
// a search that insisted on the whole string would find nothing and
// report that the task does not exist.
const closeEnough = 0.45

// findATask : Where a task is, across every list.
//
// Built because the assistant said it could not. Asked about a task
// on a list that did not exist, it answered "I have no tool to search
// for a task across all your lists, sir" and read out the lists
// instead -- which was true, and which made the recitation the only
// answer available to it. Naming the lists is not what the person
// asked; where the task is, is.
func findATask(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_find",
		Domain: "task",
		Lists:  true,
		Purpose: prompt.Text(
			"Find a task by name across every list at once, and say which list it is on.",
			"Answers whether a task exists anywhere, which reading one list cannot.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"They name a task and you do not already know which list it is on.",
				"Also when they name a task and a list, and the list is not one they have: the list was probably misheard and the task may still be there.",
			),
			prompt.Text(
				"This before saying a task does not exist, every time.",
				"Saying it is not there having read one list is a claim about every list, made from one.",
			),
		),
		Avoid: prompt.Text(
			"Do not use it to read a list: task_list reads one, and task_lists names them.",
			"Do not use it when they already said which list and that list exists -- task_list is one call instead of all of them.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"name": {
					Type: "string",
					Description: "What they called the task. Part of it is better than all of it: " +
						"the name reached you through speech and may be mangled.",
				},
			},
			Required: []string{"name"},
		},
		Examples: []tool.Example{
			{Ask: "I had a task called no tool info in Java support",
				Args: `{"name":"no tool info","saying":"looking for that task"}`},
			{Ask: "where is the task about the geyser",
				Args: `{"name":"geyser","saying":"finding that one"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Broke("The name could not be read.", err)
			}
			want := strings.TrimSpace(args.Name)
			if want == "" {
				return tool.Failed("No task was named, so there is nothing to look for.")
			}
			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			all, missed, err := lists.Everywhere(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}

			// A name may be a list's as readily as a task's, and he
			// says one without saying which. Asked for "the tasks for
			// secure access" on 20 October 2026, this searched task
			// titles alone, found none called that, and offered the
			// nearest single task -- from the list of that name, which
			// held several. He was told the list had one task and was
			// told the wrong one.
			//
			// Checked first, because a list that matches is an answer
			// and the nearest task is a guess.
			if on := onTheList(all, want); len(on) > 0 {
				var b strings.Builder
				fmt.Fprintf(&b, "%q is a list, not a task. It holds %d %s:",
					want, len(on), plural("task", len(on)))
				for _, t := range on {
					b.WriteString("\n- " + describeOnIts(t))
				}
				if len(missed) > 0 {
					fmt.Fprintf(&b, "\n\nThese lists could not be read: %s.",
						strings.Join(missed, ", "))
				}
				return tool.Reference(b.String())
			}

			var exact, near []string
			for _, t := range all {
				switch {
				case strings.EqualFold(strings.TrimSpace(t.Title), want):
					exact = append(exact, describeFound(t))
				case alike(t.Title, want) >= closeEnough:
					near = append(near, describeFound(t))
				}
			}

			var b strings.Builder
			switch {
			case len(exact) > 0:
				fmt.Fprintf(&b, "%q is on %s.", want, strings.Join(exact, ", and on "))
			case len(near) > 0:
				// The nearest, not a catalogue. They asked where one
				// thing is; handing back six possibles makes them do
				// the matching aloud.
				fmt.Fprintf(&b, "Nothing is called exactly %q. The nearest is %s. "+
					"Ask whether that is the one they meant, as a yes or no.", want, near[0])
			default:
				fmt.Fprintf(&b, "No task on any list is called %q, and none is close to it. "+
					"Say so, and offer to add it -- ask which list, or suggest the likeliest.", want)
			}
			if len(missed) > 0 {
				fmt.Fprintf(&b, " These lists could not be read, so this is not the whole picture: %s.",
					strings.Join(missed, ", "))
			}
			// A name that is nowhere in the tasks is marked as looked
			// for, which has the registry search the other domains and
			// append whatever it finds. Every other tool that fails to
			// find a name does this; this one never did, so a word that
			// turned out to be a reminder or a conversation was simply
			// reported missing.
			if len(exact) == 0 && len(near) == 0 {
				return tool.OK(b.String()).Looked("task", want)
			}
			return tool.OK(b.String())
		},
	}
}

// onTheList : The tasks on the list of this name, when the name is a
// list's.
//
// Exactly or near enough, by the same crude likeness a task name gets,
// because a list name arrives through speech as mangled as anything
// else. Nothing is returned when no list matches, which is what sends
// the caller on to look at task names instead.
func onTheList(all []tasks.Task, want string) []tasks.Task {
	var on []tasks.Task
	for _, t := range all {
		if strings.EqualFold(strings.TrimSpace(t.List), want) ||
			alike(t.List, want) >= closeEnough {
			on = append(on, t)
		}
	}
	return on
}

// describeOnIts : One task, without naming the list it is on, since
// every one of them is on the list just named.
func describeOnIts(t tasks.Task) string {
	out := strconv.Quote(strings.TrimSpace(t.Title))
	if t.Done {
		out += " (done)"
	}
	return out
}

// describeFound : One task and where it lives.
func describeFound(t tasks.Task) string {
	out := fmt.Sprintf("%q on %s", strings.TrimSpace(t.Title), t.List)
	if t.Done {
		out += " (done)"
	}
	return out
}

// alike : How much of the shorter name appears in the longer, from 0
// to 1, ignoring case and word order.
//
// Deliberately crude. The job is to survive speech, not to be a
// correct string distance: "jars and groovements" has to reach
// "Jarvis Improvement", and what the two share is their beginnings.
func alike(have, want string) float64 {
	h := strings.Fields(strings.ToLower(have))
	w := strings.Fields(strings.ToLower(want))
	if len(w) == 0 {
		return 0
	}
	hit := 0
	for _, wantWord := range w {
		for _, haveWord := range h {
			if shares(haveWord, wantWord) {
				hit++
				break
			}
		}
	}
	return float64(hit) / float64(len(w))
}

// shares : Whether two words begin the same way for long enough to be
// the same word heard badly.
func shares(a, b string) bool {
	n := min(len(a), len(b))
	if n < 3 {
		return a == b
	}
	if n > 4 {
		n = 4
	}
	return a[:n] == b[:n]
}
