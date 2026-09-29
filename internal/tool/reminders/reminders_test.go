package reminders_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
)

const (
	user   = "usr_01M3D477HXQ4YNQX7BNXJZZCV0"
	client = "cli_01M3D477HXQ4YNQX7BNXJZZCV0"
)

// india : Where the person is, for these.
var india = time.FixedZone("IST", 5*3600+1800)

// noon : The moment the clock is held at.
var noon = time.Date(2026, 9, 26, 12, 0, 0, 0, india)

// harness : A registry over an empty store, with the clock held still.
func harness(t *testing.T) (*tool.Registry, *inmemory.Store) {
	t.Helper()

	store := inmemory.New()
	clock := reminders.Clock{Now: func() time.Time { return noon }, Location: india}

	r, err := tool.NewRegistry(reminders.All(store, clock)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r, store
}

// call : Calls a tool and returns its result.
func call(t *testing.T, r *tool.Registry, name, args string) tool.Result {
	t.Helper()
	return r.Call(context.Background(), name, tool.Invocation{
		Caller: tool.Caller{UserID: user, ClientID: client, Channel: chat.ChannelVoice},
		Args:   json.RawMessage(args),
		Ran:    alreadyListed,
	})
}

// Every reminder tool has to survive registration, which is where a
// missing description or an untyped argument is caught.
func TestEveryReminderToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(reminders.All(nil, reminders.Clock{})...); err != nil {
		t.Fatalf("a reminder tool is not usable: %v", err)
	}
}

// Nothing here destroys anything, so all of it may be said out loud.
// Counted against the whole set rather than a number, so a tool added
// without a voice channel is caught instead of the count being edited.
func TestVoiceMayUseAllOfThem(t *testing.T) {
	r, _ := harness(t)
	all := len(reminders.All(nil, reminders.Clock{}))

	if got := len(r.For(chat.ChannelVoice)); got != all {
		t.Errorf("voice is offered %d of the %d reminder tools", got, all)
	}
}

// A length of time is counted by the server, not by the model.
func TestMinutesAreCountedHere(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Timer","say":"Your twenty minute timer has finished.","say_if_late":"You should have done it","minutes_from_now":20}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	all, _ := store.List(context.Background(), user, remind.Pending)
	if len(all) != 1 {
		t.Fatalf("stored %d, want 1", len(all))
	}
	if want := noon.Add(20 * time.Minute); !all[0].DueAt.Equal(want) {
		t.Errorf("due = %v, want %v", all[0].DueAt.In(india), want)
	}
}

// A written time is read in the person's own zone, not the server's.
func TestAWrittenTimeIsLocal(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Call","say":"Time to call the roofer.","say_if_late":"You should have done it","at":"2026-09-26 16:30"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	all, _ := store.List(context.Background(), user, remind.Pending)
	if got := all[0].DueAt.In(india); got.Hour() != 16 || got.Minute() != 30 {
		t.Errorf("due at %v, want half past four in India", got)
	}
}

// Several spellings of the same moment are accepted, because a model
// writes it several ways and refusing it would refuse the reminder.
func TestSeveralSpellingsOfATimeWork(t *testing.T) {
	for _, written := range []string{
		"2026-09-26 16:30", "2026-09-26T16:30", "2026-09-26 16:30:00",
		"2026-09-26T16:30:00", "2026-09-26T16:30:00+05:30",
	} {
		r, store := harness(t)
		got := call(t, r, "reminder_set", `{"title":"Call","say":"Call them.","say_if_late":"You should have done it","at":"`+written+`"}`)
		if got.Outcome != conversation.OutcomeOK {
			t.Errorf("%q was refused: %s", written, got.Content)
			continue
		}
		all, _ := store.List(context.Background(), user, remind.Pending)
		if at := all[0].DueAt.In(india); at.Hour() != 16 || at.Minute() != 30 {
			t.Errorf("%q landed at %v", written, at)
		}
	}
}

// Both ways of saying when is ambiguous, and asking is better than
// picking one.
func TestBothWaysOfSayingWhenIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20,"at":"2026-09-26 16:30"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
	if !strings.Contains(got.Content, "Which was meant") {
		t.Errorf("content = %q, want it to ask", got.Content)
	}
}

// Neither way is refused too, rather than defaulting to some moment.
func TestNoTimeAtAllIsRefused(t *testing.T) {
	r, _ := harness(t)

	if got := call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it"}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
}

// A time already gone is refused, and told the current time so the next
// attempt can be right.
func TestATimeInThePastIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set", `{"title":"Call","say":"Call them.","say_if_late":"You should have done it","at":"2026-09-26 09:00"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want it refused", got.Outcome)
	}
	if !strings.Contains(got.Content, "2026-09-26 12:00") {
		t.Errorf("content = %q, want it to say what time it is now", got.Content)
	}
}

// A moment just gone is taken as now. The model works the time out from
// what it was told, and a second or two passes while it does.
func TestAMomentJustGoneIsAccepted(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set", `{"title":"Now","say":"Now.","say_if_late":"You should have done it","at":"2026-09-26 11:59"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Errorf("a minute ago was refused: %s", got.Content)
	}
}

// A repeat is kept, so it comes back.
func TestARepeatIsKept(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set",
		`{"title":"Wake","say":"It is seven o'clock.","say_if_late":"You should have done it","at":"2026-09-28 07:00","repeats":"weekdays"}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if len(all) != 1 || all[0].Repeats != remind.Weekdays {
		t.Errorf("repeats = %q, want weekdays", all[0].Repeats)
	}
}

// A repeat the code does not know is refused rather than silently
// becoming a one-shot.
func TestAnUnknownRepeatIsRefused(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Wake","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20,"repeats":"hourly"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
	if all, _ := store.List(context.Background(), user); len(all) != 0 {
		t.Error("an unknown repeat was stored as something else")
	}
}

// The default follows the person rather than the device they happened to
// use.
func TestTheDefaultScopeFollowsThePerson(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if all[0].Scope != remind.ScopeUser {
		t.Errorf("scope = %q, want user", all[0].Scope)
	}
}

// Asked for, a reminder can belong to the device it was set on.
func TestItCanBeTiedToTheDevice(t *testing.T) {
	r, store := harness(t)

	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20,"scope":"client"}`)

	all, _ := store.List(context.Background(), user, remind.Pending)
	if all[0].Scope != remind.ScopeClient || all[0].ClientID != client {
		t.Errorf("scope = %q, client = %q", all[0].Scope, all[0].ClientID)
	}
}

// A listing says when, in the person's own words, and carries the
// identifier a cancel needs.
func TestTheListingIsUsable(t *testing.T) {
	r, _ := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20}`)

	got := call(t, r, "reminder_list", `{}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}
	for _, want := range []string{"rem_", "12:20 pm", "Timer"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("listing is missing %q:\n%s", want, got.Content)
		}
	}
}

// Nothing waiting says so, rather than returning an empty listing.
func TestAnEmptyListingSaysSo(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_list", `{}`)
	if !strings.Contains(got.Content, "nothing waiting") {
		t.Errorf("content = %q", got.Content)
	}
}

// Cancelling stops it happening.
func TestCancellingStopsIt(t *testing.T) {
	r, store := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20}`)
	all, _ := store.List(context.Background(), user, remind.Pending)

	got := call(t, r, "reminder_cancel", `{"id":"`+all[0].ID+`"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	left, _ := store.List(context.Background(), user, remind.Pending)
	if len(left) != 0 {
		t.Error("it is still waiting to happen")
	}
}

// An identifier that does not exist is refused with advice, not a guess.
func TestCancellingSomethingThatIsNotThere(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_cancel", `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV0"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Fatalf("outcome = %s, want a failure", got.Outcome)
	}
	if !strings.Contains(got.Content, "List them") {
		t.Errorf("content does not say what to do instead: %s", got.Content)
	}
}

// One person's reminder is not reachable by another.
func TestAnotherPersonCannotCancelIt(t *testing.T) {
	r, store := harness(t)
	call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20}`)
	all, _ := store.List(context.Background(), user, remind.Pending)

	got := r.Call(context.Background(), "reminder_cancel", tool.Invocation{
		Caller: tool.Caller{UserID: "usr_01M3D477HXQ4YNQX7BNXJZZCV1", Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"id":"` + all[0].ID + `"}`),
		Ran:    alreadyListed,
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
}

// A request from nobody stores nothing.
func TestARequestFromNobodyStoresNothing(t *testing.T) {
	r, store := harness(t)

	got := r.Call(context.Background(), "reminder_set", tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect},
		Args:   json.RawMessage(`{"title":"Timer","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20}`),
		Ran:    alreadyListed,
	})
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want a failure", got.Outcome)
	}
	if all, _ := store.List(context.Background(), user); len(all) != 0 {
		t.Error("something was stored for nobody")
	}
}

// A timer said in seconds is set in seconds. Refusing it because it is not
// a whole number of minutes was a limitation of the tool, not of anything
// real.
func TestATimerInSecondsWorks(t *testing.T) {
	r, store := harness(t)

	got := call(t, r, "reminder_set",
		`{"title":"Timer","say":"Your thirty second timer has finished.","say_if_late":"You should have done it","seconds_from_now":30}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("outcome = %s: %s", got.Outcome, got.Content)
	}

	all, _ := store.List(context.Background(), user, remind.Pending)
	if want := noon.Add(30 * time.Second); !all[0].DueAt.Equal(want) {
		t.Errorf("due = %v, want %v", all[0].DueAt.In(india), want)
	}
}

// Shorter than the assistant takes to say it is set would go off while it
// is still speaking.
func TestATimerTooShortIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_set", `{"title":"Timer","say":"Up.","say_if_late":"You should have done it","seconds_from_now":1}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("outcome = %s, want it refused", got.Outcome)
	}
}

// Two ways of saying when is ambiguous however they are combined.
func TestOnlyOneWayOfSayingWhen(t *testing.T) {
	r, _ := harness(t)

	for _, args := range []string{
		`{"title":"T","say":"Up.","say_if_late":"You should have done it","seconds_from_now":30,"minutes_from_now":20}`,
		`{"title":"T","say":"Up.","say_if_late":"You should have done it","seconds_from_now":30,"at":"2026-09-26 16:30"}`,
		`{"title":"T","say":"Up.","say_if_late":"You should have done it","minutes_from_now":20,"at":"2026-09-26 16:30"}`,
	} {
		if got := call(t, r, "reminder_set", args); got.Outcome != conversation.OutcomeFailed {
			t.Errorf("%s was accepted", args)
		}
	}
}

// rang : A reminder in the store that has already gone off.
func rang(t *testing.T, store *inmemory.Store, title, body string, repeats remind.Repeat) *remind.Reminder {
	t.Helper()
	ctx := context.Background()

	r, err := remind.New(user, client, remind.ScopeUser, title, body, noon.Add(-time.Minute), repeats)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("Create: %v", err)
	}

	next := time.Time{}
	if repeats != remind.Once {
		next, _ = remind.Next(r.DueAt, repeats, noon, india)
	}
	if err := store.Fired(ctx, r.ID, noon.Add(-time.Minute), next); err != nil {
		t.Fatalf("Fired: %v", err)
	}
	return r
}

// "Snooze that", with nothing else said, is the one just spoken and ten
// minutes. Both are worked out here: neither is the model's to guess.
func TestSnoozeThatMeansTheOneJustSaid(t *testing.T) {
	r, store := harness(t)
	existing := rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)

	got := call(t, r, "reminder_snooze", `{}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_snooze: %s", got.Content)
	}

	after, err := store.Get(context.Background(), user, existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != remind.Pending {
		t.Errorf("status = %q, want pending", after.Status)
	}
	if want := noon.Add(reminders.DefaultSnooze); !after.DueAt.Equal(want.UTC()) {
		t.Errorf("due at %v, want %v", after.DueAt, want.UTC())
	}
}

// A length said is the length used.
func TestSnoozeTakesALengthOfTime(t *testing.T) {
	r, store := harness(t)
	existing := rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)

	if got := call(t, r, "reminder_snooze", `{"minutes_from_now":25}`); got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_snooze: %s", got.Content)
	}

	after, err := store.Get(context.Background(), user, existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := noon.Add(25 * time.Minute); !after.DueAt.Equal(want.UTC()) {
		t.Errorf("due at %v, want %v", after.DueAt, want.UTC())
	}
}

// Nothing said aloud recently means "that" points at nothing. Picking
// something anyway would put off a reminder nobody mentioned.
func TestSnoozeWithNothingSaidRefuses(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_snooze", `{}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatalf("it put something off with nothing to point at: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Ask which") {
		t.Errorf("the refusal does not say to ask: %s", got.Content)
	}
}

// Two said together is the case that must be asked about rather than
// answered. Guessing here puts off the wrong one and says it did the right.
func TestSnoozeAsksWhichWhenTwoWereSaid(t *testing.T) {
	r, store := harness(t)
	rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)
	rang(t, store, "Mum", "Call your mother.", remind.Once)

	got := call(t, r, "reminder_snooze", `{}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatalf("it chose between two: %s", got.Content)
	}
	for _, want := range []string{"Ask which", "Tablets", "Mum"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the refusal is missing %q: %s", want, got.Content)
		}
	}
}

// One still to come is pushed back by identifier: "move my four o'clock".
func TestOneStillToComeIsPushedBack(t *testing.T) {
	r, store := harness(t)
	existing, err := remind.New(user, "", remind.ScopeUser, "Dentist", "Time to leave.",
		noon.Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), existing); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := call(t, r, "reminder_snooze", `{"id":"`+existing.ID+`","minutes_from_now":90}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_snooze: %s", got.Content)
	}

	after, err := store.Get(context.Background(), user, existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := noon.Add(90 * time.Minute); !after.DueAt.Equal(want.UTC()) {
		t.Errorf("due at %v, want %v", after.DueAt, want.UTC())
	}
}

// The one that matters. Putting off a daily reminder must not walk the
// series later: ten past seven tomorrow, twenty past the day after.
func TestSnoozingADailyOneLeavesTheSeriesAlone(t *testing.T) {
	r, store := harness(t)
	existing := rang(t, store, "Wake", "It is seven o'clock.", remind.Daily)

	got := call(t, r, "reminder_snooze", `{"minutes_from_now":10}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_snooze: %s", got.Content)
	}

	after, err := store.Get(context.Background(), user, existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	series, _ := remind.Next(existing.DueAt, remind.Daily, noon, india)
	if !after.DueAt.Equal(series) {
		t.Errorf("the daily one moved to %v: it would drift further every day", after.DueAt)
	}

	// And the put-off one exists in its own right.
	all, err := store.List(context.Background(), user, remind.Pending)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var extra int
	for i := range all {
		if all[i].ID != existing.ID && all[i].Repeats == remind.Once {
			extra++
			if want := noon.Add(10 * time.Minute); !all[i].DueAt.Equal(want.UTC()) {
				t.Errorf("the extra one is due at %v, want %v", all[i].DueAt, want.UTC())
			}
		}
	}
	if extra != 1 {
		t.Errorf("got %d one-off reminders, want the one that was put off", extra)
	}
}

// And it says so, rather than reporting a plain success that would have
// the assistant claim the seven o'clock had moved.
func TestSnoozingADailyOneSaysWhatItDid(t *testing.T) {
	r, store := harness(t)
	rang(t, store, "Wake", "It is seven o'clock.", remind.Daily)

	got := call(t, r, "reminder_snooze", `{"minutes_from_now":10}`)
	for _, want := range []string{"is not moved", "unchanged", "extra"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("the result does not say the series was left alone (%q): %s", want, got.Content)
		}
	}
}

// One called off is not put off. It is set again.
func TestACancelledOneIsNotPutOff(t *testing.T) {
	r, store := harness(t)
	existing := rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)
	if err := store.Cancel(context.Background(), user, existing.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	got := call(t, r, "reminder_snooze", `{}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatalf("a cancelled reminder was put off: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Set it again") {
		t.Errorf("the refusal does not say what to do instead: %s", got.Content)
	}
}

// An identifier the model invented is refused before it reaches the store.
func TestAnInventedIdentifierIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_snooze", `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV1"}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatal("a reminder that does not exist was put off")
	}
	if !strings.Contains(got.Content, "List them rather than guessing") {
		t.Errorf("the refusal does not say to list: %s", got.Content)
	}
}

// Somebody else's is not theirs to move.
func TestAnotherPersonsIsNotPutOff(t *testing.T) {
	r, store := harness(t)
	other, err := remind.New("usr_01M3D477HXQ4YNQX7BNXJZZCV9", "", remind.ScopeUser,
		"Theirs", "Not yours.", noon.Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), other); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := call(t, r, "reminder_snooze", `{"id":"`+other.ID+`","minutes_from_now":10}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatal("somebody else's reminder was put off")
	}
}

// missedAt : A reminder whose time passed with nothing able to say it.
func missedAt(t *testing.T, store *inmemory.Store, title, body string, due time.Time) *remind.Reminder {
	t.Helper()
	r, err := remind.New(user, "", remind.ScopeUser, title, body, due, remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Missed(context.Background(), r.ID, noon); err != nil {
		t.Fatalf("Missed: %v", err)
	}
	return r
}

// Walking back in and asking what was missed. The default window is the
// last hour, which is what somebody who has just come back is asking about.
func TestRecentAnswersForTheLastHour(t *testing.T) {
	r, store := harness(t)
	rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)

	got := call(t, r, "reminder_recent", `{}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_recent: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Time to take your tablets.") {
		t.Errorf("the recap leaves out what was said: %s", got.Content)
	}
	if !strings.Contains(got.Content, "the last hour") {
		t.Errorf("the recap does not say what window it covers: %s", got.Content)
	}
}

// The distinction that matters. Being told a thing and never being told it
// are different facts, and running them together tells somebody they heard
// something they did not.
func TestSaidAndNeverSaidAreKeptApart(t *testing.T) {
	r, store := harness(t)
	rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)
	missedAt(t, store, "Bins", "Put the bins out.", noon.Add(-30*time.Minute))

	got := call(t, r, "reminder_recent", `{}`)

	saidAt := strings.Index(got.Content, "Said out loud")
	neverAt := strings.Index(got.Content, "Never said at all")
	if saidAt < 0 || neverAt < 0 {
		t.Fatalf("the two are not labelled separately:\n%s", got.Content)
	}
	if !strings.Contains(got.Content, "Put the bins out.") {
		t.Errorf("the missed one is left out: %s", got.Content)
	}
	if saidAt > neverAt {
		t.Error("what was never said comes before what was, which reads as the main news")
	}
}

// Nothing happened is an answer, not an empty list.
func TestRecentSaysWhenNothingHappened(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_recent", `{}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_recent: %s", got.Content)
	}
	if !strings.Contains(got.Content, "Nothing has been said and nothing was missed") {
		t.Errorf("got %q, want a plain nothing-happened", got.Content)
	}
}

// Older than the window is not in the recap. A miss from yesterday is not
// what somebody who stepped out for ten minutes is asking about.
func TestRecentLeavesOutWhatIsTooOld(t *testing.T) {
	r, store := harness(t)
	missedAt(t, store, "Yesterday", "Something from yesterday.", noon.Add(-26*time.Hour))

	if got := call(t, r, "reminder_recent", `{}`); strings.Contains(got.Content, "yesterday") {
		t.Errorf("the recap reaches too far back: %s", got.Content)
	}
	// But it is there when asked for.
	if got := call(t, r, "reminder_recent", `{"hours":48}`); !strings.Contains(got.Content, "Something from yesterday.") {
		t.Errorf("asking for two days did not reach it: %s", got.Content)
	}
}

// A window nobody means is refused, with what to use instead.
func TestAnAbsurdWindowIsRefused(t *testing.T) {
	r, _ := harness(t)

	got := call(t, r, "reminder_recent", `{"hours":100000}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatal("it accepted a window of years")
	}
	if !strings.Contains(got.Content, "include_finished") {
		t.Errorf("the refusal does not say what to use instead: %s", got.Content)
	}
}

// Renaming, which is what was missing: the assistant could only cancel
// and set a new one, which loses how often it has gone off and gives it
// a different identifier.
func TestAReminderCanBeRenamed(t *testing.T) {
	r, store := harness(t)
	existing, err := remind.New(user, "", remind.ScopeUser, "Thing", "Time to do the thing, sir.",
		noon.Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), existing); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := call(t, r, "reminder_update", `{"id":"`+existing.ID+`","title":"Tablets"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_update: %s", got.Content)
	}

	after, err := store.Get(context.Background(), user, existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Title != "Tablets" {
		t.Errorf("title = %q, want Tablets", after.Title)
	}
	if after.ID != existing.ID {
		t.Error("it got a new identifier, which is what cancel-and-set does")
	}
	if after.Body != existing.Body {
		t.Errorf("what it says changed to %q, and nobody asked", after.Body)
	}
	if !after.DueAt.Equal(existing.DueAt) {
		t.Error("its time moved, and nobody asked")
	}
}

// Moving it, and making it repeat.
func TestAReminderCanBeMovedAndMadeToRepeat(t *testing.T) {
	r, store := harness(t)
	existing, err := remind.New(user, "", remind.ScopeUser, "Wake", "It is seven, sir.",
		noon.Add(time.Hour), remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := store.Create(context.Background(), existing); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got := call(t, r, "reminder_update",
		`{"id":"`+existing.ID+`","at":"2026-09-27 07:00","repeats":"daily"}`)
	if got.Outcome != conversation.OutcomeOK {
		t.Fatalf("reminder_update: %s", got.Content)
	}

	after, _ := store.Get(context.Background(), user, existing.ID)
	if after.Repeats != remind.Daily {
		t.Errorf("repeats = %q, want daily", after.Repeats)
	}
	if after.DueAt.In(india).Hour() != 7 {
		t.Errorf("due at %v, want seven in the morning", after.DueAt.In(india))
	}
}

// Asking for nothing is refused rather than quietly doing nothing.
func TestAnEmptyChangeIsRefused(t *testing.T) {
	r, store := harness(t)
	existing, _ := remind.New(user, "", remind.ScopeUser, "Thing", "Do it, sir.",
		noon.Add(time.Hour), remind.Once)
	_ = store.Create(context.Background(), existing)

	got := call(t, r, "reminder_update", `{"id":"`+existing.ID+`"}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatal("an empty change was accepted")
	}
	if !strings.Contains(got.Content, "Nothing was given to change") {
		t.Errorf("got %q", got.Content)
	}
}

// One already gone is history. Changing it would rewrite what was said.
func TestAFinishedReminderCannotBeChanged(t *testing.T) {
	r, store := harness(t)
	existing := rang(t, store, "Tablets", "Time to take your tablets.", remind.Once)

	got := call(t, r, "reminder_update", `{"id":"`+existing.ID+`","title":"Something else"}`)
	if got.Outcome == conversation.OutcomeOK {
		t.Fatal("a finished reminder was edited")
	}
	if !strings.Contains(got.Content, "already happened") {
		t.Errorf("got %q", got.Content)
	}
}

// A change that would leave it unusable is refused before it is written.
func TestAChangeThatEmptiesItIsRefused(t *testing.T) {
	r, store := harness(t)
	existing, _ := remind.New(user, "", remind.ScopeUser, "Thing", "Do it, sir.",
		noon.Add(time.Hour), remind.Once)
	_ = store.Create(context.Background(), existing)

	if got := call(t, r, "reminder_update", `{"id":"`+existing.ID+`","say":"   "}`); got.Outcome == conversation.OutcomeOK {
		t.Error("it was left with nothing to say")
	}
	after, _ := store.Get(context.Background(), user, existing.ID)
	if after.Body != "Do it, sir." {
		t.Errorf("body = %q, want it untouched", after.Body)
	}
}

// Somebody else's is not theirs to change.
func TestAnotherPersonsIsNotChanged(t *testing.T) {
	r, store := harness(t)
	other, _ := remind.New("usr_01M3D477HXQ4YNQX7BNXJZZCV9", "", remind.ScopeUser,
		"Theirs", "Not yours.", noon.Add(time.Hour), remind.Once)
	_ = store.Create(context.Background(), other)

	if got := call(t, r, "reminder_update", `{"id":"`+other.ID+`","title":"Mine now"}`); got.Outcome == conversation.OutcomeOK {
		t.Fatal("somebody else's reminder was renamed")
	}
}

// alreadyListed : Every domain's listings, so a write under test is not
// refused for want of a read it is not testing.
var alreadyListed = []string{"memory_list", "memory_search", "conversation_list",
	"conversation_find", "calendar_events", "calendar_calendars", "reminder_list",
	"reminder_recent"}
