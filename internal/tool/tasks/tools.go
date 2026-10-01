package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/heard"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// theLists : What lists the person keeps.
func theLists(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_lists",
		Domain: "task",
		Lists:  true,
		Purpose: prompt.Text(
			"Name the person's to-do lists.",
			"A task always sits on one of these, so this is how to find out which lists exist and what they are called.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"Anything to do with tasks, things to do, or a list -- asked about, mentioned in passing, or named while asking for something else.",
				"Read the lists before you answer, every time, even when they did not ask what lists they have.",
			),
			prompt.Text(
				"Above all before saying a list does not exist.",
				"A name reached you through speech and arrives mangled far more often than it arrives wrong: \"John's\" may be the list called Jonathan, or Work, or one you have simply never read.",
				"Asked to move something to a list you do not know, read these and name the nearest -- do not answer that there is no such list.",
			),
		),
		Avoid: prompt.Text(
			"Do not call it to find a task: it names the lists, not what is on them.",
			"task_list reads what is on one.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "what lists do I have", Args: `{"saying":"looking at your lists"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			held, err := lists.All(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			if len(held) == 0 {
				return tool.OK("They keep no task lists at all.")
			}

			var b strings.Builder
			fmt.Fprintf(&b, "They keep %d %s:", len(held), plural("list", len(held)))
			for _, l := range held {
				b.WriteString("\n- " + l.Title)
			}
			return tool.Reference(b.String())
		},
	}
}

// startAList : Begins a new list.
//
// The only write to a list offered. Renaming is missing because nobody
// has wanted it; deleting is missing because it takes every task on
// the list with it, with no confirmation and no undo, and a mis-heard
// word should not be able to reach that.
func startAList(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_list_add",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Start a new to-do list, such as Shopping or Work.",
			"Lists are only a name; the things to do go on them with task_add.",
		),
		UseWhen: prompt.Text(
			"They ask for a new list, or want something kept apart from what is already there.",
		),
		Avoid: prompt.Text(
			"Not for adding something to do -- that is task_add, and it needs a list that already exists.",
			"Do not start a list that is already there under a slightly different name: read task_lists first and use the one they have.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"title"},
			Properties: map[string]tool.Property{
				"title": {Type: "string", Description: "What to call it."},
			},
		},
		Examples: []tool.Example{
			{Ask: "make a shopping list", Args: `{"title":"Shopping","saying":"starting one"}`},
			{Ask: "start a list for the house", Args: `{"title":"House","saying":"starting one"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if strings.TrimSpace(args.Title) == "" {
				return tool.Failed("What should the list be called?")
			}

			before, err := lists.All(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			// One that is already there under a near-enough name is the
			// one they meant. Two lists called Shopping and Shoping is a
			// mess nobody asked for, and speech produces exactly that.
			if at, err := heard.Best(names(before), args.Title); err == nil {
				return tool.OK(fmt.Sprintf(
					"There is already a list called %s, so nothing was started. "+
						"Say that, and use it unless they want another one alongside it.",
					before[at].Title))
			}

			made, err := lists.Make(ctx, in.Caller.UserID, args.Title)
			if err != nil {
				return whenTrouble(err)
			}
			after, err := lists.All(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			return tool.Added("Started the list", made.Title, made.Title,
				len(before), len(after), "list")
		},
	}
}

// names : What the lists are called.
func names(held []tasks.List) []string {
	out := make([]string, len(held))
	for i := range held {
		out[i] = held[i].Title
	}
	return out
}

// renameAList : Changes what a list is called.
//
// Safe in a way deleting is not: nothing on the list moves, and a name
// put back is the same list again. That is why this is here and
// deleting is not.
func renameAList(lists Lists) tool.Tool {
	return tool.Tool{
		Name:   "task_list_rename",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Change what a to-do list is called.",
			"What is on it is untouched; only the name changes.",
		),
		UseWhen: prompt.Text(
			"They want a list called something else.",
			"Also when a list was started under a mis-heard name and they are putting it right.",
		),
		Avoid: prompt.Text(
			"Not for moving a task between lists, which this cannot do.",
			"Not for starting a list: task_list_add does that.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"title"},
			Properties: map[string]tool.Property{
				"list": {
					Type: "string",
					Description: prompt.Text(
						"Which list to rename. Pass what they called it even if it sounds wrong.",
						"Left out, the one they keep.",
					),
				},
				"title": {Type: "string", Description: "What to call it now."},
			},
		},
		Examples: []tool.Example{
			{Ask: "rename my list to Errands", Args: `{"title":"Errands","saying":"renaming it"}`},
			{Ask: "call the shopping list Groceries", Args: `{"list":"shopping","title":"Groceries","saying":"renaming it"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				List  string `json:"list"`
				Title string `json:"title"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			title := strings.TrimSpace(args.Title)
			if title == "" {
				return tool.Failed("What should it be called instead?")
			}

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			was := which.Title
			if was == title {
				return tool.OK("It is already called " + title + ", so nothing was changed. Say so.")
			}

			changed, err := lists.Rename(ctx, in.Caller.UserID, *which, title)
			if err != nil {
				return whenTrouble(err)
			}
			if changed == nil {
				return tool.Failed("There is no list by that name any more. Read them with task_lists.")
			}

			// Read back. A rename that did not take reads exactly like
			// one that did, from here.
			after, err := lists.All(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			var found bool
			for _, l := range after {
				if l.ID == which.ID && l.Title == title {
					found = true
				}
			}
			if !found {
				return tool.Unverified("Renaming "+was,
					"reading the lists back still does not show the new name")
			}

			return tool.Changed("Renamed the list",
				tool.Change{What: "the name", From: was, To: changed.Title})
		},
	}
}

// onAList : What is on one list.
func onAList(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_list",
		Domain: "task",
		Lists:  true,
		Purpose: prompt.Text(
			"Read what is on a to-do list: what is still to do, and what has been done.",
			"Left out, the list is the one they keep, or the first of several.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"Any question about what they have to do, what is on a list, what is left, or whether something is already on there.",
				"Call it every time, including before adding something: the thing may be on the list already, and saying so is better than a second copy of it.",
			),
			prompt.Text(
				"A task is not a reminder and not an event.",
				"A reminder is said aloud at a moment; an event takes up time in the diary; a task has no time and waits until it is ticked off.",
				"Do not answer a question about one from the other.",
			),
		),
		Avoid: prompt.Text(
			"Do not answer from what was said earlier in the conversation.",
			"A list changes on their phone as well as here, and a claim about it needs this tool to have just run.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"list": {
					Type: "string",
					Description: prompt.Text(
						"Which list to read.",
						"Pass what they called it even if it sounds wrong: names arrive mangled from speech and are matched by likeness.",
						"Left out, the list they keep.",
					),
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what is on my list", Args: `{"saying":"reading your list"}`},
			{Ask: "what do I have to do", Args: `{"saying":"looking at what you have to do"}`},
			{Ask: "is milk on the shopping list", Args: `{"list":"shopping","saying":"checking the shopping list"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				List string `json:"list"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			on, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			var b strings.Builder
			undone := left(on)
			switch {
			case len(on) == 0:
				b.WriteString("There is nothing on " + which.Title + " at all.")
			default:
				fmt.Fprintf(&b, "There are %d %s on %s, %d still to do. ",
					len(on), plural("task", len(on)), which.Title, undone)
				b.WriteString("These carry no time of day: a task is a thing to be done, " +
					"not a moment. Only the ones marked with an identifier can be ticked off.")
				for _, t := range on {
					b.WriteString("\n- " + describe(t, clock.where()) + "  [id " + t.ID + "]")
				}
			}
			if !heard.Exactly(which.Title, args.List) && strings.TrimSpace(args.List) != "" {
				b.WriteString("\n\n" + tool.BySound("list", args.List, which.Title))
			}
			return tool.Reference(b.String())
		},
	}
}

// add : Puts something on a list.
func add(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_add",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Put one or more things on a to-do list.",
			"A task waits until it is ticked off and takes up no time in the day.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"They want something written down to do, with no time attached: shopping, an errand, something to look into.",
				"Several at once go in one call rather than one call each.",
			),
			prompt.Text(
				"Not for anything with a time. A task cannot hold one -- Google records the day and discards the hour, and it cannot be read back.",
				"If they said a time, that is a reminder, and reminder_set is the tool.",
				"If it takes up time and they will attend it, that is an event for the diary.",
			),
		),
		Avoid: prompt.Text(
			"Do not accept a time of day and put it in the notes instead.",
			"They asked for something to happen at a moment and a task will not do it; say so and offer a reminder.",
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"titles"},
			Properties: map[string]tool.Property{
				"titles": {
					Type:        "array",
					MinItems:    ptr(1),
					MaxItems:    ptr(20),
					Items:       &tool.Property{Type: "string"},
					Description: "What to put on the list, one entry each.",
				},
				"list": {
					Type:        "string",
					Description: "Which list. Left out, the one they keep.",
				},
				"due": {
					Type: "string",
					Description: prompt.Text(
						"The day it should be done, as 2026-10-02.",
						"A day only: a time of day cannot be stored and will be lost, so do not pass one.",
					),
				},
				"notes": {
					Type:        "string",
					Description: "Anything else worth writing under it.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "add milk to my shopping list", Args: `{"titles":["milk"],"list":"shopping","saying":"adding that"}`},
			{Ask: "put bread and eggs on the list", Args: `{"titles":["bread","eggs"],"saying":"adding those"}`},
			{Ask: "remind me to order a cutting board", Args: `{"titles":["order a cutting board"],"saying":"writing that down"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Titles []string `json:"titles"`
				List   string   `json:"list"`
				Due    string   `json:"due"`
				Notes  string   `json:"notes"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if len(args.Titles) == 0 {
				return tool.Failed("Nothing was given to add. Say what should go on the list.")
			}

			var due time.Time
			if d := strings.TrimSpace(args.Due); d != "" {
				at, err := time.ParseInLocation(time.DateOnly, d, clock.where())
				if err != nil {
					return tool.Failed(fmt.Sprintf(
						"%q is not a day this understands; write it as 2026-10-02. "+
							"A task holds a day and never a time.", d))
				}
				due = at
			}

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			before, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			var made []string
			for _, title := range args.Titles {
				title = strings.TrimSpace(title)
				if title == "" {
					continue
				}
				t, err := lists.Add(ctx, in.Caller.UserID, *which,
					tasks.Task{Title: title, Notes: args.Notes, Due: due})
				if err != nil {
					return whenTrouble(err)
				}
				made = append(made, describe(*t, clock.where()))
			}
			if len(made) == 0 {
				return tool.Failed("Nothing was given to add. Say what should go on the list.")
			}

			// Read back, because a write that did not take is the one
			// failure nobody can hear.
			after, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}
			if len(after) != len(before)+len(made) {
				return tool.Unverified("Adding those to "+which.Title,
					"reading the list back does not show all of them")
			}

			return tool.Added("Put on "+which.Title, strings.Join(made, ", "),
				strings.Join(made, ", "), len(before), len(after), "task")
		},
	}
}

// tick : Marks something done.
func tick(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_done",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Tick one or more tasks off a list, or put one back to be done again.",
			"Ticking off keeps the task and marks it finished; nothing is destroyed.",
		),
		UseWhen: prompt.Text(
			"They say something is done, finished, bought, or dealt with.",
			"Several at once go in one call.",
			"Also to undo a mistake: pass done as false to put one back.",
		),
		Avoid: prompt.Text(
			"Never guess an identifier. It comes from task_list in this same turn and nowhere else.",
			"If they named something rather than an identifier, read the list and match it there.",
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
				"list": {
					Type:        "string",
					Description: "Which list they are on. Left out, the one they keep.",
				},
				"done": {
					Type:        "boolean",
					Description: "True to tick off, false to put back. Left out, true.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "I got the milk", Args: `{"ids":["abc123"],"saying":"ticking that off"}`},
			{Ask: "done with both of those", Args: `{"ids":["abc123","def456"],"saying":"ticking those off"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				IDs  []string `json:"ids"`
				List string   `json:"list"`
				Done *bool    `json:"done"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if lists == nil {
				return tool.Failed("There are no task lists on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if len(args.IDs) == 0 {
				return tool.Failed("Which task? Use task_list to find its identifier.")
			}
			done := args.Done == nil || *args.Done

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}
			before, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			var changed []string
			var missing []string
			for _, id := range args.IDs {
				t, err := lists.Tick(ctx, in.Caller.UserID, *which, id, done)
				if err != nil {
					return whenTrouble(err)
				}
				if t == nil {
					missing = append(missing, id)
					continue
				}
				changed = append(changed, t.Title)
			}
			if len(changed) == 0 {
				return tool.Failed("None of those are on " + which.Title +
					". Read it with task_list and work from what comes back.")
			}

			after, err := lists.On(ctx, in.Caller.UserID, *which)
			if err != nil {
				return whenTrouble(err)
			}

			var b strings.Builder
			what := "Ticked off"
			if !done {
				what = "Put back"
			}
			fmt.Fprintf(&b, "%s: %s. ", what, strings.Join(changed, ", "))
			fmt.Fprintf(&b, "There were %d still to do on %s before, and there are %d now.",
				left(before), which.Title, left(after))
			if len(missing) > 0 {
				b.WriteString(" These were not on the list at all: " + strings.Join(missing, ", ") + ".")
			}
			return tool.OK(b.String())
		},
	}
}

// ptr : A pointer to a value, for the schema's optional numbers.
func ptr(n int) *int { return &n }

// plural : A noun for a count, for something read aloud.
func plural(noun string, n int) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// amend : Changes a task that is already on a list.
//
// Every other domain has one of these -- calendar_update,
// reminder_update -- and tasks went without, so a task written down
// wrongly could only be deleted and added again. That loses the
// identifier, and loses the record if it was half done.
func amend(lists Lists, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "task_update",
		Domain: "task",
		Writes: true,
		Purpose: prompt.Text(
			"Change a task that is already on a list: what it says, its notes, or the day it is due.",
			"What is not mentioned is left as it was.",
		),
		UseWhen: prompt.Text(
			"They want a task worded differently, or the day changed, or something added under it.",
			"Also when it was written down from a mis-hearing and they are putting it right.",
		),
		Avoid: prompt.Block(
			prompt.Text(
				"Not for finishing something -- that is task_done -- and not for moving it to another list, which is task_move.",
				"Never guess an identifier: it comes from task_list in this same turn.",
			),
			prompt.Text(
				"A task cannot hold a time of day. Passing one loses the hour without saying so, and if they asked for a time they wanted a reminder.",
			),
		),
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Pattern:     idPattern,
					Description: "The identifier, exactly as task_list gave it.",
				},
				"list":  {Type: "string", Description: "Which list it is on. Left out, the one they keep."},
				"title": {Type: "string", Description: "What it should say instead."},
				"notes": {Type: "string", Description: "New notes. An empty string clears them."},
				"due": {
					Type: "string",
					Description: prompt.Text(
						"A new day, as 2026-10-02, or an empty string to take the day off it.",
						"A day only: a time of day cannot be stored.",
					),
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "change milk to oat milk", Args: `{"id":"abc123","title":"oat milk","saying":"changing that"}`},
			{Ask: "make the cutting board due Friday", Args: `{"id":"abc123","due":"2026-10-02","saying":"changing that"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID    string  `json:"id"`
				List  string  `json:"list"`
				Title *string `json:"title"`
				Notes *string `json:"notes"`
				Due   *string `json:"due"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if bad := ready(lists, in); bad != nil {
				return *bad
			}
			if strings.TrimSpace(args.ID) == "" {
				return tool.Failed("Which task? Use task_list to find its identifier.")
			}

			which, _, err := pick(ctx, lists, in.Caller.UserID, args.List)
			if err != nil {
				return whenTrouble(err)
			}

			// Read it first, both to have something to compare against
			// and so a wrong identifier is caught before anything is
			// written.
			before, err := lists.One(ctx, in.Caller.UserID, *which, args.ID)
			if err != nil {
				return whenTrouble(err)
			}
			if before == nil {
				return tool.Failed("There is no such task on " + which.Title +
					". Read it with task_list and work from what comes back.")
			}
			was := *before

			change := tasks.Amend{Title: args.Title, Notes: args.Notes}
			if args.Due != nil {
				if d := strings.TrimSpace(*args.Due); d == "" {
					var none time.Time
					change.Due = &none
				} else {
					at, err := time.ParseInLocation(time.DateOnly, d, clock.where())
					if err != nil {
						return tool.Failed(fmt.Sprintf(
							"%q is not a day this understands; write it as 2026-10-02. "+
								"A task holds a day and never a time.", d))
					}
					change.Due = &at
				}
			}
			if change.Empty() {
				return tool.Failed("Nothing was given to change. Say what should be different.")
			}

			after, err := lists.Change(ctx, in.Caller.UserID, *which, args.ID, change)
			if err != nil {
				return whenTrouble(err)
			}
			if after == nil {
				return tool.Failed("There is no such task on " + which.Title + " any more.")
			}

			return tool.Changed("Changed the task",
				tool.Change{What: "the wording", From: was.Title, To: after.Title},
				tool.Change{What: "the note", From: was.Notes, To: after.Notes},
				tool.Change{What: "the day", From: day(was.Due, clock), To: day(after.Due, clock)},
			)
		},
	}
}

// day : A due date as it would be read out, or "none".
func day(at time.Time, clock Clock) string {
	if at.IsZero() {
		return "none"
	}
	return at.In(clock.where()).Format("Monday 2 January")
}
