// Package tasks keeps the person's to-do lists in Google Tasks.
//
// The third thing that sounds like something to be done, and the line
// between the three is worth stating once. An event occupies time and
// happens whether or not anybody acts: a meeting at four is four
// o'clock gone. A reminder is a sentence with a moment attached, said
// aloud and then spent. A task is neither -- it takes up no time, there
// is no instant at which it fails, and it waits until somebody ticks it
// off. "Buy a cutting board" is the shape of it.
//
// What this cannot do, and it matters: the API records a due **date**
// and discards the time. The reference is explicit that the time cannot
// be read or written, so a task asked for at three o'clock would keep
// the day and silently lose the hour. Anything with a time belongs in a
// reminder, and the tools say so rather than accepting it and dropping
// half of it.
//
// There is also no search, no batch and nothing that reports a change,
// so finding a task means listing and matching, removing four means
// four calls, and a task ticked off on a phone is invisible here until
// the next look.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/heard"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	gtasks "google.golang.org/api/tasks/v1"
)

// Scope : What this needs granted.
//
// One scope, and Google offers no middle ground: it is view-only or
// complete control, with nothing in between that would allow adding
// without allowing deletion. The restraint has to come from this side.
const Scope = "https://www.googleapis.com/auth/tasks"

// Most : The most tasks read from one list at a time.
//
// Named because the API's own default is twenty and does not say so.
// An unset call on a list that can hold twenty thousand quietly returns
// the first twenty, which is how a listing comes to report seventy-one
// things as ten.
const Most = 100

var (
	// ErrNoSuchList : No list answers to that name.
	ErrNoSuchList = errors.New("tasks: no list by that name")
	// ErrWhichList : Several answer to it and the caller must ask which.
	ErrWhichList = errors.New("tasks: more than one list by that name")
)

// Clients : Where an authenticated HTTP client comes from.
type Clients interface {
	Client(ctx context.Context, userID string) (*http.Client, error)
}

// List : One of the person's task lists.
type List struct {
	// ID : Google's identifier, needed by every other call.
	ID string
	// Title : What it is called.
	Title string
}

// Task : One thing to do.
type Task struct {
	// ID : Google's identifier, for ticking it off or removing it.
	ID string
	// Title : What it is.
	Title string
	// Notes : Anything else written about it.
	Notes string
	// Due : The day it is meant to be done, or zero when none was set.
	//
	// A day, never a time. The API discards the time portion and will
	// not give it back, so this carries midnight and means the date.
	Due time.Time
	// Done : Whether it has been ticked off.
	Done bool
	// DoneAt : When it was ticked off, zero when it has not been.
	DoneAt time.Time
	// List : The name of the list it sits on, for an answer that says
	// where something is.
	List string
}

// Lists : The person's task lists.
type Lists struct {
	clients Clients
}

// New : Builds one over a source of authenticated clients.
func New(clients Clients) *Lists { return &Lists{clients: clients} }

// service : The Tasks API, acting as the person.
func (l *Lists) service(ctx context.Context, userID string) (*gtasks.Service, error) {
	client, err := l.clients.Client(ctx, userID)
	if err != nil {
		return nil, err
	}
	return gtasks.NewService(ctx, option.WithHTTPClient(client))
}

// All : Every list the person has, in the order Google returns them.
func (l *Lists) All(ctx context.Context, userID string) ([]List, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	found, err := svc.Tasklists.List().MaxResults(100).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks: reading the lists: %w", err)
	}

	out := make([]List, 0, len(found.Items))
	for _, it := range found.Items {
		out = append(out, List{ID: it.Id, Title: it.Title})
	}
	return out, nil
}

// Make : Starts a new list.
//
// The one write to a list that cannot lose anything. Renaming and
// deleting are not here: deleting takes every task on the list with
// it, without confirmation and without undo.
func (l *Lists) Make(ctx context.Context, userID, title string) (*List, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("tasks: a list needs a name")
	}

	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	made, err := svc.Tasklists.Insert(&gtasks.TaskList{Title: title}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks: starting %s: %w", title, err)
	}
	return &List{ID: made.Id, Title: made.Title}, nil
}

// Rename : Changes what a list is called, leaving what is on it alone.
//
// A patch rather than an update. Update replaces the whole list object
// and would clear anything not sent with it; the title is the only
// field worth writing, so patching it is both smaller and safer.
func (l *Lists) Rename(ctx context.Context, userID string, which List, title string) (*List, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("tasks: a list needs a name")
	}

	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	changed, err := svc.Tasklists.Patch(which.ID, &gtasks.TaskList{Title: title}).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: renaming %s: %w", which.Title, err)
	}
	return &List{ID: changed.Id, Title: changed.Title}, nil
}

// On : What is on one list, soonest due first and undone before done.
//
// Completed tasks are included, because "what did I finish" is a
// question and because hiding them would make a count of what is left
// impossible to check. Ones cleared away are not: Google hides those
// deliberately and showing them again would resurrect things the person
// swept off on purpose.
func (l *Lists) On(ctx context.Context, userID string, which List) ([]Task, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	found, err := svc.Tasks.List(which.ID).
		MaxResults(Most).
		ShowCompleted(true).
		ShowHidden(false).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks: reading %s: %w", which.Title, err)
	}

	out := make([]Task, 0, len(found.Items))
	for _, it := range found.Items {
		out = append(out, fromGoogle(it, which.Title))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Done != out[j].Done {
			return !out[i].Done
		}
		switch {
		case out[i].Due.IsZero() && out[j].Due.IsZero():
			return false
		case out[i].Due.IsZero():
			return false
		case out[j].Due.IsZero():
			return true
		}
		return out[i].Due.Before(out[j].Due)
	})
	return out, nil
}

// Pick : The list that best answers to a name, or every list when none
// does.
//
// Not an exact match. A name arrives through speech and is more likely
// mangled than wrong, and the matching is shared with the calendar so
// "my shopping list" finds Shopping the same way "javas" finds Jarvis.
func Pick(held []List, said string) (*List, error) {
	said = strings.TrimSpace(said)
	if len(held) == 0 {
		return nil, ErrNoSuchList
	}
	// Nothing named means the one they have, or the default Google
	// makes, which is the first.
	if said == "" {
		return &held[0], nil
	}

	names := make([]string, len(held))
	for i := range held {
		names[i] = held[i].Title
	}
	at, err := heard.Best(names, said)
	switch {
	case errors.Is(err, heard.ErrSeveral):
		return nil, ErrWhichList
	case err != nil:
		return nil, ErrNoSuchList
	}
	return &held[at], nil
}

// Add : Puts a task on a list.
func (l *Lists) Add(ctx context.Context, userID string, which List, t Task) (*Task, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	made, err := svc.Tasks.Insert(which.ID, toGoogle(t)).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks: adding to %s: %w", which.Title, err)
	}
	out := fromGoogle(made, which.Title)
	return &out, nil
}

// Tick : Marks a task done, or not done.
//
// A patch rather than an update, because an update replaces the whole
// task and would clear anything not sent with it. There is no separate
// method for this in the API: done is a status like any other field.
func (l *Lists) Tick(ctx context.Context, userID string, which List, id string, done bool) (*Task, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	patch := &gtasks.Task{Status: "needsAction"}
	if done {
		patch.Status = "completed"
	} else {
		// Clearing the completion date as well. A task left marked
		// complete while its status says otherwise is a task Google
		// will not show as either.
		patch.Completed = nil
		patch.ForceSendFields = []string{"Completed"}
	}

	changed, err := svc.Tasks.Patch(which.ID, id, patch).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: changing that task: %w", err)
	}
	out := fromGoogle(changed, which.Title)
	return &out, nil
}

// Everywhere : Every task on every list, with the list each sits on.
//
// Reading one list cannot answer "where is that task" and the
// assistant said so out loud: asked about a task on a list that did
// not exist, it answered that it had no tool to search across them
// and read out the lists instead. It was right, which is why this
// exists.
//
// All of them at once, like the calendar's. Lists are read one call
// each and a person has a handful, so in order this would be a
// second of waiting for every list they keep.
func (l *Lists) Everywhere(ctx context.Context, userID string) ([]Task, []string, error) {
	held, err := l.All(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(held) == 0 {
		return nil, nil, nil
	}

	var (
		mu     sync.Mutex
		all    []Task
		missed []string
		wg     sync.WaitGroup
	)
	for _, which := range held {
		wg.Add(1)
		go func(which List) {
			defer wg.Done()
			got, err := l.On(ctx, userID, which)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// Named rather than swallowed. A search that
				// silently skipped a list would answer "it is
				// nowhere" about a list it never read.
				missed = append(missed, which.Title)
				return
			}
			all = append(all, got...)
		}(which)
	}
	wg.Wait()

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Done != all[j].Done {
			return !all[i].Done
		}
		return all[i].Title < all[j].Title
	})
	return all, missed, nil
}

// One : A single task, or nil when there is no such thing.
func (l *Lists) One(ctx context.Context, userID string, which List, id string) (*Task, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	got, err := svc.Tasks.Get(which.ID, id).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: reading that task: %w", err)
	}
	out := fromGoogle(got, which.Title)
	return &out, nil
}

// Amend : What to change about a task. A nil field is left alone.
type Amend struct {
	Title *string
	Notes *string
	// Due : The new day, or a zero time to clear the day entirely.
	Due *time.Time
}

// Empty : Whether nothing was asked for.
func (a Amend) Empty() bool { return a.Title == nil && a.Notes == nil && a.Due == nil }

// Change : Alters a task without touching what it does not mention.
//
// A patch, not an update. Update replaces the whole task and would
// clear the notes off anything renamed, the due date off anything
// re-noted, and so on for every field the caller did not think to
// send.
func (l *Lists) Change(ctx context.Context, userID string, which List, id string, a Amend) (*Task, error) {
	if a.Empty() {
		return nil, fmt.Errorf("tasks: nothing was given to change")
	}

	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	patch := &gtasks.Task{}
	if a.Title != nil {
		patch.Title = *a.Title
	}
	if a.Notes != nil {
		patch.Notes = *a.Notes
		if *a.Notes == "" {
			// An empty string means remove it, and only a null field
			// says that to Google: a patch simply leaves out what it
			// does not mention.
			patch.ForceSendFields = append(patch.ForceSendFields, "Notes")
		}
	}
	if a.Due != nil {
		if a.Due.IsZero() {
			patch.ForceSendFields = append(patch.ForceSendFields, "Due")
		} else {
			patch.Due = time.Date(a.Due.Year(), a.Due.Month(), a.Due.Day(),
				0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		}
	}

	changed, err := svc.Tasks.Patch(which.ID, id, patch).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: changing that task: %w", err)
	}
	out := fromGoogle(changed, which.Title)
	return &out, nil
}

// Remove : Deletes a task outright.
//
// Ticking off is the usual thing and keeps the record; this is for
// something that should never have been there. Google answers with an
// empty body either way, so the caller has to read the list back to
// know what happened.
func (l *Lists) Remove(ctx context.Context, userID string, which List, id string) error {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return err
	}
	if err := svc.Tasks.Delete(which.ID, id).Context(ctx).Do(); err != nil {
		if gone(err) {
			return nil
		}
		return fmt.Errorf("tasks: removing that task: %w", err)
	}
	return nil
}

// RemoveList : Deletes a list, and everything on it.
//
// The most destructive call in this API. Google takes every task with
// the list, answers with an empty body, and offers no undo; a task
// assigned from a Doc or a Chat Space is destroyed at the source as
// well. Nothing here can soften that, so the caller counts what is on
// it first and says so.
func (l *Lists) RemoveList(ctx context.Context, userID string, which List) error {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return err
	}
	if err := svc.Tasklists.Delete(which.ID).Context(ctx).Do(); err != nil {
		if gone(err) {
			return nil
		}
		return fmt.Errorf("tasks: removing %s: %w", which.Title, err)
	}
	return nil
}

// Clear : Hides every completed task on a list.
//
// Hidden rather than deleted: they are still there and still reachable
// by asking for hidden ones, but they stop appearing in every ordinary
// read. Google reports nothing about how many went.
func (l *Lists) Clear(ctx context.Context, userID string, which List) error {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return err
	}
	if err := svc.Tasks.Clear(which.ID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("tasks: clearing %s: %w", which.Title, err)
	}
	return nil
}

// Move : Moves a task to another list.
//
// Only between lists. Reordering and nesting are the same API call and
// are left alone: nobody describes a position out loud, and the
// parameters that do it are easy to pass by accident.
func (l *Lists) Move(ctx context.Context, userID string, from List, id string, to List) (*Task, error) {
	svc, err := l.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	moved, err := svc.Tasks.Move(from.ID, id).
		DestinationTasklist(to.ID).
		Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: moving that task: %w", err)
	}
	out := fromGoogle(moved, to.Title)
	return &out, nil
}

// toGoogle : A task in the form the API takes.
//
// The due date is sent as midnight UTC. Google records the date and
// throws the time away whatever is sent, so sending a real time would
// only invite somebody to believe it was kept.
func toGoogle(t Task) *gtasks.Task {
	out := &gtasks.Task{
		Title:  strings.TrimSpace(t.Title),
		Notes:  strings.TrimSpace(t.Notes),
		Status: "needsAction",
	}
	if !t.Due.IsZero() {
		out.Due = time.Date(t.Due.Year(), t.Due.Month(), t.Due.Day(),
			0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	return out
}

// fromGoogle : A task as this package sees it.
func fromGoogle(g *gtasks.Task, list string) Task {
	out := Task{
		ID: g.Id, Title: g.Title, Notes: g.Notes,
		Done: g.Status == "completed", List: list,
	}
	if g.Due != "" {
		if at, err := time.Parse(time.RFC3339, g.Due); err == nil {
			out.Due = at
		}
	}
	if g.Completed != nil && *g.Completed != "" {
		if at, err := time.Parse(time.RFC3339, *g.Completed); err == nil {
			out.DoneAt = at
		}
	}
	return out
}

// gone : Whether the API said there is no such thing.
func gone(err error) bool {
	var api *googleapi.Error
	return errors.As(err, &api) && (api.Code == http.StatusNotFound || api.Code == http.StatusGone)
}
