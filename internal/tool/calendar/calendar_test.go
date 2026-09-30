package calendar_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	calendartool "github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
)

// india : The zone the tools read written times in.
func india() *time.Location { return time.FixedZone("IST", 5*3600+1800) }

// at : A fixed afternoon.
func at() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }

// diary : A stand-in for the calendar.
type diary struct {
	added  []calendar.Event
	mine   []calendar.Event
	busy   []calendar.Event
	held   []calendar.Owned
	theirs []calendar.Event
	unread []string
	refuse error
}

// Calendars : The calendars, with whichever was marked as the assistant's.
func (d *diary) Calendars(_ context.Context, _ string) ([]calendar.Owned, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	return append([]calendar.Owned(nil), d.held...), nil
}

// Theirs : Events on one of the person's own calendars, matched by name.
func (d *diary) Theirs(_ context.Context, _, which string, _, _ time.Time) (calendar.Owned, []calendar.Event, error) {
	if d.refuse != nil {
		return calendar.Owned{}, nil, d.refuse
	}
	// Matched the way the real diary matches, so these tests exercise the
	// matching rather than a stricter copy of it.
	c, err := calendar.Pick(d.held, which)
	if err != nil {
		return calendar.Owned{}, nil, err
	}
	return *c, append([]calendar.Event(nil), d.theirs...), nil
}

// Rename : Renames the one marked as the assistant's, in place.
func (d *diary) Rename(_ context.Context, _, name string) (*calendar.Owned, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	for i := range d.held {
		if d.held[i].Mine {
			d.held[i].Name = name
			out := d.held[i]
			return &out, nil
		}
	}
	return nil, errors.New("no calendar of mine")
}

func (d *diary) Add(_ context.Context, _ string, e calendar.Event) (*calendar.Event, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	e.ID = "evt_1"
	d.added = append(d.added, e)
	return &e, nil
}

// Cancel : Actually removes it, so the read-back a tool does afterwards
// tests something. A double that accepts a deletion and keeps the row
// makes every verification look like a failure.
func (d *diary) Cancel(_ context.Context, _, id string) error {
	if d.refuse != nil {
		return d.refuse
	}
	kept := d.mine[:0]
	for _, e := range d.mine {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	d.mine = kept
	return nil
}

func (d *diary) One(_ context.Context, _, id string) (*calendar.Event, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	for i := range d.mine {
		if d.mine[i].ID == id {
			return &d.mine[i], nil
		}
	}
	return nil, nil
}

func (d *diary) Update(_ context.Context, _, id string, a calendar.Amend) (*calendar.Event, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	for i := range d.mine {
		if d.mine[i].ID != id {
			continue
		}
		if a.Title != nil {
			d.mine[i].Title = *a.Title
		}
		if a.Where != nil {
			d.mine[i].Where = *a.Where
		}
		if a.Notes != nil {
			d.mine[i].Notes = *a.Notes
		}
		if a.Starts != nil {
			d.mine[i].Starts = *a.Starts
		}
		if a.Ends != nil {
			d.mine[i].Ends = *a.Ends
		}
		return &d.mine[i], nil
	}
	return nil, nil
}

// Everywhere : Everything, the way the real one merges it -- the
// assistant's own and each of theirs, with the calendar's name on each
// event so a test can tell which came from where.
func (d *diary) Everywhere(_ context.Context, _ string, _, _ time.Time) ([]calendar.Event, []string, error) {
	if d.refuse != nil {
		return nil, nil, d.refuse
	}
	var all []calendar.Event
	for _, e := range d.mine {
		e.Mine = true
		if e.Calendar == "" {
			e.Calendar = "Jarvis"
		}
		all = append(all, e)
	}
	for _, e := range d.theirs {
		if e.Calendar == "" {
			e.Calendar = "Personal"
		}
		all = append(all, e)
	}
	return all, d.unread, nil
}

func (d *diary) Mine(_ context.Context, _ string, _, _ time.Time) ([]calendar.Event, error) {
	return d.mine, d.refuse
}

func (d *diary) Busy(_ context.Context, _ string, _, _ time.Time) ([]calendar.Event, error) {
	return d.busy, d.refuse
}

// run : Calls one tool by name with the given arguments.
func run(t *testing.T, d calendartool.Diary, name, args string) tool.Result {
	t.Helper()
	for _, candidate := range calendartool.All(d, calendartool.Clock{Now: at, Location: india()}) {
		if candidate.Name != name {
			continue
		}
		// Validated the way the registry validates, against the schema
		// the model is actually shown. Calling Run directly skipped this,
		// so no test here had ever exercised a schema: a placeholder
		// identifier sailed through and the tool ran on it.
		// The saying is supplied when a test has not written one. These
		// tests are about each tool's own arguments; that one belongs to
		// every tool and has a test of its own.
		args = withSaying(args)
		if err := tool.Validate(tool.Narrated(candidate.Params, "sir"), json.RawMessage(args)); err != nil {
			return tool.Failed(candidate.Name + " was not called correctly: " + err.Error())
		}
		clean, _ := json.Marshal(stripSaying(args))
		return candidate.Run(context.Background(), tool.Invocation{
			Args:   clean,
			Caller: tool.Caller{UserID: "usr_1"},
		})
	}
	t.Fatalf("no tool called %s", name)
	return tool.Result{}
}

// A written time is read in the person's own zone, with no offset in it.
//
// A model asked for an offset writes whatever it last saw, and an hour
// wrong in a diary is worse than a refusal.
func TestATimeIsReadInThePersonsZone(t *testing.T) {
	d := &diary{}
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"2026-09-28T15:00","saying":"doing that"}`)

	if len(d.added) != 1 {
		t.Fatalf("result = %+v, want one event added", got)
	}
	// Three in the afternoon in India is half past nine in the morning UTC.
	want := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)
	if !d.added[0].Starts.Equal(want) {
		t.Errorf("starts = %v, want %v", d.added[0].Starts.UTC(), want)
	}
	// An hour by default, which is the ordinary length of an appointment.
	if d.added[0].Ends.Sub(d.added[0].Starts) != time.Hour {
		t.Errorf("lasts %v, want an hour", d.added[0].Ends.Sub(d.added[0].Starts))
	}
}

// A time it cannot read is refused rather than guessed at.
func TestAnUnreadableTimeIsRefused(t *testing.T) {
	d := &diary{}
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"next tuesday","saying":"doing that"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
	if len(d.added) != 0 {
		t.Error("something was added anyway")
	}
}

// An empty day says every calendar was looked at, because now every
// calendar has been. The warning this replaces -- that the assistant
// can only see its own -- was true when reading one and is a lie when
// reading all of them.
func TestAnEmptyDayNamesWhatWasLookedAt(t *testing.T) {
	got := run(t, &diary{}, "calendar_events", `{"days":1,"saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if !strings.Contains(got.Content, "any of their calendars") {
		t.Errorf("content = %q, want it to say all of them were read", got.Content)
	}
}

// TestEveryCalendarIsReadWithoutBeingAsked : The default is all of
// them, in code, not a line of prompt hoping the model passes an
// argument.
//
// Asked whether anything was on 2 October the assistant answered that
// nothing was written and offered to check their own calendar -- while
// the holiday calendar had Gandhi Jayanti on it. The offer is the tell:
// it knew there was somewhere else to look and did not look.
func TestEveryCalendarIsReadWithoutBeingAsked(t *testing.T) {
	d := &diary{
		mine: []calendar.Event{{
			ID: "evt_1", Title: "Standup",
			Starts: time.Date(2026, 10, 2, 10, 0, 0, 0, india()),
			Ends:   time.Date(2026, 10, 2, 10, 15, 0, 0, india()),
		}},
		theirs: []calendar.Event{{
			ID: "hol_1", Title: "Gandhi Jayanti", Calendar: "Holidays in India",
			Starts: time.Date(2026, 10, 2, 0, 0, 0, 0, india()),
			Ends:   time.Date(2026, 10, 2, 0, 0, 0, 0, india()),
			AllDay: true,
		}},
	}

	got := run(t, d, "calendar_events", `{"from":"2026-10-02","to":"2026-10-02","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "Gandhi Jayanti") {
		t.Errorf("the holiday was not read: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Holidays in India") {
		t.Errorf("which calendar it came from was not said: %s", got.Content)
	}
	// The one it may change carries an identifier; the holiday does not.
	if !strings.Contains(got.Content, "[id evt_1]") {
		t.Errorf("its own event lost its identifier: %s", got.Content)
	}
	if strings.Contains(got.Content, "[id hol_1]") {
		t.Errorf("a calendar it cannot change was given an identifier: %s", got.Content)
	}
}

// TestACalendarThatWouldNotOpenIsNamed : A day is not called empty on
// the strength of a calendar that failed to load.
func TestACalendarThatWouldNotOpenIsNamed(t *testing.T) {
	d := &diary{unread: []string{"Holidays in India"}}

	got := run(t, d, "calendar_events", `{"days":1,"saying":"doing that"}`)

	if !strings.Contains(got.Content, "Holidays in India") {
		t.Errorf("the calendar that failed was not named: %s", got.Content)
	}
	if !strings.Contains(got.Content, "not the whole picture") {
		t.Errorf("content = %q, want it to say the answer is incomplete", got.Content)
	}
}

// Being busy is reported as time taken, never as what is happening: the
// permission granted does not include that and the model must not fill
// the gap.
func TestBusyDoesNotInventWhatTheyAreDoing(t *testing.T) {
	d := &diary{busy: []calendar.Event{{
		Starts: time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC),
		Ends:   time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC),
	}}}

	got := run(t, d, "calendar_free", `{"from":"2026-09-28T15:00","saying":"doing that"}`)

	if !strings.Contains(got.Content, "Busy") {
		t.Errorf("content = %q, want it to say busy", got.Content)
	}
	if !strings.Contains(got.Content, "not visible") {
		t.Errorf("content = %q, want it to say what is not known", got.Content)
	}
}

// A free window says so plainly.
func TestFreeSaysFree(t *testing.T) {
	got := run(t, &diary{}, "calendar_free", `{"from":"2026-09-28T15:00","minutes":30,"saying":"doing that"}`)

	if !strings.HasPrefix(got.Content, "Free") {
		t.Errorf("content = %q, want it to start by saying free", got.Content)
	}
	if !strings.Contains(got.Content, "30 minutes") {
		t.Errorf("content = %q, want the window in it", got.Content)
	}
}

// A lapsed connection is reported as something only the person can fix,
// and explicitly not as an empty calendar.
func TestALapsedConnectionIsNotAnEmptyCalendar(t *testing.T) {
	d := &diary{refuse: google.ErrNeedsReconnect}
	got := run(t, d, "calendar_events", `{"saying":"doing that"}`)

	if got.Outcome != "failed" {
		t.Fatalf("outcome = %q, want failed: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "settings") {
		t.Errorf("content = %q, want it to say where to fix it", got.Content)
	}
	if !strings.Contains(got.Content, "empty") {
		t.Errorf("content = %q, want it to warn against saying the calendar is empty", got.Content)
	}
}

// No account connected is its own answer, not a failure of the calendar.
func TestNoAccountIsSaidPlainly(t *testing.T) {
	d := &diary{refuse: google.ErrNotConnected}
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"2026-09-28T15:00","saying":"doing that"}`)

	if !strings.Contains(got.Content, "connect") {
		t.Errorf("content = %q, want it to say to connect an account", got.Content)
	}
}

// An all-day event carries no time and is not given one.
func TestAnAllDayEventHasNoTime(t *testing.T) {
	d := &diary{}
	run(t, d, "calendar_add", `{"title":"Birthday","starts":"2026-10-02","all_day":true,"saying":"doing that"}`)

	if len(d.added) != 1 {
		t.Fatal("nothing was added")
	}
	if !d.added[0].AllDay {
		t.Error("it was not marked as all day")
	}
}

// Without a server-side calendar the tools say so rather than panicking
// on a nil.
func TestNoCalendarAtAll(t *testing.T) {
	got := run(t, nil, "calendar_events", `{"saying":"doing that"}`)
	if got.Outcome != "failed" || !strings.Contains(got.Content, "no calendar") {
		t.Errorf("result = %+v", got)
	}
}

// asked : The window a listing reports having looked at.
func asked(t *testing.T, d *diary, args string) string {
	t.Helper()
	return run(t, d, "calendar_events", args).Content
}

// A named date means that whole day, not the instant it begins.
func TestADateMeansTheWholeDay(t *testing.T) {
	d := &diary{}
	got := asked(t, d, `{"from":"2026-09-03"}`)

	if !strings.Contains(got, "Thursday 3 September") {
		t.Errorf("content = %q, want it to name the day it looked at", got)
	}
}

// A day already past is a fair question, and the answer says which day
// rather than talking about days ahead.
func TestAPastDayCanBeAskedAbout(t *testing.T) {
	d := &diary{mine: []calendar.Event{{
		ID: "evt001", Title: "Something", Mine: true,
		Starts: time.Date(2026, 9, 3, 9, 30, 0, 0, time.UTC),
		Ends:   time.Date(2026, 9, 3, 10, 30, 0, 0, time.UTC),
	}}}
	got := asked(t, d, `{"from":"2026-09-03"}`)

	if strings.Contains(got, "next") {
		t.Errorf("content = %q, want it to name the date rather than days ahead", got)
	}
	if !strings.Contains(got, "Something") {
		t.Errorf("content = %q, want the event", got)
	}
}

// A range covers both ends inclusively and says so.
func TestARangeCoversBothEnds(t *testing.T) {
	d := &diary{}
	got := asked(t, d, `{"from":"2026-09-03","to":"2026-09-05"}`)

	if !strings.Contains(got, "3 September") || !strings.Contains(got, "5 September") {
		t.Errorf("content = %q, want both ends named", got)
	}
}

// Without dates it behaves as it did: a count of days ahead.
func TestWithoutDatesItCountsDaysAhead(t *testing.T) {
	got := asked(t, &diary{}, `{"days":3}`)
	if !strings.Contains(got, "next 3 days") {
		t.Errorf("content = %q", got)
	}
	got = asked(t, &diary{}, `{}`)
	if !strings.Contains(got, "next 7 days") {
		t.Errorf("content = %q, want seven days by default", got)
	}
}

// A range that ends before it starts is refused rather than silently
// returning nothing, which would read as "your day was empty".
func TestABackwardsRangeIsRefused(t *testing.T) {
	got := run(t, &diary{}, "calendar_events", `{"from":"2026-09-05","to":"2026-09-03","saying":"doing that"}`)
	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
}

// An empty answer says every calendar was read, whichever window was
// asked for.
func TestAnEmptyRangeStillSaysWhatWasRead(t *testing.T) {
	got := asked(t, &diary{}, `{"from":"2026-09-03"}`)
	if !strings.Contains(got, "any of their calendars") {
		t.Errorf("content = %q, want it to say all of them were read", got)
	}
}

// an : One event already in the diary, for the change tests.
func an(id, title string) []calendar.Event {
	return []calendar.Event{{
		ID: id, Title: title, Mine: true,
		Starts: time.Date(2026, 10, 29, 6, 30, 0, 0, time.UTC),
		Ends:   time.Date(2026, 10, 29, 7, 30, 0, 0, time.UTC),
	}}
}

// Renaming reports both ends, so the person hears what it was called.
func TestRenamingSaysWhatItWas(t *testing.T) {
	d := &diary{mine: an("evt001", "Meeting for haircut")}
	got := run(t, d, "calendar_update", `{"id":"evt001","title":"My Favorite Date","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"Meeting for haircut", "My Favorite Date"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}

// Moving the start keeps the length it had, which is what "move it to
// four" means.
func TestMovingKeepsTheLength(t *testing.T) {
	d := &diary{mine: an("evt001", "Haircut")}
	run(t, d, "calendar_update", `{"id":"evt001","starts":"2026-10-29T16:00","saying":"doing that"}`)

	if got := d.mine[0].Ends.Sub(d.mine[0].Starts); got != time.Hour {
		t.Errorf("length = %v, want the hour it had", got)
	}
}

// An identifier that is not there is refused before anything is
// written, rather than creating something new.
func TestAnUnknownEventIsRefused(t *testing.T) {
	d := &diary{mine: an("evt001", "Haircut")}
	got := run(t, d, "calendar_update", `{"id":"nope","title":"Other","saying":"doing that"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused", got.Outcome)
	}
	if d.mine[0].Title != "Haircut" {
		t.Errorf("the existing event was changed to %q", d.mine[0].Title)
	}
}

// Nothing to change is said rather than reported as a change.
func TestNothingToChangeIsRefused(t *testing.T) {
	d := &diary{mine: an("evt001", "Haircut")}
	if got := run(t, d, "calendar_update", `{"id":"evt001","saying":"doing that"}`); got.Outcome != "failed" {
		t.Errorf("outcome = %q: %s", got.Outcome, got.Content)
	}
}

// TestCalendarsListsThemWithTheTotal : The listing says how many there are
// and which one may be written to.
//
// The count is stated because a listing that only prints rows invites the
// number of rows to be reported as the number of things, which is how
// sixty-two conversations were once answered as ten.
func TestCalendarsListsThemWithTheTotal(t *testing.T) {
	d := &diary{held: []calendar.Owned{
		{ID: "a", Name: "Jarvis", Role: "owner", Mine: true},
		{ID: "b", Name: "rjdhanush22@gmail.com", Role: "owner"},
		{ID: "c", Name: "Holidays in India", Role: "reader"},
	}}
	got := run(t, d, "calendar_calendars", `{"saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s, want ok: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"3 calendars in total", "Jarvis", "this is the one you write to",
		"Holidays in India", "read only"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("listing does not mention %q:\n%s", want, got.Content)
		}
	}
}

// TestRenameSaysWhatItWas : Renaming reports the change from one name to
// the other, read back rather than assumed.
func TestRenameSaysWhatItWas(t *testing.T) {
	d := &diary{held: []calendar.Owned{{ID: "a", Name: "Jarvis", Role: "owner", Mine: true}}}
	got := run(t, d, "calendar_rename", `{"name":"Personal Assistant","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s, want ok: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "Jarvis") || !strings.Contains(got.Content, "Personal Assistant") {
		t.Errorf("does not say what changed from and to:\n%s", got.Content)
	}
	if d.held[0].Name != "Personal Assistant" {
		t.Errorf("calendar name = %q, want it renamed", d.held[0].Name)
	}
}

// TestRenameNeedsAName : An empty name is refused rather than applied.
func TestRenameNeedsAName(t *testing.T) {
	d := &diary{held: []calendar.Owned{{ID: "a", Name: "Jarvis", Mine: true}}}
	got := run(t, d, "calendar_rename", `{"name":"   ","saying":"doing that"}`)

	if got.Outcome == "ok" {
		t.Errorf("an empty name was accepted: %s", got.Content)
	}
	if d.held[0].Name != "Jarvis" {
		t.Errorf("the calendar was renamed anyway, to %q", d.held[0].Name)
	}
}

// TestEventsReadsTheirCalendar : Naming a calendar reads that one, and
// says it cannot be changed.
func TestEventsReadsTheirCalendar(t *testing.T) {
	when := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	d := &diary{
		held:   []calendar.Owned{{ID: "b", Name: "rjdhanush22@gmail.com", Role: "owner"}},
		theirs: []calendar.Event{{ID: "x1", Title: "Dentist", Starts: when, Ends: when.Add(time.Hour)}},
	}
	got := run(t, d, "calendar_events", `{"days":7,"calendar":"rjdhanush22@gmail.com","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"1 entries", "rjdhanush22@gmail.com", "Dentist", "cannot change it"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q:\n%s", want, got.Content)
		}
	}
	// Their events are read only, so an identifier the model could pass to
	// a change tool must not be offered.
	if strings.Contains(got.Content, "[id ") {
		t.Errorf("offered an identifier for a calendar it cannot change:\n%s", got.Content)
	}
}

// TestEventsUnknownCalendarAsksWhich : A name that matches nothing brings
// back the names that do exist, so the model can ask which was meant.
//
// It is never answered as an empty day, and never as "there is no such
// calendar": the name arrived through speech and a mangled one looks
// exactly like a wrong one.
func TestEventsUnknownCalendarAsksWhich(t *testing.T) {
	d := &diary{held: []calendar.Owned{
		{ID: "a", Name: "Jarvis", Mine: true},
		{ID: "b", Name: "rjdhanush22@gmail.com"},
	}}
	got := run(t, d, "calendar_events", `{"days":7,"calendar":"birthdays","saying":"doing that"}`)

	if got.Outcome == "ok" {
		t.Errorf("an unknown calendar was answered rather than queried: %s", got.Content)
	}
	for _, want := range []string{"Jarvis", "rjdhanush22@gmail.com", "Ask which"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q:\n%s", want, got.Content)
		}
	}
}

// TestEventsMatchesAMangledName : A name mauled by speech-to-text still
// finds the calendar it meant.
func TestEventsMatchesAMangledName(t *testing.T) {
	when := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	d := &diary{
		held:   []calendar.Owned{{ID: "b", Name: "rjdhanush22@gmail.com"}},
		theirs: []calendar.Event{{ID: "x1", Title: "Dentist", Starts: when, Ends: when.Add(time.Hour)}},
	}
	got := run(t, d, "calendar_events", `{"days":7,"calendar":"rjdanesh22rjmail.com","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("a misheard address was not matched: %s", got.Content)
	}
	if !strings.Contains(got.Content, "rjdhanush22@gmail.com") {
		t.Errorf("does not name the calendar it actually read:\n%s", got.Content)
	}
}

// TestEventsWithoutCalendarStillReadsMine : Leaving it out is unchanged.
func TestEventsWithoutCalendarStillReadsMine(t *testing.T) {
	when := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	d := &diary{mine: []calendar.Event{{ID: "evt_9", Title: "Standup", Starts: when, Ends: when.Add(time.Hour)}}}
	got := run(t, d, "calendar_events", `{"days":7,"saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "Standup") || !strings.Contains(got.Content, "[id evt_9]") {
		t.Errorf("own calendar should still come back with identifiers:\n%s", got.Content)
	}
}

// TestNamingYourOwnCalendarIsNotReadOnly : Asking for the assistant's own
// calendar by name reaches the same place as not naming one.
//
// It did not. Reading "Personal Assistant" by name went down the path
// meant for the person's calendars, which withholds identifiers and says
// nothing can be changed. The assistant, unable to mend the four one-day
// entries it had made, added more.
func TestNamingYourOwnCalendarIsNotReadOnly(t *testing.T) {
	when := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	d := &diary{
		held: []calendar.Owned{
			{ID: "a", Name: "Personal Assistant", Role: "owner", Mine: true},
			{ID: "b", Name: "rjdhanush22@gmail.com", Role: "owner"},
		},
		theirs: []calendar.Event{{ID: "evt_stay", Title: "Stay at Atlantic Inn",
			Starts: when, Ends: when.AddDate(0, 0, 3), AllDay: true, Mine: true}},
	}
	got := run(t, d, "calendar_events", `{"days":30,"calendar":"Personal Assistant","saying":"doing that"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "[id evt_stay]") {
		t.Errorf("own calendar named aloud gave no identifier, so nothing could be changed:\n%s", got.Content)
	}
	if strings.Contains(got.Content, "cannot") {
		t.Errorf("own calendar named aloud was called unchangeable:\n%s", got.Content)
	}
}

// TestCancelTakesSeveralAtOnce : Four events removed in one call are
// counted once, not four times.
//
// Four separate calls owed four overlapping pairs of numbers and the
// person heard "there were 8 before and 7 now, there were 7 before and 6
// now…". One call, one read-back, one count.
func TestCancelTakesSeveralAtOnce(t *testing.T) {
	when := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	d := &diary{}
	for _, id := range []string{"aaaaa1", "bbbbb2", "ccccc3", "ddddd4", "keeper5"} {
		d.mine = append(d.mine, calendar.Event{ID: id, Title: "Stay", Starts: when,
			Ends: when.AddDate(0, 0, 1), AllDay: true, Mine: true})
	}

	got := run(t, d, "calendar_cancel",
		`{"ids":["aaaaa1","bbbbb2","ccccc3","ddddd4"],"saying":"removing the duplicates"}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	if strings.Count(got.Content, "before") != 1 {
		t.Errorf("expected one count sentence, got:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "5") || !strings.Contains(got.Content, "1 event") {
		t.Errorf("does not say 5 before and 1 now:\n%s", got.Content)
	}
}

// TestCancelRefusesAnEmptyList : A call that would remove nothing is a
// mistake, not a no-op.
func TestCancelRefusesAnEmptyList(t *testing.T) {
	if got := run(t, &diary{}, "calendar_cancel", `{"ids":[],"saying":"removing"}`); got.Outcome == "ok" {
		t.Errorf("an empty list was accepted: %s", got.Content)
	}
}

// TestCancelStillRefusesAPlaceholder : The pattern applies to every
// element, so an invented identifier is caught inside a list too.
func TestCancelStillRefusesAPlaceholder(t *testing.T) {
	got := run(t, &diary{}, "calendar_cancel",
		`{"ids":["aaaaa1","<id_for_the_second_event>"],"saying":"removing"}`)
	if got.Outcome == "ok" {
		t.Errorf("a placeholder inside a list was accepted: %s", got.Content)
	}
	if !strings.Contains(got.Content, "ids[1]") {
		t.Errorf("does not say which element was wrong:\n%s", got.Content)
	}
}

// stripSaying : The arguments a tool actually receives, with the saying
// taken off the way the registry takes it off.
func stripSaying(args string) map[string]any {
	var given map[string]any
	if err := json.Unmarshal([]byte(args), &given); err != nil {
		return map[string]any{}
	}
	delete(given, "saying")
	return given
}

// withSaying : The arguments with a saying added if one is missing.
func withSaying(args string) string {
	var given map[string]any
	if err := json.Unmarshal([]byte(args), &given); err != nil {
		return args
	}
	if _, there := given["saying"]; there {
		return args
	}
	given["saying"] = "doing that"
	out, err := json.Marshal(given)
	if err != nil {
		return args
	}
	return string(out)
}

// TestABirthdayIsNotAnnouncedAsAllDay : "Meganadham's birthday today,
// all day" tells somebody that a birthday lasts a day.
//
// Told apart by Google's own event type rather than by the word in the
// title, which would only work in one language.
func TestABirthdayIsNotAnnouncedAsAllDay(t *testing.T) {
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, india())
	d := &diary{theirs: []calendar.Event{{
		ID: "b1", Title: "Meganadham's birthday", Calendar: "Birthdays",
		Starts: day, Ends: day, AllDay: true, Kind: "birthday",
	}}}

	got := run(t, d, "calendar_events", `{"from":"2026-09-30","to":"2026-09-30","saying":"looking"}`)

	if strings.Contains(got.Content, "all day") {
		t.Errorf("a birthday was called all day: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Meganadham's birthday") {
		t.Errorf("the birthday went missing: %s", got.Content)
	}
}

// TestAnOrdinaryWholeDayEventStillSaysAllDay : The word earns its place
// where the event is not inherently a day, such as a holiday or a day
// off.
func TestAnOrdinaryWholeDayEventStillSaysAllDay(t *testing.T) {
	day := time.Date(2026, 10, 2, 0, 0, 0, 0, india())
	d := &diary{theirs: []calendar.Event{{
		ID: "h1", Title: "Gandhi Jayanti", Calendar: "Holidays in India",
		Starts: day, Ends: day, AllDay: true,
	}}}

	got := run(t, d, "calendar_events", `{"from":"2026-10-02","to":"2026-10-02","saying":"looking"}`)

	if !strings.Contains(got.Content, "all day") {
		t.Errorf("a holiday lost its all day: %s", got.Content)
	}
}

// TestAStayOverSeveralDaysSaysBothEnds : It used to say the first day
// only, so a stay from the 26th to the 29th read as the 26th.
func TestAStayOverSeveralDaysSaysBothEnds(t *testing.T) {
	d := &diary{theirs: []calendar.Event{{
		ID: "t1", Title: "Chennai", Calendar: "Personal",
		Starts: time.Date(2026, 10, 26, 0, 0, 0, 0, india()),
		Ends:   time.Date(2026, 10, 29, 0, 0, 0, 0, india()),
		AllDay: true,
	}}}

	got := run(t, d, "calendar_events", `{"from":"2026-10-26","to":"2026-10-29","saying":"looking"}`)

	for _, want := range []string{"Monday 26 October", "Thursday 29 October"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("content = %q, want %q in it", got.Content, want)
		}
	}
}
