package calendar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/heard"

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
	// Calendar : The name of the calendar it sits on.
	//
	// Set when several are read together, where the name is the only
	// thing telling a holiday apart from something the person wrote
	// down. Empty when only one calendar was read and the caller
	// already knows which.
	Calendar string
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
	// AllDay : Whether the event takes whole days rather than a slot.
	//
	// Needed because Google stores the two differently -- a date for an
	// all-day event, a timestamp for a timed one -- and patching one
	// with the other is a request to change its kind, which it refuses.
	// Without this a birthday could not be moved at all: every attempt
	// sent a timestamp, Google declined, and the failure was reported
	// as "Google will not accept a date in the past", which was never
	// true.
	//
	// It says what the event already is rather than asking for a
	// change, so it does not count towards Empty.
	AllDay *bool
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

	patch := patchFor(a, d.location)

	changed, err := svc.Events.Patch(id, eventID, patch).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			return nil, fmt.Errorf("calendar: there is no such event to change")
		}
		return nil, fmt.Errorf("calendar: changing that event: %w", err)
	}
	return fromGoogle(changed, true, d.location), nil
}

// patchFor : The change in the form the API takes.
//
// Separate from Update because it is the part worth testing: it decides
// between dates and timestamps, and getting that wrong is a request
// Google refuses outright rather than a value it stores badly.
func patchFor(a Amend, loc *time.Location) *gcal.Event {
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
	// An all-day event is dates; a timed one is timestamps. Sending the
	// wrong shape asks Google to change the kind of event it is, which
	// it will not do in a patch.
	allDay := a.AllDay != nil && *a.AllDay
	if a.Starts != nil {
		if allDay {
			patch.Start = &gcal.EventDateTime{
				Date: a.Starts.In(loc).Format(time.DateOnly),
			}
		} else {
			patch.Start = &gcal.EventDateTime{
				DateTime: a.Starts.In(loc).Format(time.RFC3339),
				TimeZone: loc.String(),
			}
		}
	}
	if a.Ends != nil {
		if allDay {
			// Exclusive, as Google counts it: a one-day event on the
			// tenth ends on the eleventh.
			patch.End = &gcal.EventDateTime{
				Date: a.Ends.In(loc).AddDate(0, 0, 1).Format(time.DateOnly),
			}
		} else {
			patch.End = &gcal.EventDateTime{
				DateTime: a.Ends.In(loc).Format(time.RFC3339),
				TimeZone: loc.String(),
			}
		}
	}
	if len(clear) > 0 {
		patch.NullFields = clear
	}

	return patch
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

// Theirs : What is on one of the person's own calendars, between two
// times.
//
// Read only, whichever calendar it is. Writing is reachable only on the
// one the assistant made, and that is enforced by the permission rather
// than by this: calendar.readonly can read every calendar and change
// none.
//
// The calendar is named the way a person would name it, so a spoken
// "my main calendar" or "holidays" finds one without an identifier
// having to survive being said out loud. An exact identifier works too.
func (d *Diary) Theirs(ctx context.Context, userID, which string, from, to time.Time) (Owned, []Event, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return Owned{}, nil, err
	}

	held, err := d.Calendars(ctx, userID)
	if err != nil {
		return Owned{}, nil, err
	}
	found, err := pickCalendar(held, which)
	if err != nil {
		return Owned{}, nil, fmt.Errorf("%w: %s", err, which)
	}

	out, err := d.on(ctx, svc, *found, from, to)
	if err != nil {
		return *found, nil, err
	}
	return *found, out, nil
}

// on : The events on one calendar, between two times.
//
// Mine decides whether the identifiers are worth handing over and
// whether anything may be changed. Naming the assistant's own calendar
// out loud must reach the same place as not naming one.
func (d *Diary) on(ctx context.Context, svc *gcal.Service, which Owned, from, to time.Time) ([]Event, error) {
	list, err := svc.Events.List(which.ID).
		TimeMin(from.Format(time.RFC3339)).
		TimeMax(to.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(MaxList).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar: reading %s: %w", which.Name, err)
	}

	out := make([]Event, 0, len(list.Items))
	for _, item := range list.Items {
		if e := fromGoogle(item, which.Mine, d.location); e != nil {
			e.Calendar = which.Name
			out = append(out, *e)
		}
	}
	return out, nil
}

// Everywhere : What is on every calendar the person has, between two
// times, soonest first.
//
// The default for any question about a date, because the person does not
// keep one calendar and does not think of themselves as keeping several.
// Reading only the assistant's own answered "is there anything on the
// 2nd of October" with nothing written here, when the 2nd is Gandhi
// Jayanti and the holiday calendar says so. An answer that is silent
// about what it did not look at is worse than no answer.
//
// Read at the same time rather than one after another. One calendar
// takes several seconds, and a person with five of them would wait half
// a minute to be told what is on Friday.
//
// A calendar that will not be read is reported rather than dropped: the
// caller has to be able to say which ones it saw, or "nothing on that
// day" means nothing.
func (d *Diary) Everywhere(ctx context.Context, userID string, from, to time.Time) ([]Event, []string, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	held, err := d.Calendars(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(held) == 0 {
		return nil, nil, nil
	}

	var (
		mu     sync.Mutex
		all    []Event
		missed []string
		wg     sync.WaitGroup
	)
	for _, c := range held {
		wg.Add(1)
		go func(c Owned) {
			defer wg.Done()
			got, err := d.on(ctx, svc, c, from, to)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				missed = append(missed, c.Name)
				return
			}
			all = append(all, got...)
		}(c)
	}
	wg.Wait()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Starts.Equal(all[j].Starts) {
			return all[i].Title < all[j].Title
		}
		return all[i].Starts.Before(all[j].Starts)
	})
	sort.Strings(missed)
	return all, missed, nil
}

// ErrNoSuchCalendar : No calendar of the person's answers to that name.
var ErrNoSuchCalendar = errors.New("calendar: no calendar by that name")

// ErrWhichCalendar : Several calendars are near enough to what was said
// that picking one would be a guess.
var ErrWhichCalendar = errors.New("calendar: more than one calendar could be meant")

// Pick : The calendar a spoken name refers to. Exported so a test double
// standing in for a diary matches names the way this one does.
func Pick(held []Owned, which string) (*Owned, error) { return pickCalendar(held, which) }

// pickCalendar : The calendar a spoken name refers to.
//
// The words people use for their own calendar are resolved here, before
// the general matching: Google names it after their address, which nobody
// says out loud.
func pickCalendar(held []Owned, which string) (*Owned, error) {
	if strings.TrimSpace(which) == "" {
		return nil, ErrNoSuchCalendar
	}
	for i := range held {
		if held[i].ID == which {
			return &held[i], nil
		}
	}

	switch strings.ToLower(strings.TrimSpace(which)) {
	case "main", "primary", "mine", "my calendar", "my main calendar", "personal":
		for i := range held {
			if !held[i].Mine && strings.Contains(held[i].Name, "@") {
				return &held[i], nil
			}
		}
	}

	names := make([]string, len(held))
	for i := range held {
		names[i] = held[i].Name
	}
	at, err := heard.Best(names, which)
	switch {
	case errors.Is(err, heard.ErrSeveral):
		return nil, ErrWhichCalendar
	case err != nil:
		return nil, ErrNoSuchCalendar
	}
	return &held[at], nil
}
