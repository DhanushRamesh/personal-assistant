// Package calendar puts things in a diary and says when somebody is busy.
//
// Two scopes, both non-sensitive, and the division between them is the
// whole shape of this package. calendar.app.created gives a calendar of
// its own and complete control of that one; calendar.freebusy says when
// the person is busy across every calendar they have, without saying
// what they are busy with.
//
// So: it can write, but only where it cannot disturb anything, and it
// can see that four o'clock is taken without seeing whose meeting it
// is. That is a smaller thing than reading the real diary, and it is
// bought for nothing -- no app verification, no published privacy
// policy, and no refresh token expiring every seven days.
package calendar

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// Scopes : What this needs granted. Both are non-sensitive.
var Scopes = []string{
	"https://www.googleapis.com/auth/calendar.app.created",
	"https://www.googleapis.com/auth/calendar.freebusy",
}

// Name : What the assistant's own calendar is called in Google Calendar.
//
// Visible on the person's phone beside their real calendars, so it is
// named for what it is rather than after the software.
const Name = "Jarvis"

// Clients : Somewhere to get an HTTP client that acts as a person, and
// somewhere to remember which calendar is ours.
//
// Remembered rather than discovered, because the permission that makes
// this safe is also what stops it looking: calendar.app.created allows
// making a calendar and doing anything to that one, and refuses
// CalendarList.list with a 403. There is no way to ask Google which
// calendar is mine. The only way to know is to have written it down.
type Clients interface {
	Client(ctx context.Context, userID string) (*http.Client, error)
	Calendar(ctx context.Context, userID string) (string, error)
	SetCalendar(ctx context.Context, userID, calendarID string) error
}

// Diary : The assistant's own calendar, and what it can see of the rest.
type Diary struct {
	clients  Clients
	location *time.Location
	now      func() time.Time

	// mu, own : The identifier of the assistant's calendar, once found.
	//
	// Cached because finding it means listing every calendar, and it
	// cannot change: the calendar is made once and kept. Cleared
	// nowhere, because a calendar the person deletes by hand is
	// noticed as a 404 and made again.
	mu  sync.Mutex
	own map[string]string
}

// New : Builds one over a source of authenticated clients.
func New(clients Clients, location *time.Location, now func() time.Time) *Diary {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if location == nil {
		location = time.UTC
	}
	return &Diary{clients: clients, location: location, now: now, own: map[string]string{}}
}

// service : The Calendar API, acting as the person.
func (d *Diary) service(ctx context.Context, userID string) (*gcal.Service, error) {
	client, err := d.clients.Client(ctx, userID)
	if err != nil {
		return nil, err
	}
	return gcal.NewService(ctx, option.WithHTTPClient(client))
}

// mine : The identifier of the assistant's own calendar, making it if
// there is not one yet.
//
// Read from the store rather than found by listing: listing is exactly
// what this permission forbids. A stored identifier can go stale if the
// calendar is deleted by hand, which shows up as a 404 when it is next
// used and is dealt with there by making another.
func (d *Diary) mine(ctx context.Context, userID string, svc *gcal.Service) (string, error) {
	d.mu.Lock()
	cached := d.own[userID]
	d.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	kept, err := d.clients.Calendar(ctx, userID)
	if err != nil {
		return "", err
	}
	if kept != "" {
		d.remember(userID, kept)
		return kept, nil
	}

	made, err := svc.Calendars.Insert(&gcal.Calendar{
		Summary:     Name,
		Description: "Reminders and events set through " + Name + ".",
		TimeZone:    d.location.String(),
	}).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("calendar: making my calendar: %w", err)
	}
	// Written down before it is used. Made and not recorded means a
	// second one next time, and a person with five calendars called
	// Jarvis and no way to tell which holds what.
	if err := d.clients.SetCalendar(ctx, userID, made.Id); err != nil {
		return "", err
	}
	d.remember(userID, made.Id)
	return made.Id, nil
}

// remember : Caches which calendar is ours.
func (d *Diary) remember(userID, id string) {
	d.mu.Lock()
	d.own[userID] = id
	d.mu.Unlock()
}

// forget : Drops what is remembered, so the next call makes a new
// calendar. Used when Google says the old one is not there any more.
func (d *Diary) forget(ctx context.Context, userID string) {
	d.mu.Lock()
	delete(d.own, userID)
	d.mu.Unlock()
	_ = d.clients.SetCalendar(ctx, userID, "")
}

// gone : Whether Google is saying the thing does not exist.
func gone(err error) bool {
	var api *googleapi.Error
	return errors.As(err, &api) && (api.Code == http.StatusNotFound || api.Code == http.StatusGone)
}

// Owned : One calendar the person has, as the assistant sees it.
type Owned struct {
	// ID : Google's identifier for it.
	ID string
	// Name : What it is called.
	Name string
	// Role : What the account may do with it: owner, writer, reader or
	// freeBusyReader.
	Role string
	// Mine : Whether this is the calendar the assistant writes to.
	Mine bool
}

// Calendars : Every calendar the person has, with the assistant's own
// marked.
//
// Reading the list needs calendar.readonly; the permission that grants the
// assistant a calendar of its own forbids it. So this works only while both
// are granted, and says so plainly rather than returning an empty list.
func (d *Diary) Calendars(ctx context.Context, userID string) ([]Owned, error) {
	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	ours, _ := d.clients.Calendar(ctx, userID)

	list, err := svc.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("calendar: listing the calendars: %w", err)
	}

	out := make([]Owned, 0, len(list.Items))
	for _, c := range list.Items {
		out = append(out, Owned{
			ID:   c.Id,
			Name: c.Summary,
			Role: c.AccessRole,
			Mine: ours != "" && c.Id == ours,
		})
	}
	return out, nil
}

// Rename : Changes what the assistant's own calendar is called.
//
// Its own and no other. The permission reaches only calendars this app
// made, so renaming anything else fails at Google; refusing here instead
// gives the model a sentence it can pass on rather than a scope error.
func (d *Diary) Rename(ctx context.Context, userID, name string) (*Owned, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("a calendar needs a name")
	}

	svc, err := d.service(ctx, userID)
	if err != nil {
		return nil, err
	}
	id, err := d.mine(ctx, userID, svc)
	if err != nil {
		return nil, err
	}

	// Patched rather than replaced, so the timezone and description it was
	// made with survive being renamed.
	done, err := svc.Calendars.Patch(id, &gcal.Calendar{Summary: name}).Context(ctx).Do()
	if err != nil {
		if gone(err) {
			d.forget(ctx, userID)
		}
		return nil, fmt.Errorf("calendar: renaming my calendar: %w", err)
	}
	return &Owned{ID: done.Id, Name: done.Summary, Role: "owner", Mine: true}, nil
}
