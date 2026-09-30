package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// remove : Deletes tasks outright.
//
// Apart from task_done on purpose. Ticking off is what somebody means
// nine times in ten, keeps the record and can be undone; this is for
// something that should never have been on the list. Saying them
// differently is what stops the model reaching for the destructive one
// when the harmless one was meant.
func remove(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_remove",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Delete one or more tasks from a list, so they are gone rather than ticked off.",
		),
		UseWhen: prompt.Text(
			"They want something off the list that should not have been there: a duplicate, a mistake, something no longer wanted.",
			"Several at once go in one call.",
		),
		Avoid: prompt.Text(
			"Not for something they have finished -- that is task_done, which keeps the record and can be undone.",
			"There is no undo here.",
			"Never guess an identifier: it comes from task_list in this same turn.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"ids"},
			Properties: map[string]tool.Property{
				"ids": {
					Type:     "array",
					MinItems: ptr(1),
					MaxItems: ptr(20),
					Items:    &tool.Property{Type: "string", Pattern: idPattern},
					Description: prompt.Text(
						"The identifiers, exactly as task_list gave them.",
						"Never guessed, and never a description of the task.",
					),
				},
				"list": {Type: "string", Description: "Which list. Left out, the one they keep."},
			},
		},
		Examples: []tool.Example{
			{Ask: "delete that one, it was a mistake", Args: `{"ids":["abc123"],"saying":"taking that off"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				IDs  []string `json:"ids"`
				List string   `json:"list"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if bad := ready(lists, in); bad != nil {
				return *bad
			}
			if len(args.IDs) == 0 {
				return tool.Failed("Which task? Use task_list to find its identifier.")
			}

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			before, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			for _, id := range args.IDs {
				if err := lists.Remove(ctx, in.Caller.UserID, *which, id); err != nil {
					return whenTrouble(err)
				}
			}

			// Counted afterwards, because the API answers a deletion
			// with an empty body and says nothing about what went.
			after, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}
			return tool.RemovedMany("Taken off "+which.Title,
				len(args.IDs), len(before), len(after), "task")
		},
	}
}

// clear : Sweeps the completed tasks out of the way.
func clear(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_clear",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Put every finished task on a list out of sight.",
			"They are hidden rather than destroyed, and stop appearing in ordinary reads.",
		),
		UseWhen: prompt.Text(
			"They want a list tidied, or the done ones out of the way.",
		),
		Avoid: prompt.Text(
			"It takes all of them at once and there is no choosing which.",
			"Nothing still to do is touched.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"list": {Type: "string", Description: "Which list. Left out, the one they keep."},
			},
		},
		Examples: []tool.Example{
			{Ask: "clear the finished ones", Args: `{"saying":"tidying the list"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				List string `json:"list"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if bad := ready(lists, in); bad != nil {
				return *bad
			}
			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			before, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}
			done := len(before) - left(before)
			if done == 0 {
				return tool.OK("Nothing on " + which.Title + " is finished, so nothing was cleared. Say so.")
			}

			if err := lists.Clear(ctx, in.Caller.UserID, *which); err != nil {
				return whenTrouble(err)
			}
			after, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}
			return tool.Removed(fmt.Sprintf("Cleared the finished tasks from %s", which.Title),
				len(before), len(after), "task")
		},
	}
}

// move : Sends a task to another list.
func move(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_move",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Move one or more tasks from one list to another.",
			"Nothing is copied: the task leaves the first list.",
		),
		UseWhen: prompt.Text(
			"Something was put on the wrong list, or belongs with a different set of things.",
			"Several at once go in one call.",
		),
		Avoid: prompt.Text(
			"Not for reordering within a list, which this does not do.",
			"Never guess an identifier: it comes from task_list in this same turn.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"ids", "to"},
			Properties: map[string]tool.Property{
				"ids": {
					Type:        "array",
					MinItems:    ptr(1),
					MaxItems:    ptr(20),
					Items:       &tool.Property{Type: "string", Pattern: idPattern},
					Description: "The identifiers, exactly as task_list gave them.",
				},
				"from": {Type: "string", Description: "The list they are on now. Left out, the one they keep."},
				"to":   {Type: "string", Description: "The list to move them to."},
			},
		},
		Examples: []tool.Example{
			{Ask: "move milk to the shopping list", Args: `{"ids":["abc123"],"to":"shopping","saying":"moving that"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				IDs  []string `json:"ids"`
				From string   `json:"from"`
				To   string   `json:"to"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if bad := ready(lists, in); bad != nil {
				return *bad
			}
			if len(args.IDs) == 0 || strings.TrimSpace(args.To) == "" {
				return tool.Failed("Say which tasks, and which list they should go to.")
			}

			from, _, err := pick(ctx, lists, in.Caller.UserID, args.From)
			if err != nil {
				return whenTrouble(err)
			}
			to, _, err := pick(ctx, lists, in.Caller.UserID, args.To)
			if err != nil {
				return whenTrouble(err)
			}
			if from.ID == to.ID {
				return tool.OK("Those are already on " + to.Title + ", so nothing was moved. Say so.")
			}

			var moved []string
			for _, id := range args.IDs {
				t, err := lists.Move(ctx, in.Caller.UserID, *from, id, *to)
				if err != nil {
					return whenTrouble(err)
				}
				if t == nil {
					continue
				}
				moved = append(moved, t.Title)
			}
			if len(moved) == 0 {
				return tool.Failed("None of those are on " + from.Title +
					". Read it with task_list and work from what comes back.")
			}
			return tool.Changed("Moved them",
				tool.Change{What: "the list", From: from.Title, To: to.Title})
		},
	}
}

// removeAList : Deletes a list and everything on it.
//
// The most destructive thing in this domain, and the one place where
// the count is said before rather than after. Google gives no undo and
// answers with an empty body, and a task assigned from a Doc or a Chat
// Space is destroyed at its source as well.
func removeAList(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_list_remove",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Delete a whole to-do list, and everything on it.",
		),
		UseWhen: prompt.Text(
			"They ask for a list to be got rid of, and mean the list rather than the things on it.",
		),
		Avoid: prompt.Block(
			prompt.Text(
				"Everything on the list goes with it, and there is no undo.",
				"If they meant to tidy up, task_clear puts the finished ones out of sight and keeps them.",
				"If they meant one thing, task_remove takes one thing.",
			),
			prompt.Text(
				"Say how many tasks went with it. The answer is the only record of what was lost.",
			),
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"list"},
			Properties: map[string]tool.Property{
				"list": {Type: "string", Description: "Which list to delete."},
			},
		},
		Examples: []tool.Example{
			{Ask: "delete the shopping list", Args: `{"list":"shopping","saying":"removing it"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				List string `json:"list"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if bad := ready(lists, in); bad != nil {
				return *bad
			}
			if strings.TrimSpace(args.List) == "" {
				return tool.Failed("Which list? Deleting one takes everything on it, so it has to be named.")
			}

			which, held, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			if len(held) == 1 {
				return tool.Failed("That is their only list, and Google will not leave them with none. " +
					"Say so, and offer to clear it instead.")
			}
			// Counted before, because afterwards there is nothing to
			// count and the answer is the only record of what went.
			on, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			if err := lists.RemoveList(ctx, in.Caller.UserID, *which); err != nil {
				return whenTrouble(err)
			}
			after, err := lists.All(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			for _, l := range after {
				if l.ID == which.ID {
					return tool.Unverified("Deleting "+which.Title,
						"reading the lists back still shows it")
				}
			}

			return tool.Removed(fmt.Sprintf(
				"Deleted the list %s, and the %d %s on it went with it",
				which.Title, len(on), plural("task", len(on))),
				len(held), len(after), "list")
		},
	}
}

// ready : Whether the tool can run at all, or why not.
func ready(lists Lists, in tool.Invocation) *tool.Result {
	if lists == nil {
		bad := tool.Failed("There are no task lists on this server.")
		return &bad
	}
	if in.Caller.UserID == "" {
		bad := tool.Failed("This request did not come from a known person.")
		return &bad
	}
	return nil
}
