// Package storetest runs one set of cases against any remind.Store.
//
// The stores have drifted before. An in-memory one that accepted what
// MySQL refused made a test pass where the server would have failed, and
// the difference only showed up in use. Anything whose behaviour both must
// share belongs here rather than in either package's own tests.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// New : Opens a store and returns it with a user to own the reminders.
//
// A fresh one per case: MySQL's is a real database shared with other
// runs, so a case that reads everything would otherwise see their rows.
type New func(t *testing.T) (remind.Store, string)

// Run : Every case both stores must pass.
func Run(t *testing.T, open New) {
	t.Helper()

	for _, c := range []struct {
		name string
		run  func(t *testing.T, open New)
	}{
		{"a reminder that rang can be put off", aFiredOneRingsAgain},
		{"one still waiting can be pushed back", aWaitingOneMovesBack},
		{"putting one off makes it due again", aSnoozedOneIsDue},
		{"how often it rang survives", firesSurviveASnooze},
		{"a repeating one is not moved", aRepeatingOneIsRefused},
		{"a missed one is not put off", aMissedOneIsRefused},
		{"a cancelled one is not put off", aCancelledOneIsRefused},
		{"somebody else's is not theirs to move", anotherPersonsIsNotFound},
		{"put off until nothing is refused", noTimeIsRefused},
		{"one held back is waiting", aHeldOneIsWaiting},
		{"a held one can be recorded as said", aHeldOneCanBeDelivered},
		{"what was just said comes back", whatWasSaidComesBack},
		{"the most recent is first", theMostRecentIsFirst},
		{"two said together both come back", twoTogetherBothComeBack},
		{"older than the window is left out", tooOldIsLeftOut},
		{"one never said is not said", neverSaidIsNotReturned},
		{"somebody else's is not listed", anotherPersonsIsNotSpoken},
	} {
		t.Run(c.name, func(t *testing.T) { c.run(t, open) })
	}
}

// ---------------------------------------------------------------- snooze

// A one-shot that already rang goes back to pending at the new time. This
// is the whole point: "snooze that" said after it went off.
func aFiredOneRingsAgain(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, r.ID, now())

	later := now().Add(10 * time.Minute)
	if err := s.Snooze(ctx, user, r.ID, later); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	got := read(t, s, user, r.ID)
	if got.Status != remind.Pending {
		t.Errorf("status = %q, want pending: a reminder put off must ring again", got.Status)
	}
	if !got.DueAt.Equal(later) {
		t.Errorf("due at %v, want %v", got.DueAt, later)
	}
}

// Pushing one back before it rings: "move my four o'clock to five".
func aWaitingOneMovesBack(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Call", now().Add(time.Hour), remind.Once)

	later := now().Add(2 * time.Hour)
	if err := s.Snooze(ctx, user, r.ID, later); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	if got := read(t, s, user, r.ID); !got.DueAt.Equal(later) {
		t.Errorf("due at %v, want %v", got.DueAt, later)
	}
}

// Pending at a new time is not enough; the firing loop has to pick it up,
// or it was put off into silence.
func aSnoozedOneIsDue(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, r.ID, now())

	if err := s.Snooze(ctx, user, r.ID, ago(time.Second)); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	// Everything, not one batch. Due is not scoped to a person -- the
	// firing loop wants every one that is ready -- so in a database
	// another run has left rows in, a batch of twenty is twenty of
	// somebody else's.
	due, err := s.Due(ctx, now(), 100000)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	for i := range due {
		if due[i].ID == r.ID {
			return
		}
	}
	t.Error("a reminder put off until now is not due: it would never ring again")
}

// How many times it has gone off is worth keeping. It is the only way to
// know a thing has been put off four times running.
func firesSurviveASnooze(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, r.ID, now())

	if err := s.Snooze(ctx, user, r.ID, now().Add(time.Minute)); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	if got := read(t, s, user, r.ID); got.Fires != 1 {
		t.Errorf("fires = %d, want 1: putting it off is not unsaying it", got.Fires)
	}
}

// Moving a daily one's due time would move every day after it with it.
// The refusal is what keeps a seven o'clock at seven.
func aRepeatingOneIsRefused(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	due := now().Add(time.Hour)
	r := stored(t, s, user, "Wake", due, remind.Daily)

	err := s.Snooze(ctx, user, r.ID, now().Add(10*time.Minute))
	if !errors.Is(err, remind.ErrSnoozeRepeats) {
		t.Fatalf("Snooze = %v, want ErrSnoozeRepeats", err)
	}
	if got := read(t, s, user, r.ID); !got.DueAt.Equal(due) {
		t.Errorf("the series moved to %v, and would drift further every day", got.DueAt)
	}
}

// A missed one was never said. It is set again, not put off.
func aMissedOneIsRefused(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Bins", ago(2*time.Hour), remind.Once)
	if err := s.Missed(ctx, r.ID, now()); err != nil {
		t.Fatalf("Missed: %v", err)
	}

	if err := s.Snooze(ctx, user, r.ID, now().Add(time.Minute)); !errors.Is(err, remind.ErrNotSnoozable) {
		t.Errorf("Snooze = %v, want ErrNotSnoozable", err)
	}
}

// Called off on purpose. Bringing it back is not putting it off.
func aCancelledOneIsRefused(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Dentist", now().Add(time.Hour), remind.Once)
	if err := s.Cancel(ctx, user, r.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if err := s.Snooze(ctx, user, r.ID, now().Add(time.Minute)); !errors.Is(err, remind.ErrNotSnoozable) {
		t.Errorf("Snooze = %v, want ErrNotSnoozable", err)
	}
}

// Ownership, in the same terms as everything else here: not theirs is not
// there.
func anotherPersonsIsNotFound(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", now().Add(time.Hour), remind.Once)

	if err := s.Snooze(ctx, "usr_someone_else", r.ID, now().Add(time.Minute)); !errors.Is(err, remind.ErrNotFound) {
		t.Errorf("Snooze = %v, want ErrNotFound", err)
	}
	if got := read(t, s, user, r.ID); got.Status != remind.Pending {
		t.Errorf("status = %q: somebody else's reminder was changed", got.Status)
	}
}

// A zero time is a reminder due at no moment, which is how one vanishes.
func noTimeIsRefused(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", now().Add(time.Hour), remind.Once)

	if err := s.Snooze(ctx, user, r.ID, time.Time{}); !errors.Is(err, remind.ErrNoTime) {
		t.Errorf("Snooze = %v, want ErrNoTime", err)
	}
}

// ----------------------------------------------------------- last spoken

// The reminder that just went off is what "that" means.
func whatWasSaidComesBack(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, r.ID, now())

	got, err := s.LastSpoken(ctx, user, ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 1 || got[0].ID != r.ID {
		t.Fatalf("LastSpoken = %v, want the one just said", ids(got))
	}
}

// Newest first, because "that" means the last thing said and not the
// first.
func theMostRecentIsFirst(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	older := stored(t, s, user, "Bins", ago(10*time.Minute), remind.Once)
	newer := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, older.ID, ago(9*time.Minute))
	fire(t, s, newer.ID, ago(time.Minute))

	got, err := s.LastSpoken(ctx, user, ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LastSpoken = %v, want both", ids(got))
	}
	if got[0].ID != newer.ID {
		t.Errorf("LastSpoken = %v, want the most recent first", ids(got))
	}
}

// Two due at once is the case that must not be answered by guessing. Both
// come back so the caller can ask which.
func twoTogetherBothComeBack(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	at := ago(time.Minute)
	first := stored(t, s, user, "Tablets", at, remind.Once)
	second := stored(t, s, user, "Call mum", at, remind.Once)
	fire(t, s, first.ID, at)
	fire(t, s, second.ID, at)

	got, err := s.LastSpoken(ctx, user, ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LastSpoken = %v, want both: one answer here is a guess", ids(got))
	}
}

// Past the window it is not "that" any more, and picking it would put off
// something said an hour ago.
func tooOldIsLeftOut(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Hour), remind.Once)
	fire(t, s, r.ID, ago(time.Hour))

	got, err := s.LastSpoken(ctx, user, ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LastSpoken = %v, want nothing that old", ids(got))
	}
}

// One waiting to go off has not been said, so it cannot be what was said.
func neverSaidIsNotReturned(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	stored(t, s, user, "Tablets", now().Add(time.Hour), remind.Once)

	got, err := s.LastSpoken(ctx, user, ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LastSpoken = %v, want nothing: it has not gone off yet", ids(got))
	}
}

func anotherPersonsIsNotSpoken(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	fire(t, s, r.ID, now())

	got, err := s.LastSpoken(ctx, "usr_someone_else", ago(15*time.Minute))
	if err != nil {
		t.Fatalf("LastSpoken: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LastSpoken = %v, want nothing of somebody else's", ids(got))
	}
}

// Kept back because nobody was there to hear it. Its due time is left
// alone: that is what the reminder was for, and what the person is told.
func aHeldOneIsWaiting(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	due := ago(time.Minute)
	r := stored(t, s, user, "Tablets", due, remind.Once)

	if err := s.Hold(ctx, r.ID, now()); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	got := read(t, s, user, r.ID)
	if got.Status != remind.Held {
		t.Errorf("status = %q, want held", got.Status)
	}
	if !got.DueAt.Equal(due) {
		t.Errorf("due moved to %v, want %v: it says what the reminder was for", got.DueAt, due)
	}

	waiting, err := s.Waiting(ctx, user)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(waiting) != 1 || waiting[0].ID != r.ID {
		t.Errorf("Waiting = %v, want the held one", ids(waiting))
	}
}

// Delivering one on arrival has to be recordable, or it is said and stays
// held and is said again on the next arrival.
func aHeldOneCanBeDelivered(t *testing.T, open New) {
	ctx, s, user := setup(t, open)
	r := stored(t, s, user, "Tablets", ago(time.Minute), remind.Once)
	if err := s.Hold(ctx, r.ID, now()); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	if err := s.Fired(ctx, r.ID, now(), time.Time{}); err != nil {
		t.Fatalf("Fired on a held one: %v", err)
	}

	got := read(t, s, user, r.ID)
	if got.Status != remind.Done {
		t.Errorf("status = %q, want done", got.Status)
	}
	waiting, err := s.Waiting(ctx, user)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(waiting) != 0 {
		t.Errorf("still waiting after delivery: %v", ids(waiting))
	}
}

// ---------------------------------------------------------------- helpers

// now : The moment, rounded to what MySQL keeps, so a comparison is not
// lost to the fractional part the column drops.
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// ago : A moment in the past.
func ago(d time.Duration) time.Time { return now().Add(-d) }

// setup : A fresh store, somebody to own the reminders, and a context.
func setup(t *testing.T, open New) (context.Context, remind.Store, string) {
	t.Helper()
	s, user := open(t)
	return context.Background(), s, user
}

// stored : A reminder in the store, due at the given time.
func stored(t *testing.T, s remind.Store, user, title string, due time.Time, repeats remind.Repeat) *remind.Reminder {
	t.Helper()
	r, err := remind.New(user, "", remind.ScopeUser, title, "time to "+title, due, repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// fire : Records that a one-shot went off at the given moment.
func fire(t *testing.T, s remind.Store, id string, at time.Time) {
	t.Helper()
	if err := s.Fired(context.Background(), id, at, time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}
}

// read : One reminder, which must be there.
func read(t *testing.T, s remind.Store, user, id string) *remind.Reminder {
	t.Helper()
	got, err := s.Get(context.Background(), user, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got
}

// ids : What came back, for a failure message.
func ids(all []remind.Reminder) []string {
	out := make([]string, 0, len(all))
	for i := range all {
		out = append(out, all[i].ID+" ("+all[i].Title+")")
	}
	return out
}
