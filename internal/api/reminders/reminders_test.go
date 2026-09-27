package reminders_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// env : A server with a reminder store behind it.
func env(t *testing.T) (*apitest.Env, *inmemory.Store) {
	t.Helper()
	store := inmemory.New()
	return apitest.NewWith(t, apitest.Options{Reminders: store}), store
}

// put : Stores a reminder for the logged-in person.
func put(t *testing.T, e *apitest.Env, s *inmemory.Store, title string, due time.Time) *remind.Reminder {
	t.Helper()
	r, err := remind.New(e.User.ID, "", remind.ScopeUser, title, "time to "+title, due, remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(t.Context(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// listed : The listing, decoded.
func listed(t *testing.T, e *apitest.Env, path string) reminders.ListResponse {
	t.Helper()
	rec := e.Get(t, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got reminders.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return got
}

// What is coming is listed, soonest first.
func TestListShowsWhatIsComing(t *testing.T) {
	e, store := env(t)
	now := time.Now().UTC()
	put(t, e, store, "Later", now.Add(2*time.Hour))
	put(t, e, store, "Sooner", now.Add(time.Hour))

	got := listed(t, e, "/v1/reminders")
	if len(got.Reminders) != 2 {
		t.Fatalf("got %d, want 2", len(got.Reminders))
	}
	if got.Reminders[0].Title != "Sooner" {
		t.Errorf("first is %q, want the sooner one", got.Reminders[0].Title)
	}
	if got.Reminders[0].Say == "" || got.Reminders[0].Status != string(remind.Pending) {
		t.Errorf("reminder = %+v", got.Reminders[0])
	}
}

// What fired as it should is left out; what was never said is not. A
// reminder that vanished silently is the failure this screen exists to
// make visible.
func TestFinishedOnesAreLeftOutButMissedOnesAreNot(t *testing.T) {
	e, store := env(t)
	now := time.Now().UTC()
	coming := put(t, e, store, "Coming", now.Add(time.Hour))
	gone := put(t, e, store, "Gone", now.Add(-time.Hour))
	if err := store.Cancel(t.Context(), e.User.ID, gone.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	missed := put(t, e, store, "Never said", now.Add(-3*time.Hour))
	if err := store.Missed(t.Context(), missed.ID, now); err != nil {
		t.Fatalf("Missed: %v", err)
	}

	got := listed(t, e, "/v1/reminders")
	var sawComing, sawMissed, sawCancelled bool
	for _, r := range got.Reminders {
		switch r.ID {
		case coming.ID:
			sawComing = true
		case missed.ID:
			sawMissed = true
		case gone.ID:
			sawCancelled = true
		}
	}
	if !sawComing || !sawMissed {
		t.Errorf("listing = %+v, want the coming one and the missed one", got.Reminders)
	}
	if sawCancelled {
		t.Error("a cancelled reminder was listed")
	}

	all := listed(t, e, "/v1/reminders?all=true")
	if len(all.Reminders) != 3 {
		t.Errorf("got %d with all=true, want 3", len(all.Reminders))
	}
}

// Nothing waiting is an empty list, not a failure.
func TestNothingWaitingIsAnEmptyList(t *testing.T) {
	e, _ := env(t)

	if got := listed(t, e, "/v1/reminders"); len(got.Reminders) != 0 {
		t.Errorf("got %d, want none", len(got.Reminders))
	}
}

// Cancelling stops it, and says so.
func TestCancellingStopsIt(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Timer", time.Now().UTC().Add(time.Hour))

	rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+r.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	after, err := store.Get(t.Context(), e.User.ID, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != remind.Cancelled {
		t.Errorf("status = %q, want cancelled", after.Status)
	}
}

// An identifier that is not one, or names nothing, is missing rather than
// an error.
func TestAnUnknownReminderIsNotFound(t *testing.T) {
	e, _ := env(t)

	for _, id := range []string{remind.NewID(), "not-an-id", "rem_garbage"} {
		if rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+id, ""); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE %s: status = %d, want 404", id, rec.Code)
		}
	}
}

// One person must never see or call off another's, and must not learn it
// exists either.
func TestAnotherPersonsReminderIsHidden(t *testing.T) {
	e, store := env(t)

	stranger, err := chat.NewUser("stranger", "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := e.Repo.CreateUser(t.Context(), stranger); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	theirs, err := remind.New(stranger.ID, "", remind.ScopeUser, "Private", "their business", time.Now().Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(t.Context(), theirs); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := listed(t, e, "/v1/reminders?all=true"); len(got.Reminders) != 0 {
		t.Errorf("another person's reminder was listed: %+v", got.Reminders)
	}
	if rec := e.Do(t, http.MethodDelete, "/v1/reminders/"+theirs.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	still, err := store.Get(t.Context(), stranger.ID, theirs.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if still.Status != remind.Pending {
		t.Error("another person's reminder was called off")
	}
}

// A server with nowhere to keep reminders serves an empty list rather
// than failing.
func TestNoStoreIsAnEmptyList(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{})

	if got := listed(t, e, "/v1/reminders"); len(got.Reminders) != 0 {
		t.Errorf("got %d, want none", len(got.Reminders))
	}
}

// snoozed : Puts one off through the endpoint, decoded.
func snoozed(t *testing.T, e *apitest.Env, id, body string) reminders.SnoozeResponse {
	t.Helper()
	rec := e.Do(t, http.MethodPost, "/v1/reminders/"+id+"/snooze", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got reminders.SnoozeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return got
}

// Putting one off moves it and leaves it waiting.
func TestSnoozingMovesIt(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Tablets", time.Now().UTC().Add(time.Minute))

	got := snoozed(t, e, r.ID, `{"minutes":30}`)
	if got.Added {
		t.Error("a one-off was added for something that is not a series")
	}

	after, err := store.Get(t.Context(), e.User.ID, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want pending", after.Status)
	}
	if left := time.Until(after.DueAt); left < 29*time.Minute || left > 31*time.Minute {
		t.Errorf("due in %v, want about thirty minutes", left)
	}
}

// The button sends nothing, and nothing means ten minutes.
func TestAnEmptyBodyIsTenMinutes(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Tablets", time.Now().UTC().Add(time.Minute))

	snoozed(t, e, r.ID, "")

	after, err := store.Get(t.Context(), e.User.ID, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if left := time.Until(after.DueAt); left < 9*time.Minute || left > 11*time.Minute {
		t.Errorf("due in %v, want about ten minutes", left)
	}
}

// A repeating one is not moved, and the answer says so, or the screen
// tells somebody their daily alarm has shifted when it has not.
func TestSnoozingASeriesAddsOneBeside(t *testing.T) {
	e, store := env(t)
	due := time.Now().UTC().Add(time.Minute)
	r, err := remind.New(e.User.ID, "", remind.ScopeUser, "Wake", "It is seven.", due, remind.Daily)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(t.Context(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := snoozed(t, e, r.ID, `{"minutes":10}`)
	if !got.Added {
		t.Error("the answer does not say a separate one was added")
	}
	if got.Reminder.ID == r.ID {
		t.Error("the series itself was answered with, so the screen would show it moved")
	}

	after, err := store.Get(t.Context(), e.User.ID, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !after.DueAt.Equal(due.Truncate(time.Nanosecond)) {
		t.Errorf("the series moved to %v, want %v", after.DueAt, due)
	}
}

// One already called off has nothing to put off.
func TestSnoozingACancelledOneConflicts(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Tablets", time.Now().UTC().Add(time.Minute))
	if err := store.Cancel(t.Context(), e.User.ID, r.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	rec := e.Do(t, http.MethodPost, "/v1/reminders/"+r.ID+"/snooze", `{"minutes":10}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// The same 404 as everywhere else, so another person's reminder is not
// revealed by the difference between a refusal and a miss.
func TestSnoozingAnotherPersonsIsNotFound(t *testing.T) {
	e, store := env(t)
	other, err := remind.New(chat.NewUserID(), "", remind.ScopeUser, "Theirs", "Not yours.",
		time.Now().UTC().Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(t.Context(), other); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec := e.Do(t, http.MethodPost, "/v1/reminders/"+other.ID+"/snooze", `{"minutes":10}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// An identifier that is not one is refused before it reaches the store.
func TestSnoozingAnInventedIdentifierIsNotFound(t *testing.T) {
	e, _ := env(t)

	rec := e.Do(t, http.MethodPost, "/v1/reminders/not-an-id/snooze", `{"minutes":10}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// A length nobody means is refused rather than stored.
func TestAnAbsurdLengthIsRefused(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Tablets", time.Now().UTC().Add(time.Minute))

	for _, body := range []string{`{"minutes":-5}`, `{"minutes":99999999}`} {
		rec := e.Do(t, http.MethodPost, "/v1/reminders/"+r.ID+"/snooze", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", body, rec.Code, rec.Body.String())
		}
	}
}

// A held reminder is waiting, and leaving it off the screen made one
// kept back while somebody was out of the room vanish.
func TestAHeldReminderIsListed(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Tablets", time.Now().UTC().Add(-time.Minute))
	if err := store.Hold(t.Context(), r.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	got := listed(t, e, "/v1/reminders")
	if len(got.Reminders) != 1 || got.Reminders[0].Status != "held" {
		t.Fatalf("got %v, want the held one", got.Reminders)
	}
}

// What already happened is asked for, not shown by default: a screen
// that opens on a log buries the things actually coming.
func TestWhatHappenedIsOnlyShownWhenAsked(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Gone", time.Now().UTC().Add(-time.Hour))
	if err := store.Fired(t.Context(), r.ID, time.Now().UTC(), time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	if got := listed(t, e, "/v1/reminders"); len(got.Reminders) != 0 {
		t.Errorf("got %v, want nothing by default", got.Reminders)
	}
	got := listed(t, e, "/v1/reminders?past=true")
	if len(got.Reminders) != 1 || got.Reminders[0].Status != "done" {
		t.Errorf("got %v, want the finished one", got.Reminders)
	}
}

// A cancelled one was never received. Listing it among things that
// happened says it happened, so past leaves it out and all does not.
func TestACancelledOneIsNotAPastReminder(t *testing.T) {
	e, store := env(t)
	r := put(t, e, store, "Called off", time.Now().UTC().Add(time.Hour))
	if err := store.Cancel(t.Context(), e.User.ID, r.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if got := listed(t, e, "/v1/reminders?past=true"); len(got.Reminders) != 0 {
		t.Errorf("got %v, want nothing: it never reached anybody", got.Reminders)
	}
	if got := listed(t, e, "/v1/reminders?all=true"); len(got.Reminders) != 1 {
		t.Errorf("got %v, want it under the whole record", got.Reminders)
	}
}
