package calendar_test

import (
	"context"
	"encoding/json"
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
	refuse error
}

func (d *diary) Add(_ context.Context, _ string, e calendar.Event) (*calendar.Event, error) {
	if d.refuse != nil {
		return nil, d.refuse
	}
	e.ID = "evt_1"
	d.added = append(d.added, e)
	return &e, nil
}

func (d *diary) Cancel(_ context.Context, _, _ string) error { return d.refuse }

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
		return candidate.Run(context.Background(), tool.Invocation{
			Args:   json.RawMessage(args),
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
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"2026-09-28T15:00"}`)

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
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"next tuesday"}`)

	if got.Outcome != "failed" {
		t.Errorf("outcome = %q, want it refused: %s", got.Outcome, got.Content)
	}
	if len(d.added) != 0 {
		t.Error("something was added anyway")
	}
}

// An empty diary is never reported as an empty day. The assistant can
// only see its own calendar, and saying otherwise would be a claim
// about the person's real one.
func TestAnEmptyDiaryIsNotAnEmptyDay(t *testing.T) {
	got := run(t, &diary{}, "calendar_list", `{"days":1}`)

	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if !strings.Contains(got.Content, "their own") {
		t.Errorf("content = %q, want it to say this is not their real calendar", got.Content)
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

	got := run(t, d, "calendar_free", `{"from":"2026-09-28T15:00"}`)

	if !strings.Contains(got.Content, "Busy") {
		t.Errorf("content = %q, want it to say busy", got.Content)
	}
	if !strings.Contains(got.Content, "not visible") {
		t.Errorf("content = %q, want it to say what is not known", got.Content)
	}
}

// A free window says so plainly.
func TestFreeSaysFree(t *testing.T) {
	got := run(t, &diary{}, "calendar_free", `{"from":"2026-09-28T15:00","minutes":30}`)

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
	got := run(t, d, "calendar_list", `{}`)

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
	got := run(t, d, "calendar_add", `{"title":"Dentist","starts":"2026-09-28T15:00"}`)

	if !strings.Contains(got.Content, "connect") {
		t.Errorf("content = %q, want it to say to connect an account", got.Content)
	}
}

// An all-day event carries no time and is not given one.
func TestAnAllDayEventHasNoTime(t *testing.T) {
	d := &diary{}
	run(t, d, "calendar_add", `{"title":"Birthday","starts":"2026-10-02","all_day":true}`)

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
	got := run(t, nil, "calendar_list", `{}`)
	if got.Outcome != "failed" || !strings.Contains(got.Content, "no calendar") {
		t.Errorf("result = %+v", got)
	}
}
