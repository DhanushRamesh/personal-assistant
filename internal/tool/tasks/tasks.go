// Package tasks offers the person's to-do lists to the model.
//
// Four tools out of the API's fourteen methods, and the omissions are
// the point. There is no tool that deletes a list, because deleting one
// takes every task on it with no confirmation and no undo, and a
// mis-heard word should not be able to reach that. There is none that
// clears completed tasks in bulk, none that reorders, and none that
// makes subtasks -- nobody asks for any of that out loud, and each one
// would be another tool for the model to confuse with the others.
//
// Removing a single task is also left out for now. Ticking it off is
// what somebody means nine times in ten, it keeps the record, and it
// can be undone.
package tasks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/tasks"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Clock : What the tools need to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written day means.
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// Lists : What the tools need from the task store.
type Lists interface {
	All(ctx context.Context, userID string) ([]tasks.List, error)
	On(ctx context.Context, userID string, which tasks.List) ([]tasks.Task, error)
	Add(ctx context.Context, userID string, which tasks.List, t tasks.Task) (*tasks.Task, error)
	Tick(ctx context.Context, userID string, which tasks.List, id string, done bool) (*tasks.Task, error)
	Make(ctx context.Context, userID, title string) (*tasks.List, error)
	Rename(ctx context.Context, userID string, which tasks.List, title string) (*tasks.List, error)
	Remove(ctx context.Context, userID string, which tasks.List, id string) error
	RemoveList(ctx context.Context, userID string, which tasks.List) error
	Clear(ctx context.Context, userID string, which tasks.List) error
	Move(ctx context.Context, userID string, from tasks.List, id string, to tasks.List) (*tasks.Task, error)
	One(ctx context.Context, userID string, which tasks.List, id string) (*tasks.Task, error)
	Change(ctx context.Context, userID string, which tasks.List, id string, a tasks.Amend) (*tasks.Task, error)
	Everywhere(ctx context.Context, userID string) ([]tasks.Task, []string, error)
}

// All : Every task tool, in the order they are offered.
func All(lists Lists, clock Clock) []tool.Tool {
	return []tool.Tool{
		theLists(lists),
		findATask(lists, clock),
		startAList(lists),
		renameAList(lists),
		onAList(lists, clock),
		add(lists, clock),
		tick(lists, clock),
		amend(lists, clock),
		remove(lists),
		clear(lists),
		move(lists, clock),
		removeAList(lists),
	}
}

// idPattern : The shape of a task identifier, so a description of a
// task cannot be passed off as one.
const idPattern = "^[A-Za-z0-9_-]{5,256}$"

// whenTrouble : What to tell the model when Google cannot be reached.
func whenTrouble(err error) tool.Result {
	if errors.Is(err, google.ErrNotConnected) {
		return tool.Failed("Their Google account is not connected, so there are no lists to read. " +
			"Say so, and that they can connect it in the settings.")
	}
	if errors.Is(err, tasks.ErrWhichList) {
		return tool.Failed("More than one list answers to that name. Use task_lists and ask which.")
	}
	if errors.Is(err, tasks.ErrNoSuchList) {
		return tool.Failed("There is no list by that name. Use task_lists to see what there is.")
	}
	return tool.Failed("Their task lists could not be reached just now. " +
		"Say that, and that it says nothing about what is on them.")
}

// pick : The list a name means, reading them first.
func pick(ctx context.Context, lists Lists, userID, said string) (*tasks.List, []tasks.List, error) {
	held, err := lists.All(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	which, err := tasks.Pick(held, said)
	if err != nil {
		return nil, held, err
	}
	return which, held, nil
}

// describe : One task in a line, for the model to read back.
func describe(t tasks.Task, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(t.Title)
	if !t.Due.IsZero() {
		b.WriteString(", due " + t.Due.In(loc).Format("Monday 2 January"))
	}
	if t.Done {
		b.WriteString(", done")
	}
	if n := strings.TrimSpace(t.Notes); n != "" {
		b.WriteString(" -- " + n)
	}
	return b.String()
}

// left : How many of a set are not yet done.
func left(all []tasks.Task) int {
	n := 0
	for _, t := range all {
		if !t.Done {
			n++
		}
	}
	return n
}
