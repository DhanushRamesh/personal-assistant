package remind_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// heard : A Speaker that records what it was given, and can refuse.
type heard struct {
	mu     sync.Mutex
	said   []remind.Reminder
	refuse error
}

func (h *heard) Say(_ context.Context, r remind.Reminder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refuse != nil {
		return h.refuse
	}
	h.said = append(h.said, r)
	return nil
}

func (h *heard) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.said)
}

// loop : A loop over a fresh store, with the clock held still.
func loop(t *testing.T, at time.Time) (*remind.Loop, *inmemory.Store, *heard) {
	t.Helper()
	store, speaker := inmemory.New(), &heard{}
	return &remind.Loop{
		Store:    store,
		Speaker:  speaker,
		Location: india,
		Now:      func() time.Time { return at },
	}, store, speaker
}

// put : Stores a reminder due at the given time.
func put(t *testing.T, s *inmemory.Store, due time.Time, repeats remind.Repeat) *remind.Reminder {
	t.Helper()
	r, err := remind.New("usr_1", "", remind.ScopeUser, "Wake", "time to get up", due, repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

// Something due is said, once, and finished.
func TestSomethingDueIsSaidAndFinished(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, now.Add(-time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Said != 1 {
		t.Fatalf("pass = %+v, want one said", got)
	}
	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Done {
		t.Errorf("status = %q, want done", after.Status)
	}
}

// Nothing due is said, which is almost every pass for ever.
func TestNothingDueSaysNothing(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(time.Hour), remind.Once)

	if got := l.Once(context.Background()); got != (remind.Pass{}) {
		t.Errorf("pass = %+v, want nothing done", got)
	}
	if spoke.count() != 0 {
		t.Error("something not yet due was said")
	}
}

// Two passes do not say the same thing twice.
func TestItIsNotSaidTwice(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(-time.Minute), remind.Once)

	l.Once(context.Background())
	l.Once(context.Background())

	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}
}

// A repeating one is said and comes back, rather than finishing.
func TestARepeatingOneComesBack(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, _ := loop(t, now)
	r := put(t, store, now.Add(-time.Minute), remind.Daily)

	l.Once(context.Background())

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still pending", after.Status)
	}
	if !after.DueAt.After(now) {
		t.Errorf("due = %v, want a time after now", after.DueAt.In(india))
	}
	if after.Fires != 1 {
		t.Errorf("fires = %d, want 1", after.Fires)
	}
}

// Something far too late is not said. A timer three hours late is wrong.
func TestSomethingTooLateIsNotSaid(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, now.Add(-3*time.Hour), remind.Once)

	if got := l.Once(context.Background()); got.Missed != 1 {
		t.Fatalf("pass = %+v, want one missed", got)
	}
	if spoke.count() != 0 {
		t.Error("something three hours late was said anyway")
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Missed {
		t.Errorf("status = %q, want missed", after.Status)
	}
}

// Something a little late is still said, because it is still worth having.
func TestSomethingALittleLateIsStillSaid(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	put(t, store, now.Add(-20*time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Said != 1 {
		t.Errorf("pass = %+v, want it said", got)
	}
	if spoke.count() != 1 {
		t.Error("something twenty minutes late was dropped")
	}
}

// A repeating one that was missed is moved on, not ended. Marking it
// missed would stop it for good.
func TestAMissedRepeatIsMovedOnNotEnded(t *testing.T) {
	now := at(2026, 9, 26, 12, 0)
	l, store, spoke := loop(t, now)
	r := put(t, store, at(2026, 9, 20, 7, 0), remind.Daily)

	got := l.Once(context.Background())
	if got.Moved != 1 || got.Missed != 0 {
		t.Fatalf("pass = %+v, want it moved on", got)
	}
	if spoke.count() != 0 {
		t.Error("a reminder six days late was said")
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still alive", after.Status)
	}
	if next := after.DueAt.In(india); next.Day() != 27 || next.Hour() != 7 {
		t.Errorf("next = %v, want 7am on the 27th", next)
	}
	if after.Fires != 0 {
		t.Errorf("fires = %d, want nothing counted: nobody heard it", after.Fires)
	}
}

// Nothing to say it means it stays pending and is tried again, rather
// than being recorded as delivered.
func TestSomethingNobodyCouldSayStaysPending(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	l, store, spoke := loop(t, now)
	spoke.refuse = remind.ErrNowhereToSay
	r := put(t, store, now.Add(-time.Minute), remind.Once)

	if got := l.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want one failure", got)
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want it still pending to try again", after.Status)
	}
	if after.Fires != 0 {
		t.Errorf("fires = %d, want nothing counted", after.Fires)
	}
}

// Trying again cannot go on for ever: once it is older than the grace
// window it becomes missed.
func TestRetryingEndsAtTheGraceWindow(t *testing.T) {
	due := at(2026, 9, 26, 7, 0)
	store, spoke := inmemory.New(), &heard{refuse: remind.ErrNowhereToSay}

	now := due.Add(time.Minute)
	l := &remind.Loop{
		Store: store, Speaker: spoke, Location: india,
		Now: func() time.Time { return now },
	}
	r := put(t, store, due, remind.Once)

	if got := l.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want it retried", got)
	}

	now = due.Add(2 * time.Hour)
	if got := l.Once(context.Background()); got.Missed != 1 {
		t.Fatalf("pass = %+v, want it given up on", got)
	}

	after, _ := store.Get(context.Background(), "usr_1", r.ID)
	if after.Status != remind.Missed {
		t.Errorf("status = %q, want missed", after.Status)
	}
}

// Running says what is due and stops when told.
func TestRunSaysAndStops(t *testing.T) {
	now := time.Now().UTC()
	store, spoke := inmemory.New(), &heard{}
	l := &remind.Loop{
		Store: store, Speaker: spoke, Location: india,
		Every: 5 * time.Millisecond,
	}
	put(t, store, now.Add(-time.Minute), remind.Once)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for spoke.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not stop when its context ended")
	}
	if spoke.count() != 1 {
		t.Errorf("said %d times, want once", spoke.count())
	}
}

// Nothing configured to speak refuses rather than reporting success, so a
// reminder nobody could hear is not recorded as delivered.
func TestNowhereRefuses(t *testing.T) {
	err := remind.Nowhere{}.Say(context.Background(), remind.Reminder{})
	if !errors.Is(err, remind.ErrNowhereToSay) {
		t.Errorf("error = %v, want ErrNowhereToSay", err)
	}
}

// One delivery is enough: a browser that is closed does not stop the
// satellite saying it.
func TestEverywhereNeedsOnlyOneToTake(t *testing.T) {
	good := &heard{}
	e := remind.Everywhere{To: []remind.Speaker{remind.Nowhere{}, good}}

	if err := e.Say(context.Background(), remind.Reminder{Body: "up"}); err != nil {
		t.Errorf("Say: %v", err)
	}
	if good.count() != 1 {
		t.Error("the one place that would take it did not get it")
	}
}

// Nowhere at all is a failure, not a quiet success.
func TestEverywhereFailsWhenNobodyTakesIt(t *testing.T) {
	e := remind.Everywhere{To: []remind.Speaker{remind.Nowhere{}, remind.Nowhere{}}}

	if err := e.Say(context.Background(), remind.Reminder{Body: "up"}); err == nil {
		t.Error("it reported success with nowhere to deliver")
	}
}

// What is said is what the reminder says, not its filing name.
func TestSpokenIsTheBodyNotTheTitle(t *testing.T) {
	now := at(2026, 9, 26, 7, 0)
	got := remind.Spoken(remind.Reminder{Title: "Wake", Body: "time to get up", DueAt: now}, now, india)
	if got != "time to get up" {
		t.Errorf("Spoken = %q", got)
	}
}

// A late one says so, and says what time it was due. Said at a quarter to
// eleven, one that sounded exactly like a reminder said at ten would be
// acted on as though it were ten.
func TestALateReminderSaysSo(t *testing.T) {
	due := at(2026, 9, 26, 10, 0)
	got := remind.Spoken(
		remind.Reminder{Title: "Call", Body: "Time to call the roofer.", DueAt: due},
		due.Add(45*time.Minute), india)

	for _, want := range []string{"should have heard this", "10:00 am", "Time to call the roofer."} {
		if !strings.Contains(got, want) {
			t.Errorf("Spoken is missing %q:\n%s", want, got)
		}
	}
	// The lateness comes first. "Time to call the roofer" heard at a
	// quarter to eleven sounds like now unless something says otherwise
	// before the words arrive.
	if strings.Index(got, "10:00 am") > strings.Index(got, "Time to call") {
		t.Errorf("the time it was due comes after the reminder itself:\n%s", got)
	}
}

// A moment's lateness is the loop's own delay and the wait for the
// satellite to fall quiet. Remarking on it would be noise.
func TestAMomentLateSaysNothing(t *testing.T) {
	due := at(2026, 9, 26, 10, 0)
	got := remind.Spoken(
		remind.Reminder{Body: "Time to call.", DueAt: due}, due.Add(30*time.Second), india)

	if strings.Contains(got, "late") {
		t.Errorf("Spoken = %q, want no remark", got)
	}
}

// One that crossed midnight names the day, or "due at 11:50 pm" reads as
// tonight.
func TestALateReminderFromYesterdayNamesTheDay(t *testing.T) {
	due := at(2026, 9, 26, 23, 50)
	got := remind.Spoken(
		remind.Reminder{Body: "Time for bed.", DueAt: due}, due.Add(30*time.Minute), india)

	if !strings.Contains(got, "on Saturday") {
		t.Errorf("Spoken does not name the day:\n%s", got)
	}
}

// elsewhere : A presence that answers however the test says.
type elsewhere struct {
	away bool
	// unsure : Nothing current backs the answer up. The zero value is a
	// confident one, so the tests that predate this read unchanged.
	unsure bool
}

func (e elsewhere) Look(context.Context, string) remind.Where {
	return remind.Where{Away: e.away, Sure: !e.unsure}
}

// held : A reminder due now, for the presence tests.
func heldCase(t *testing.T, repeats remind.Repeat) (*inmemory.Store, *remind.Reminder) {
	t.Helper()
	store := inmemory.New()
	r, err := remind.New("usr_1", "", remind.ScopeUser, "Tablets", "Take your tablets.",
		time.Now().UTC().Add(-time.Second), repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, r
}

// Nobody in the room, so it is kept back rather than said to an empty one.
func TestAReminderIsHeldWhenNobodyIsThere(t *testing.T) {
	store, r := heldCase(t, remind.Once)
	sat := &heard{}
	loop := &remind.Loop{Store: store, Speaker: sat, Presence: elsewhere{away: true}}

	pass := loop.Once(context.Background())

	if pass.Held != 1 || pass.Said != 0 {
		t.Errorf("pass = %+v, want one held and nothing said", pass)
	}
	got, err := store.Get(context.Background(), "usr_1", r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != remind.Held {
		t.Errorf("status = %q, want held", got.Status)
	}
	if !got.DueAt.Equal(r.DueAt) {
		t.Error("holding moved its due time; it says what the reminder was for")
	}
}

// The rule that matters. Anything short of a confident "they are out"
// speaks: presence here is a signal that has already been seen to drift
// ten decibels in half an hour, and a reminder withheld from somebody
// sitting there is lost until they think to ask.
func TestNotKnowingMeansSayingIt(t *testing.T) {
	for _, c := range []struct {
		name string
		p    remind.Presence
	}{
		{"nothing configured", nil},
		{"it says they are here", elsewhere{away: false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			store, _ := heldCase(t, remind.Once)
			sat := &heard{}
			loop := &remind.Loop{Store: store, Speaker: sat, Presence: c.p}

			if pass := loop.Once(context.Background()); pass.Said != 1 || pass.Held != 0 {
				t.Errorf("pass = %+v, want it said", pass)
			}
		})
	}
}

// A repeating one is not held. Its next turn is along soon enough, and
// holding one would queue a morning alarm to go off at lunchtime.
func TestARepeatingReminderIsNotHeld(t *testing.T) {
	store, _ := heldCase(t, remind.Daily)
	sat := &heard{}
	loop := &remind.Loop{Store: store, Speaker: sat, Presence: elsewhere{away: true}}

	if pass := loop.Once(context.Background()); pass.Held != 0 || pass.Said != 1 {
		t.Errorf("pass = %+v, want the repeating one said, not held", pass)
	}
}

// What is held is what gets delivered, oldest first.
func TestWhatIsHeldIsWaiting(t *testing.T) {
	store, r := heldCase(t, remind.Once)
	loop := &remind.Loop{Store: store, Speaker: &heard{}, Presence: elsewhere{away: true}}
	loop.Once(context.Background())

	waiting, err := store.Waiting(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(waiting) != 1 || waiting[0].ID != r.ID {
		t.Errorf("Waiting = %v, want the held one", waiting)
	}
}

// A held reminder waits for somebody to walk in. Until this it waited
// for ever: Due returns only pending ones, so nothing looked at it
// again, and a ten o'clock reminder was still spoken at seven.
func TestAReminderHeldTooLongBecomesAMiss(t *testing.T) {
	store := inmemory.New()
	old, err := remind.New("usr_1", "", remind.ScopeUser, "Tablets", "Take your tablets.",
		time.Now().UTC().Add(-3*time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), old); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Hold(context.Background(), old.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	loop := &remind.Loop{Store: store, Speaker: &heard{}, Presence: elsewhere{away: true}}
	if pass := loop.Once(context.Background()); pass.Missed != 1 {
		t.Errorf("pass = %+v, want the stale one given up on", pass)
	}

	got, err := store.Get(context.Background(), "usr_1", old.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != remind.Missed {
		t.Errorf("status = %q, want missed", got.Status)
	}
}

// One held a few minutes ago is still worth saying when they walk in.
func TestARecentlyHeldReminderIsLeftAlone(t *testing.T) {
	store := inmemory.New()
	r, err := remind.New("usr_1", "", remind.ScopeUser, "Tablets", "Take your tablets.",
		time.Now().UTC().Add(-2*time.Minute), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Hold(context.Background(), r.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Hold: %v", err)
	}

	loop := &remind.Loop{Store: store, Speaker: &heard{}, Presence: elsewhere{away: true}}
	if pass := loop.Once(context.Background()); pass.Missed != 0 {
		t.Errorf("pass = %+v, want it left waiting", pass)
	}

	waiting, err := store.Waiting(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(waiting) != 1 {
		t.Errorf("Waiting = %v, want it still held", waiting)
	}
}

// The past-tense wording, with the hour put in by the server. The
// grammar is the model's, written when the reminder was set; the time
// is arithmetic and arithmetic is not the model's to get wrong.
func TestALateReminderUsesThePastTenseWording(t *testing.T) {
	due := at(2026, 9, 26, 10, 0)
	got := remind.Spoken(remind.Reminder{
		Title:    "Tablets",
		Body:     "Time to take your tablets, sir.",
		SaidLate: "You should have taken your tablets.",
		DueAt:    due,
	}, due.Add(2*time.Hour), india)

	want := "You should have taken your tablets at 10:00 am, sir."
	if got != want {
		t.Errorf("Spoken = %q, want %q", got, want)
	}
}

// On time it says the ordinary thing, whatever past form was written.
func TestThePastTenseWordingIsOnlyUsedWhenLate(t *testing.T) {
	due := at(2026, 9, 26, 10, 0)
	got := remind.Spoken(remind.Reminder{
		Body:     "Time to take your tablets, sir.",
		SaidLate: "You should have taken your tablets.",
		DueAt:    due,
	}, due, india)

	if got != "Time to take your tablets, sir." {
		t.Errorf("Spoken = %q, want the ordinary wording", got)
	}
}

// Nothing written falls back to the hour in front of the body, which is
// what every reminder made before this has.
func TestWithoutAPastTenseWordingItFallsBack(t *testing.T) {
	due := at(2026, 9, 26, 10, 0)
	got := remind.Spoken(remind.Reminder{
		Body:  "Time to take your tablets, sir.",
		DueAt: due,
	}, due.Add(2*time.Hour), india)

	for _, want := range []string{"should have heard this", "10:00 am", "Time to take your tablets"} {
		if !strings.Contains(got, want) {
			t.Errorf("Spoken = %q, missing %q", got, want)
		}
	}
}

// A reminder said while nothing could confirm anybody was there is
// delivered, and noted as unwitnessed so it can be raised at the door.
//
// Presence is fail-safe and speaks whenever it cannot be sure, which is
// right. But it cannot tell "they are here" from "I have no idea", and
// treated both as heard: a reminder spoken into a twelve-minute hole
// where the sensor did not exist was recorded as said, so walking back
// in produced a greeting and no mention of it.
func TestAReminderSaidWithNobodyConfirmedIsNoted(t *testing.T) {
	store, r := heldCase(t, remind.Once)
	say := &heard{}

	loop := &remind.Loop{
		Store: store, Speaker: say,
		Presence: elsewhere{away: false, unsure: true},
	}
	pass := loop.Once(context.Background())

	if pass.Said != 1 {
		t.Fatalf("pass = %+v, want it said", pass)
	}

	unheard, err := store.Unheard(context.Background(), r.UserID)
	if err != nil {
		t.Fatalf("reading what was said to nobody: %v", err)
	}
	if len(unheard) != 1 || unheard[0].ID != r.ID {
		t.Fatalf("unheard = %v, want the one just said", unheard)
	}
	if unheard[0].UnwitnessedAt == nil {
		t.Error("it was not marked as said to nobody in particular")
	}
}

// Said with somebody confirmed there is an ordinary delivery, and is not
// raised again.
func TestAReminderSaidToSomebodyPresentIsNotRaisedAgain(t *testing.T) {
	store, r := heldCase(t, remind.Once)
	say := &heard{}

	loop := &remind.Loop{
		Store: store, Speaker: say,
		Presence: elsewhere{away: false},
	}
	if pass := loop.Once(context.Background()); pass.Said != 1 {
		t.Fatalf("pass = %+v, want it said", pass)
	}

	unheard, err := store.Unheard(context.Background(), r.UserID)
	if err != nil {
		t.Fatalf("reading what was said to nobody: %v", err)
	}
	if len(unheard) != 0 {
		t.Errorf("unheard = %v, want nothing", unheard)
	}
}

// Being unsure does not hold anything back. Speaking is still the right
// answer; the note is only so the delivery can be questioned later.
func TestBeingUnsureStillSpeaks(t *testing.T) {
	store, _ := heldCase(t, remind.Once)
	say := &heard{}

	loop := &remind.Loop{
		Store: store, Speaker: say,
		Presence: elsewhere{away: false, unsure: true},
	}
	pass := loop.Once(context.Background())

	if pass.Held != 0 {
		t.Errorf("pass = %+v, want nothing held", pass)
	}
	if say.count() != 1 {
		t.Errorf("spoke %d times, want one", say.count())
	}
}

// Known away still holds, unsure or not: a confident "elsewhere" is the
// one answer that stops a reminder.
func TestAConfidentAwayStillHolds(t *testing.T) {
	store, _ := heldCase(t, remind.Once)

	loop := &remind.Loop{
		Store: store, Speaker: &heard{},
		Presence: elsewhere{away: true},
	}
	if pass := loop.Once(context.Background()); pass.Held != 1 {
		t.Fatalf("pass = %+v, want it held", pass)
	}
}
