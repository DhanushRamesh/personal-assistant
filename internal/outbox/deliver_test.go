package outbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/outbox"
	"github.com/DhanushRamesh/personal-assistant/internal/outbox/inmemory"
)

// noon : The moment the clock is held at.
var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// mouth : Somewhere announcements go, which can be told to refuse.
type mouth struct {
	mu      sync.Mutex
	said    []string
	refuses error
}

func (m *mouth) Say(_ context.Context, _ string, _ conversation.Kind, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refuses != nil {
		return m.refuses
	}
	m.said = append(m.said, text)
	return nil
}

func (m *mouth) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.said)
}

// elsewhere : A presence sensor with a fixed opinion.
type elsewhere struct{ away bool }

func (e elsewhere) Away(context.Context, string) bool { return e.away }

// owe : Writes one thing down, at the given moment.
func owe(t *testing.T, s *inmemory.Store, id string, kind conversation.Kind, at time.Time) {
	t.Helper()
	o, err := outbox.New(id, "usr_1", kind, "something worth hearing", at)
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	if err := s.Owe(context.Background(), o); err != nil {
		t.Fatalf("Owe: %v", err)
	}
}

// deliver : A deliverer over a store, with the clock held still.
func deliver(s *inmemory.Store, m *mouth, now time.Time, p outbox.Presence) *outbox.Deliverer {
	return &outbox.Deliverer{
		Store:    s,
		Speaker:  m,
		Presence: p,
		Now:      func() time.Time { return now },
	}
}

// The ordinary case, and the one that proves the rest mean
// something.
func TestWhatIsOwedIsSaid(t *testing.T) {
	s, m := inmemory.New(), &mouth{}
	owe(t, s, "a", conversation.Immediate, noon)

	if got := deliver(s, m, noon, nil).Once(context.Background()); got.Said != 1 {
		t.Fatalf("pass = %+v, want it said", got)
	}
	if o, _ := s.Get("a"); o.State != outbox.Said {
		t.Errorf("state = %q, want said", o.State)
	}
}

// Owner: *"what if an event is not announced due to any issue, like
// the server is off."* The server comes back owing something, and
// the answer is not to announce it whenever it happens to return.
func TestSomethingTooOldIsNotSaidAtAll(t *testing.T) {
	s, m := inmemory.New(), &mouth{}
	owe(t, s, "a", conversation.Immediate, noon)

	// Immediate is worth five minutes. An hour later the server is
	// back and the moment is long gone.
	got := deliver(s, m, noon.Add(time.Hour), nil).Once(context.Background())

	if got.Expired != 1 {
		t.Fatalf("pass = %+v, want it expired", got)
	}
	if m.count() != 0 {
		t.Errorf("a stale announcement was said anyway")
	}
	o, _ := s.Get("a")
	if o.State != outbox.Expired {
		t.Errorf("state = %q, want expired", o.State)
	}
	if o.Why == "" {
		t.Error("nothing records why it was never heard")
	}
}

// Expiry is fixed when the thing was true, not when the server
// came back. Measured from the return it would never expire at
// all, which is the bug this is most likely to grow.
func TestItsMomentIsMeasuredFromWhenItWasTrue(t *testing.T) {
	o, err := outbox.New("a", "usr_1", conversation.Immediate, "x", noon)
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	if !o.Expires.Equal(noon.Add(5 * time.Minute)) {
		t.Errorf("expires at %s, not five minutes after it was true", o.Expires)
	}
}

// A reminder is still true later, so an empty room keeps it.
func TestSomethingWorthKeepingIsHeld(t *testing.T) {
	s, m := inmemory.New(), &mouth{}
	owe(t, s, "a", conversation.ReminderAnnouncement, noon)

	out := elsewhere{away: true}
	if got := deliver(s, m, noon, out).Once(context.Background()); got.Held != 1 {
		t.Fatalf("pass = %+v, want it held", got)
	}
	if m.count() != 0 {
		t.Errorf("it was said to an empty room")
	}

	// They come back inside the hour it stays worth saying.
	if got := deliver(s, m, noon.Add(time.Minute), elsewhere{}).Once(context.Background()); got.Said != 1 {
		t.Fatalf("pass = %+v, want it said on their return", got)
	}
}

// "Welcome back" kept until morning is not a greeting, and a
// device that said to announce something at once meant at once.
func TestSomethingOnlyTrueNowIsDropped(t *testing.T) {
	s, m := inmemory.New(), &mouth{}
	owe(t, s, "a", conversation.Immediate, noon)

	got := deliver(s, m, noon, elsewhere{away: true}).Once(context.Background())

	if got.Dropped != 1 {
		t.Fatalf("pass = %+v, want it dropped", got)
	}
	if o, _ := s.Get("a"); o.State != outbox.Dropped {
		t.Errorf("state = %q, want dropped", o.State)
	}
}

// Owner: *"or the phone is off."* Speaking can fail, and a failure
// that loses the announcement is the thing this replaces.
func TestSayingItCanFailAndBeTriedAgain(t *testing.T) {
	s := inmemory.New()
	m := &mouth{refuses: errors.New("the satellite is not listening")}
	owe(t, s, "a", conversation.ReminderAnnouncement, noon)

	d := deliver(s, m, noon, nil)
	if got := d.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want a failed attempt", got)
	}
	o, _ := s.Get("a")
	if o.State != outbox.Owed {
		t.Fatalf("state = %q, want it still owed", o.State)
	}
	if o.Tries != 1 {
		t.Errorf("tries = %d, want the attempt counted", o.Tries)
	}
	if o.Why == "" {
		t.Error("nothing records what went wrong")
	}

	m.mu.Lock()
	m.refuses = nil
	m.mu.Unlock()
	if got := d.Once(context.Background()); got.Said != 1 {
		t.Fatalf("pass = %+v, want it said once the way was clear", got)
	}
}

// Trying for ever is its own failure. The clock bounds the common
// case; the count bounds a thing that refuses every time.
func TestItGivesUpAfterEnoughTries(t *testing.T) {
	s := inmemory.New()
	m := &mouth{refuses: errors.New("nothing is listening")}
	owe(t, s, "a", conversation.ReminderAnnouncement, noon)

	d := deliver(s, m, noon, nil)
	for range 10 {
		d.Once(context.Background())
	}
	o, _ := s.Get("a")
	if o.State != outbox.Dropped {
		t.Errorf("state = %q after ten passes, want it given up on", o.State)
	}
	if o.Tries > 5 {
		t.Errorf("tries = %d, want no more than the five its kind allows", o.Tries)
	}
}

// A reply is not a thing to deliver later, and the outbox refuses
// it at the door rather than at delivery.
func TestOnlyAnnouncementsMayBeOwed(t *testing.T) {
	if _, err := outbox.New("a", "usr_1", conversation.Chat, "hello", noon); err == nil {
		t.Error("a plain reply was accepted as something to announce")
	}
	if _, err := outbox.New("a", "usr_1", conversation.Immediate, "", noon); err == nil {
		t.Error("an announcement with nothing to say was accepted")
	}
}

// Every kind that can be announced has to have a policy, or it
// reaches the deliverer and nothing knows what to do with it.
func TestEveryAnnouncementHasARule(t *testing.T) {
	for _, k := range []conversation.Kind{
		conversation.ReminderAnnouncement,
		conversation.PresenceAnnouncement,
		conversation.Remark,
		conversation.Immediate,
		conversation.Coming,
		conversation.Renaming,
	} {
		rule, ok := k.Rule()
		if !ok {
			t.Errorf("%s has no policy", k)
			continue
		}
		if rule.Worth <= 0 {
			t.Errorf("%s is worth saying for no time at all", k)
		}
		if rule.Tries <= 0 {
			t.Errorf("%s is never tried", k)
		}
	}
}
