package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	gcal "google.golang.org/api/calendar/v3"
)

// MaxList : The most events returned at once.
const MaxList = 50

// Event : Something in the diary.
type Event struct {
	// ID : Google's identifier, for changing or cancelling it later.
	ID string
	// Title : What it is called.
	Title string
	// Where : The place, if one was given.
	Where string
	// Notes : Anything else said about it.
	Notes string
	// Starts, Ends : When. For an all-day event the times are midnight
	// and AllDay is set.
	Starts time.Time
	Ends   time.Time
	// AllDay : Whether it takes the whole day rather than a slot.
	AllDay bool
	// Mine : Whether it is on the assistant's own calendar, and so
	// something it can change. Anything else it only knows the shape of.
	Mine bool
}

// Add : Puts an event on the assistant's own calendar.
func (d *Diary) Add(ctx context.Context, userID string, e Event) (*Event, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return nil, err
	}

	made, err := svc.Events.Insert(id, toGoogle(e, d.location)).Context(ctx).Do()
	if gone(err) {
		// The calendar was deleted by hand since it was last seen.
		// Made again rather than failing: the person asked for an
		// event, not for a report about bookkeeping.
		d.forget(ctx, userID)
		if id, err = d.mine(ctx, userID, svc); err != nil {
			return nil, err
		}
		made, err = svc.Events.Insert(id, toGoogle(e, d.location)).Context(ctx).Do()
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: adding %q: %w", e.Title, err)
	}
	return fromGoogle(made, true, d.location), nil
}

// Amend : What to change about an event. A nil field is left alone.
//
// Pointers rather than zero values, because clearing a location and
// leaving it untouched are different intentions and an empty string
// cannot say which was meant.
type Amend struct {
	Title  *string
	Where  *string
	Notes  *string
	Starts *time.Time
	Ends   *time.Time
}

// Empty : Whether nothing was actually given to change.
func (a Amend) Empty() bool {
	return a.Title == nil && a.Where == nil && a.Notes == nil &&
		a.Starts == nil && a.Ends == nil
}

// Update : Changes an event on the assistant's own calendar.
//
// A patch rather than a replace, so anything not named keeps what it
// had. Without this the only way to change a name was to cancel and
// recreate, which loses the identifier and leaves two events behind
// when the cancel fails -- which is exactly what happened on 27
// September 2026.
func (d *Diary) Update(ctx context.Context, userID, eventID string, a Amend) (*Event, error) {
	if a.Empty() {
		return nil, fmt.Errorf("calendar: nothing was given to change")
	}

	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return nil, err
	}

	patch := &gcal.Event{}
	var clear []string
	if a.Title != nil {
		patch.Summary = *a.Title
	}
	if a.Where != nil {
		patch.Location = *a.Where
		if *a.Where == "" {
			clear = append(clear, "Location")
		}
	}
	if a.Notes != nil {
		patch.Description = *a.Notes
		if *a.Notes == "" {
			clear = append(clear, "Description")
		}
	}
	if a.Starts != nil {
		patch.Start = &gcal.EventDateTime{
			DateTime: a.Starts.In(d.location).Format(time.RFC3339),
			TimeZone: d.location.String(),
		}
	}
	if a.Ends != nil {
		patch.End = &gcal.EventDateTime{
			DateTime: a.Ends.In(d.location).Format(time.RFC3339),
			TimeZone: d.location.String(),
		}
	}
	if len(clear) > 0 {
		patch.NullFields = clear
	}

	changed, err := svc.Events.Patch(id, eventID, patch).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, fmt.Errorf("calendar: there is no such event to change")
		}
		return nil, fmt.Errorf("calendar: changing that event: %w", err)
	}
	return fromGoogle(changed, true, d.location), nil
}

// One : A single event from the assistant's own calendar, for reading
// back what a change actually did.
func (d *Diary) One(ctx context.Context, userID, eventID string) (*Event, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return nil, err
	}

	got, err := svc.Events.Get(id, eventID).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("calendar: reading that event: %w", err)
	}
	return fromGoogle(got, true, d.location), nil
}

// Cancel : Removes an event from the assistant's own calendar.
//
// Only its own. Nothing else is reachable with these scopes, which is
// the point of them: there is no way for this to delete a real meeting
// by mistake.
func (d *Diary) Cancel(ctx context.Context, userID, eventID string) error {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return err
	}

	if err := svc.Events.Delete(id, eventID).Context(ctx).Do(); err != nil {
		if gone(err) {
			return fmt.Errorf("calendar: there is no such event to cancel")
		}
		return fmt.Errorf("calendar: cancelling: %w", err)
	}
	return nil
}

// Mine : What is on the assistant's own calendar between two times,
// soonest first.
func (d *Diary) Mine(ctx context.Context, userID string, from, to time.Time) ([]Event, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return nil, err
	}

	// singleEvents expands a repeating event into the occurrences that
	// actually fall in the window, which is what somebody asking what
	// is on today means.
	found, err := svc.Events.List(id).
		TimeMin(from.Format(time.RFC3339)).
		TimeMax(to.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(MaxList).
		Context(ctx).Do()
	if gone(err) {
		d.forget(ctx, userID)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("calendar: reading my calendar: %w", err)
	}

	out := make([]Event, 0, len(found.Items))
	for _, item := range found.Items {
		if e := fromGoogle(item, true, d.location); e != nil {
			out = append(out, *e)
		}
	}
	return out, nil
}

// Busy : When the person is occupied between two times, across every
// calendar they have.
//
// Only the shape: a start and an end. What they are doing is not
// readable with these scopes and is not returned here, because it is
// not known.
func (d *Diary) Busy(ctx context.Context, userID string, from, to time.Time) ([]Event, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}

	answer, err := svc.Freebusy.Query(&gcal.FreeBusyRequest{
		TimeMin: from.Format(time.RFC3339),
		TimeMax: to.Format(time.RFC3339),
		Items:   []*gcal.FreeBusyRequestItem{{Id: "primary"}},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar: asking when you are busy: %w", err)
	}

	var out []Event
	for _, cal := range answer.Calendars {
		for _, slot := range cal.Busy {
			starts, err1 := time.Parse(time.RFC3339, slot.Start)
			ends, err2 := time.Parse(time.RFC3339, slot.End)
			if err1 != nil || err2 != nil {
				continue
			}
			out = append(out, Event{Starts: starts, Ends: ends})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Starts.Before(out[j].Starts) })
	return out, nil
}

// Free : Whether nothing is booked over the whole of a window.
func (d *Diary) Free(ctx context.Context, userID string, from, to time.Time) (bool, []Event, error) {
	busy, err := d.Busy(ctx, userID, from, to)
	if err != nil {
		return false, nil, err
	}
	return len(busy) == 0, busy, nil
}

// toGoogle : An event in the form the API takes.
func toGoogle(e Event, loc *time.Location) *gcal.Event {
	out := &gcal.Event{
		Summary:     strings.TrimSpace(e.Title),
		Location:    strings.TrimSpace(e.Where),
		Description: strings.TrimSpace(e.Notes),
	}
	if e.AllDay {
		// A whole-day event is a date and no time, and its end is the
		// day after: Google treats the end as exclusive.
		out.Start = &gcal.EventDateTime{Date: e.Starts.In(loc).Format(time.DateOnly)}
		out.End = &gcal.EventDateTime{Date: e.Ends.In(loc).AddDate(0, 0, 1).Format(time.DateOnly)}
		return out
	}
	out.Start = &gcal.EventDateTime{
		DateTime: e.Starts.In(loc).Format(time.RFC3339), TimeZone: loc.String(),
	}
	out.End = &gcal.EventDateTime{
		DateTime: e.Ends.In(loc).Format(time.RFC3339), TimeZone: loc.String(),
	}
	return out
}

// fromGoogle : An event as this package sees it, or nil if it carries no
// time this can use.
func fromGoogle(g *gcal.Event, mine bool, loc *time.Location) *Event {
	if g == nil || g.Start == nil || g.End == nil {
		return nil
	}
	out := &Event{
		ID: g.Id, Title: g.Summary, Where: g.Location, Notes: g.Description, Mine: mine,
	}

	if g.Start.Date != "" {
		starts, err := time.ParseInLocation(time.DateOnly, g.Start.Date, loc)
		if err != nil {
			return nil
		}
		ends := starts
		if parsed, err := time.ParseInLocation(time.DateOnly, g.End.Date, loc); err == nil {
			// Exclusive on the way in, so exclusive on the way back.
			ends = parsed.AddDate(0, 0, -1)
		}
		out.Starts, out.Ends, out.AllDay = starts, ends, true
		return out
	}

	starts, err := time.Parse(time.RFC3339, g.Start.DateTime)
	if err != nil {
		return nil
	}
	ends, err := time.Parse(time.RFC3339, g.End.DateTime)
	if err != nil {
		ends = starts
	}
	out.Starts, out.Ends = starts, ends
	return out
}
