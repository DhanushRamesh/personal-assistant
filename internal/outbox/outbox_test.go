package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/outbox"
	"github.com/DhanushRamesh/personal-assistant/internal/outbox/inmemory"
)

// noon : The moment the clock is held at.
var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// typed, voiced : One client with a screen and one with a
// microphone, which is the whole reason state is per client.
const (
	typed  = "cli_typed"
	voiced = "cli_voiced"
)

// publish : Puts one announcement out to both clients.
func publish(t *testing.T, s *inmemory.Store, id string,
	kind conversation.Kind, at time.Time) outbox.Owing {
	t.Helper()
	o, err := outbox.New(id, "usr_1", kind, "something worth hearing", at)
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	if err := s.Publish(context.Background(), o, []string{typed, voiced}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return o
}

// Owner: *"a announcement has to be published to the client, like a
// pub sub. Each client is subscribed."* One announcement, one copy
// per client, each with its own verdict.
func TestOneAnnouncementReachesEveryClient(t *testing.T) {
	s := inmemory.New()
	publish(t, s, "a", conversation.Immediate, noon)

	for _, client := range []string{typed, voiced} {
		owing, err := s.Pending(context.Background(), client, 0)
		if err != nil {
			t.Fatalf("Pending: %v", err)
		}
		if len(owing) != 1 {
			t.Fatalf("%s has %d pending, wanted 1", client, len(owing))
		}
		if owing[0].Owing.Text != "something worth hearing" {
			t.Errorf("%s was given %q", client, owing[0].Owing.Text)
		}
	}
}

// The whole point of per-client state: a warning read on a laptop
// and missed by a speaker in an empty room is both at once, and a
// single verdict could not say so.
func TestOneClientSayingItLeavesTheOtherOwing(t *testing.T) {
	s := inmemory.New()
	publish(t, s, "a", conversation.Immediate, noon)

	// The typed client has no room to be empty, so it says it the
	// moment it arrives.
	if err := s.Mark(context.Background(), typed, "a", outbox.Said, 1, "", noon); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	left, _ := s.Pending(context.Background(), typed, 0)
	if len(left) != 0 {
		t.Errorf("the typed client still owes %d after saying it", len(left))
	}
	still, _ := s.Pending(context.Background(), voiced, 0)
	if len(still) != 1 {
		t.Fatalf("the voice client owes %d, wanted the announcement still waiting", len(still))
	}
}

// Owner: *"when next time presence is true, the call is made to
// the server and gets the pending announcements."* Nothing was
// written while the room was empty, so it is simply still there.
func TestAVoiceClientCollectsWhatItMissed(t *testing.T) {
	s := inmemory.New()
	publish(t, s, "a", conversation.ReminderAnnouncement, noon)

	// The room was empty. The client said nothing and wrote
	// nothing, which is the default rather than a report.
	owing, _ := s.Pending(context.Background(), voiced, 0)
	if len(owing) != 1 {
		t.Fatalf("owes %d, wanted it still waiting", len(owing))
	}

	// The watch comes back into range.
	if err := s.Mark(context.Background(), voiced, "a", outbox.Said, 1, "", noon.Add(time.Minute)); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if left, _ := s.Pending(context.Background(), voiced, 0); len(left) != 0 {
		t.Errorf("still owes %d after saying it", len(left))
	}
}

// Published to nobody is a watcher shouting into a server with no
// clients. Refused loudly, because the silence that follows should
// have a reason.
func TestPublishingToNobodyIsAnError(t *testing.T) {
	s := inmemory.New()
	o, _ := outbox.New("a", "usr_1", conversation.Immediate, "hello", noon)

	if err := s.Publish(context.Background(), o, nil); err == nil {
		t.Error("an announcement with no audience was accepted")
	}
}

// A voice client switched off for a week must not come back to a
// queue of yesterday. The one question a client cannot answer,
// because it is about a moment rather than a device.
func TestWhatWentStaleIsSweptUp(t *testing.T) {
	s := inmemory.New()
	publish(t, s, "old", conversation.Immediate, noon)
	publish(t, s, "new", conversation.Immediate, noon.Add(time.Hour))

	sweep := &outbox.Sweeper{
		Store: s,
		Clients: func(context.Context) ([]string, error) {
			return []string{typed, voiced}, nil
		},
		// An hour on, the first is long past its five minutes and
		// the second has only just happened.
		Now: func() time.Time { return noon.Add(time.Hour) },
	}
	if closed := sweep.Once(context.Background()); closed != 2 {
		t.Fatalf("closed %d, wanted the stale one for both clients", closed)
	}

	for _, client := range []string{typed, voiced} {
		left, _ := s.Pending(context.Background(), client, 0)
		if len(left) != 1 {
			t.Fatalf("%s owes %d, wanted only the fresh one", client, len(left))
		}
		if left[0].Owing.ID != "new" {
			t.Errorf("%s kept %q, wanted the fresh one", client, left[0].Owing.ID)
		}
	}
	for _, d := range s.Copies() {
		if d.Owing.ID == "old" && d.Why == "" {
			t.Error("nothing records why the stale one was never heard")
		}
	}
}

// Expiry is fixed when the thing was true, not when a client gets
// round to collecting it. Measured from the collection it would
// never expire, which is the bug this is most likely to grow.
func TestItsMomentIsMeasuredFromWhenItWasTrue(t *testing.T) {
	o, err := outbox.New("a", "usr_1", conversation.Immediate, "x", noon)
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	if !o.Expires.Equal(noon.Add(5 * time.Minute)) {
		t.Errorf("expires at %s, not five minutes after it was true", o.Expires)
	}
}

// A reply is not a thing to publish, and it is refused at the door
// rather than at delivery.
func TestOnlyAnnouncementsMayBeOwed(t *testing.T) {
	if _, err := outbox.New("a", "usr_1", conversation.Chat, "hello", noon); err == nil {
		t.Error("a plain reply was accepted as something to announce")
	}
	if _, err := outbox.New("a", "usr_1", conversation.Immediate, "", noon); err == nil {
		t.Error("an announcement with nothing to say was accepted")
	}
}

// Every kind that can be announced has to have a policy, or it
// reaches a client and nothing knows how long it is worth saying.
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
	}
}

// everyone : A person with both clients.
func everyone(context.Context, string) ([]string, error) {
	return []string{typed, voiced}, nil
}

// Owner: *"this event is client specific only, not all clients
// should get that."* A conversation renamed on a laptop is that
// laptop's business. Put on a speaker it is noise about a thing
// nobody in that room did.
func TestSomethingOneClientCausedGoesOnlyToIt(t *testing.T) {
	s := inmemory.New()
	p := &outbox.Publisher{Store: s, Clients: everyone}

	o, err := outbox.New("a", "usr_1", conversation.Renaming, `Named it "Gunalan call check"`, noon)
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	o.From = typed
	if err := p.Publish(context.Background(), o); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	mine, _ := s.Pending(context.Background(), typed, 0)
	if len(mine) != 1 {
		t.Errorf("the client that caused it owes %d, wanted 1", len(mine))
	}
	theirs, _ := s.Pending(context.Background(), voiced, 0)
	if len(theirs) != 0 {
		t.Errorf("another client was given %d copies of somebody else's rename", len(theirs))
	}
}

// A reminder's whole point is to arrive when they are somewhere
// else, so every client gets it whoever caused it.
func TestSomethingAClockCausedGoesEverywhere(t *testing.T) {
	s := inmemory.New()
	p := &outbox.Publisher{Store: s, Clients: everyone}

	o, _ := outbox.New("a", "usr_1", conversation.ReminderAnnouncement, "take the tablets", noon)
	if err := p.Publish(context.Background(), o); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for _, client := range []string{typed, voiced} {
		owing, _ := s.Pending(context.Background(), client, 0)
		if len(owing) != 1 {
			t.Errorf("%s owes %d, wanted the reminder", client, len(owing))
		}
	}
}

// A device event is Asked -- the room rather than the phone -- and
// has no causing client at all. Refusing it silenced every
// situation, so nobody named means everybody until Asked is split
// into the two things it is carrying.
func TestAskedWithNoCauseStillReachesEverybody(t *testing.T) {
	s := inmemory.New()
	p := &outbox.Publisher{Store: s, Clients: everyone}

	o, _ := outbox.New("a", "usr_1", conversation.Immediate, "the charger is doing nothing", noon)
	if err := p.Publish(context.Background(), o); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for _, client := range []string{typed, voiced} {
		owing, _ := s.Pending(context.Background(), client, 0)
		if len(owing) != 1 {
			t.Errorf("%s owes %d, wanted the announcement", client, len(owing))
		}
	}
}
