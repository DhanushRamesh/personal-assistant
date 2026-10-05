package alarm_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/alarm"
)

// flaky : A phone that refuses until it is told to stop refusing.
type flaky struct {
	mu      sync.Mutex
	refuses bool
	asked   int
	last    alarm.Alarm
}

func (f *flaky) Set(_ context.Context, a alarm.Alarm) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	f.last = a
	if f.refuses {
		return errors.New("the phone is asleep")
	}
	return nil
}

func (f *flaky) relent() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuses = false
}

func (f *flaky) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked
}

// noon : The moment the clock is held at.
var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// keeper : A Keeper over a flaky phone, with the clock held still.
func keeper(f *flaky) *alarm.Keeper {
	return &alarm.Keeper{
		Setter: f,
		Every:  time.Millisecond,
		Now:    func() time.Time { return noon },
	}
}

// waitFor : Waits briefly for a condition, so a retry loop running
// on its own clock does not need a sleep of a fixed length.
func waitFor(t *testing.T, why string, ok func() bool) {
	t.Helper()
	for range 200 {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", why)
}

// Owner: *"if reaching the phone failed, then it has to be retried
// a few times, before the actual alarm time."* A phone is asleep or
// off the tailnet far more often than it is broken, and all of
// those fix themselves within minutes.
func TestAnAlarmThePhoneRefusedIsTriedAgain(t *testing.T) {
	phone := &flaky{refuses: true}
	k := keeper(phone)

	err := k.Set(context.Background(), alarm.Alarm{
		At: noon.Add(time.Hour), Label: "take the tablets", Key: "rem_1",
	})
	if err == nil {
		t.Fatal("a refusal was reported as an alarm")
	}
	if k.Waiting() != 1 {
		t.Fatalf("%d alarms are owed, wanted 1", k.Waiting())
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go k.Run(ctx)

	waitFor(t, "a second attempt", func() bool { return phone.count() > 1 })
	phone.relent()
	waitFor(t, "the alarm to be owed no longer", func() bool { return k.Waiting() == 0 })

	if phone.last.Label != "take the tablets" {
		t.Errorf("the retry lost the words: %q", phone.last.Label)
	}
}

// An alarm set first time is not kept, or the keeper would grow
// without bound through an ordinary day.
func TestAnAlarmThatWorkedIsNotKept(t *testing.T) {
	k := keeper(&flaky{})

	if err := k.Set(context.Background(), alarm.Alarm{
		At: noon.Add(time.Hour), Key: "rem_1",
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if k.Waiting() != 0 {
		t.Errorf("%d alarms are owed after one that worked", k.Waiting())
	}
}

// Retrying stops at the alarm's own time. An alarm for a moment
// that has passed helps nobody, and one set seconds beforehand is
// worse than useless: it rings while they are still reading that
// it could not be set.
func TestItStopsTryingOnceTheTimeHasCome(t *testing.T) {
	phone := &flaky{refuses: true}
	k := keeper(phone)

	if err := k.Set(context.Background(), alarm.Alarm{
		At: noon.Add(30 * time.Second), Key: "rem_soon",
	}); err == nil {
		t.Fatal("a refusal was reported as an alarm")
	}
	if k.Waiting() != 0 {
		t.Errorf("kept an alarm whose time had effectively come: %d owed", k.Waiting())
	}
}

// A phone that answers and refuses -- a profile switched off, a
// changed secret -- must not be retried every two minutes until
// morning. The clock bounds the common case; this bounds the rest.
func TestItGivesUpAfterEnoughTries(t *testing.T) {
	phone := &flaky{refuses: true}
	k := keeper(phone)
	k.Tries = 3

	if err := k.Set(context.Background(), alarm.Alarm{
		At: noon.Add(24 * time.Hour), Key: "rem_1",
	}); err == nil {
		t.Fatal("a refusal was reported as an alarm")
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go k.Run(ctx)

	waitFor(t, "the keeper to give up", func() bool { return k.Waiting() == 0 })

	if got := phone.count(); got > 5 {
		t.Errorf("the phone was asked %d times for an alarm bounded at 3 retries", got)
	}
}

// One reminder amended twice is owed one alarm, not three.
func TestTheSameReminderIsOwedOneAlarm(t *testing.T) {
	k := keeper(&flaky{refuses: true})

	for _, when := range []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour} {
		_ = k.Set(context.Background(), alarm.Alarm{At: noon.Add(when), Key: "rem_1"})
	}
	if k.Waiting() != 1 {
		t.Errorf("%d alarms are owed for one reminder", k.Waiting())
	}
}
