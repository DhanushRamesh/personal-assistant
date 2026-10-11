package elsewhere

import (
	"context"
	"strings"
	"time"

	"personal-assistant/internal/domain/calendar"
	"personal-assistant/internal/domain/job"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/remind"
	"personal-assistant/internal/domain/tasks"
)

// Ahead, Behind : How much of the diary is worth searching.
//
// A diary is unbounded in both directions and a name search over all of
// it would be a different and much slower thing. Something being "the
// water alarm" to them means it is near: a fortnight either side covers
// what a person is thinking of when they say stop that, and a month is
// one request to Google rather than many.
const (
	Behind = 14 * 24 * time.Hour
	Ahead  = 14 * 24 * time.Hour
)

// Reminders : The person's own reminders, in any state.
//
// Every state on purpose. One already cancelled or already fired is
// still the thing they are talking about, and "that is the one, and it
// already went off" is a better answer than not finding it.
func Reminders(store remind.Store, in *time.Location) Source {
	return Source{Kind: "reminder", Look: func(ctx context.Context, userID string) ([]Thing, error) {
		if store == nil {
			return nil, nil
		}
		all, err := store.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := make([]Thing, 0, len(all))
		for i := range all {
			out = append(out, Thing{
				Kind:  "reminder",
				Named: named(all[i].Title, all[i].Body),
				When:  moment(all[i].DueAt, in),
				Tool:  "reminder_cancel",
				Live:  all[i].Status == remind.Pending || all[i].Status == remind.Held,
			})
		}
		return out, nil
	}}
}

// Rules : The standing rules, his own and the ones written for him.
func Rules(store interface {
	Mine(ctx context.Context, userID, kind string, limit int, states ...job.State) ([]job.Job, error)
}, kind string, in *time.Location) Source {
	return Source{Kind: "rule", Look: func(ctx context.Context, userID string) ([]Thing, error) {
		if store == nil {
			return nil, nil
		}
		all, err := store.Mine(ctx, userID, kind, job.Limit)
		if err != nil {
			return nil, err
		}
		out := make([]Thing, 0, len(all))
		for i := range all {
			out = append(out, Thing{
				Kind:  "rule",
				Named: all[i].Name,
				When:  strings.TrimSpace(all[i].Schedule),
				Tool:  "rule_cancel",
				Live:  all[i].State == job.Active,
			})
		}
		return out, nil
	}}
}

// Tasks : Everything on every list.
func Tasks(lists interface {
	Everywhere(ctx context.Context, userID string) ([]tasks.Task, []string, error)
}, in *time.Location) Source {
	return Source{Kind: "task", Look: func(ctx context.Context, userID string) ([]Thing, error) {
		if lists == nil {
			return nil, nil
		}
		// The second return is the lists that could not be read. A
		// partial answer is the right one here: this is already the
		// fallback, and some of it beats none of it.
		all, _, err := lists.Everywhere(ctx, userID)
		if err != nil {
			return nil, err
		}
		out := make([]Thing, 0, len(all))
		for i := range all {
			out = append(out, Thing{
				Kind:  "task",
				Named: all[i].Title,
				When:  moment(all[i].Due, in),
				Tool:  "task_remove",
				Live:  !all[i].Done,
			})
		}
		return out, nil
	}}
}

// Diary : What is on any of their calendars, near today.
func Diary(d interface {
	Everywhere(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, []string, error)
}, now func() time.Time, in *time.Location) Source {
	return Source{Kind: "diary entry", Look: func(ctx context.Context, userID string) ([]Thing, error) {
		if d == nil || now == nil {
			return nil, nil
		}
		at := now()
		all, _, err := d.Everywhere(ctx, userID, at.Add(-Behind), at.Add(Ahead))
		if err != nil {
			return nil, err
		}
		out := make([]Thing, 0, len(all))
		for i := range all {
			out = append(out, Thing{
				Kind:  "diary entry",
				Named: all[i].Title,
				When:  moment(all[i].Starts, in),
				Tool:  "calendar_events",
				Live:  true,
			})
		}
		return out, nil
	}}
}

// Remembered : What the assistant has been asked to keep.
//
// Last of the five and the least likely to be it, but his list named
// it, and a thing he told it to remember is a thing he can ask it to
// forget.
func Remembered(store memory.Store) Source {
	return Source{Kind: "memory", Look: func(ctx context.Context, userID string) ([]Thing, error) {
		if store == nil {
			return nil, nil
		}
		all, err := store.All(ctx, userID, memory.TierAlways)
		if err != nil {
			return nil, err
		}
		out := make([]Thing, 0, len(all))
		for i := range all {
			out = append(out, Thing{
				Kind:  "memory",
				Named: named(all[i].Subject, all[i].Body),
				Tool:  "memory_forget",
				Live:  true,
			})
		}
		return out, nil
	}}
}

// named : What a thing is called, falling back to what it says.
//
// A reminder's title is usually its whole text and sometimes empty,
// and a thing with no name cannot be matched by one.
func named(title, body string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return strings.TrimSpace(body)
}

// moment : When something is, in words, or nothing for no time.
//
// Short on purpose: these go in a list the model reads out, and the
// year is never the thing in question when somebody says stop that.
func moment(at time.Time, in *time.Location) string {
	if at.IsZero() {
		return ""
	}
	if in == nil {
		in = time.UTC
	}
	return "at " + at.In(in).Format("3:04 pm on Monday 2 January")
}

// Instead : What to add to a tool's result, satisfying tool.Elsewhere.
func (s Search) Instead(ctx context.Context, userID, named, except string) string {
	return Says(named, s.Found(ctx, userID, named, except))
}
