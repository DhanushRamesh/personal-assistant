package errand_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/base/errand"
	"github.com/DhanushRamesh/personal-assistant/internal/base/errand/inmemory"
)

var noon = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// hand : What one kind does, and what it was given.
type hand struct {
	did     []errand.Errand
	refuses error
}

func (h *hand) do(_ context.Context, e errand.Errand) error {
	h.did = append(h.did, e)
	return h.refuses
}

// one : An errand ready to go, about a thing, expiring well ahead.
func one(id, about string, kind errand.Kind, after time.Time) errand.Errand {
	body, _ := json.Marshal(map[string]string{"label": about})
	made, _ := errand.New(id, "usr_1", kind, about, body, after, noon.Add(time.Hour))
	return made
}

// sender : A sender over a store, with the clock held still.
func sender(s *inmemory.Store, h *hand, now time.Time) *errand.Sender {
	return &errand.Sender{
		Store: s,
		Hands: errand.Hands{errand.AlarmSet: h.do, errand.AlarmClear: h.do},
		Now:   func() time.Time { return now },
	}
}

// Owner: *"isolate the process where something from server to
// phone is happening."* Asking writes a row; the sender gets it
// there.
func TestWhatIsOwedReachesTheDevice(t *testing.T) {
	s, h := inmemory.New(), &hand{}
	run := sender(s, h, noon)
	if err := run.Ask(context.Background(), one("err_1", "rem_1", errand.AlarmSet, noon)); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if got := run.Once(context.Background()); got.Sent != 1 {
		t.Fatalf("pass = %+v, want one sent", got)
	}
	if len(h.did) != 1 || h.did[0].Kind != errand.AlarmSet {
		t.Fatalf("the device got %v", h.did)
	}
	after, _ := s.Get("err_1")
	if after.State != errand.Done {
		t.Errorf("state = %q", after.State)
	}
}

// Taking an alarm back and putting the next one on are two starts
// of the phone's clock activity, and back to back the second is
// dropped. The gap is a timestamp, not a sleep in somebody's turn.
func TestOneWaitsForItsTurn(t *testing.T) {
	s, h := inmemory.New(), &hand{}
	ctx := context.Background()
	run := sender(s, h, noon)
	_ = run.Ask(ctx, one("err_off", "rem_1", errand.AlarmClear, noon))
	_ = run.Ask(ctx, one("err_on", "rem_1", errand.AlarmSet, noon.Add(2*time.Second)))

	if got := run.Once(ctx); got.Sent != 1 {
		t.Fatalf("pass = %+v, want only the first", got)
	}
	if len(h.did) != 1 || h.did[0].Kind != errand.AlarmClear {
		t.Fatalf("the wrong one went first: %v", h.did)
	}

	later := sender(s, h, noon.Add(3*time.Second))
	if got := later.Once(ctx); got.Sent != 1 {
		t.Fatalf("pass = %+v, want the second once its moment came", got)
	}
	if len(h.did) != 2 || h.did[1].Kind != errand.AlarmSet {
		t.Fatalf("the second never went: %v", h.did)
	}
}

// A phone that is asleep costs nothing in the turn that asked, and
// the errand is still owed afterwards.
func TestARefusedErrandIsStillOwed(t *testing.T) {
	s := inmemory.New()
	h := &hand{refuses: errors.New("the phone did not answer")}
	run := sender(s, h, noon)
	_ = run.Ask(context.Background(), one("err_1", "rem_1", errand.AlarmSet, noon))

	if got := run.Once(context.Background()); got.Failed != 1 {
		t.Fatalf("pass = %+v, want one failure", got)
	}
	after, _ := s.Get("err_1")
	if after.State != errand.Waiting {
		t.Errorf("state = %q, want it still owed", after.State)
	}
	if after.Tries != 1 || after.Why == "" {
		t.Errorf("the attempt was not recorded: %+v", after)
	}
}

// Somebody who believes an alarm is coming and finds none was ever
// set is worse off than somebody told. With the asking and the
// getting separated, this is the only place the truth is known.
func TestGivingUpIsSaidOutLoud(t *testing.T) {
	s := inmemory.New()
	h := &hand{refuses: errors.New("the phone did not answer")}
	var said []string
	run := sender(s, h, noon)
	run.Tries = 2
	run.Told = func(_ context.Context, _, text string) { said = append(said, text) }

	ctx := context.Background()
	_ = run.Ask(ctx, one("err_1", "rem_1", errand.AlarmSet, noon))
	run.Once(ctx)
	run.Once(ctx)

	after, _ := s.Get("err_1")
	if after.State != errand.GaveUp {
		t.Fatalf("state = %q, want it given up on", after.State)
	}
	if len(said) != 1 {
		t.Fatalf("it was said %d times, want once", len(said))
	}
}

// An alarm for a moment that has passed helps nobody.
func TestAnErrandStopsBeingWorthDoing(t *testing.T) {
	s, h := inmemory.New(), &hand{}
	body, _ := json.Marshal(map[string]string{})
	e, _ := errand.New("err_1", "usr_1", errand.AlarmSet, "rem_1", body, noon, noon.Add(time.Minute))
	run := sender(s, h, noon.Add(2*time.Minute))
	_ = run.Ask(context.Background(), e)

	if got := run.Once(context.Background()); got.Expired != 1 {
		t.Fatalf("pass = %+v, want it expired", got)
	}
	if len(h.did) != 0 {
		t.Errorf("a stale errand was carried out anyway: %v", h.did)
	}
}

// An alarm queued for a time that has since moved must never
// arrive.
func TestLaterNewsCancelsWhatIsWaiting(t *testing.T) {
	s, h := inmemory.New(), &hand{}
	ctx := context.Background()
	run := sender(s, h, noon)
	_ = run.Ask(ctx, one("err_1", "rem_1", errand.AlarmSet, noon))
	_ = run.Ask(ctx, one("err_2", "rem_other", errand.AlarmSet, noon))

	dropped, err := run.Drop(ctx, "usr_1", "rem_1")
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("%d dropped, want one", dropped)
	}

	if got := run.Once(ctx); got.Sent != 1 {
		t.Fatalf("pass = %+v, want only the other one sent", got)
	}
	if len(h.did) != 1 || h.did[0].About != "rem_other" {
		t.Errorf("the superseded one went anyway: %v", h.did)
	}
}

// A kind nothing carries out would be read and skipped for ever.
func TestAnErrandNobodyCanDoIsEnded(t *testing.T) {
	s, h := inmemory.New(), &hand{}
	run := sender(s, h, noon)
	_ = run.Ask(context.Background(), one("err_1", "rem_1", "nonsense.kind", noon))

	run.Once(context.Background())
	after, _ := s.Get("err_1")
	if after.State != errand.GaveUp {
		t.Errorf("state = %q, want it given up on", after.State)
	}
}

// Whatever is asking is in the middle of answering somebody.
func TestWakeNeverBlocks(t *testing.T) {
	run := &errand.Sender{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < errand.Backlog*3; i++ {
			run.Wake()
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wake blocked with nothing reading the other end")
	}
}
