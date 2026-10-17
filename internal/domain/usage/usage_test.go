package usage_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
	"personal-assistant/internal/domain/usage/inmemory"
)

// Chat is the person's turns and what follows from them directly;
// everything else the server asked on its own account. The settings
// page splits on this, so a feature landing on the wrong side moves
// thousands of tokens from one figure to the other.
func TestWhatCountsAsChat(t *testing.T) {
	for _, f := range environment.Features() {
		want := f == environment.FeatureChat || f == environment.FeatureTitle ||
			f == environment.FeatureCondense
		if usage.IsChat(f) != want {
			t.Errorf("IsChat(%s) = %v, want %v", f, !want, want)
		}
	}
	failed := usage.Call{Call: environment.Call{
		Label: environment.Label{Feature: environment.FeatureNoticing},
		Ended: environment.EndedFailed,
	}}
	if !usage.KindFailed.Matches(failed) || !usage.KindBackground.Matches(failed) ||
		usage.KindChat.Matches(failed) || !usage.KindAll.Matches(failed) {
		t.Error("a failed noticing call was sorted wrongly")
	}
	if usage.Kind("costly").Valid() || !usage.KindFailed.Valid() {
		t.Error("kinds are not told apart from nonsense")
	}
}

func TestALimitIsClamped(t *testing.T) {
	for in, want := range map[int]int{0: 100, -3: 100, 7: 7, 500: 500, 501: 500} {
		if got := usage.Clamp(in); got != want {
			t.Errorf("Clamp(%d) = %d, want %d", in, got, want)
		}
	}
}

// failing : A store that refuses every call.
type failing struct{ usage.Store }

func (failing) Add(context.Context, usage.Call) error { return errors.New("disk full") }

// The recorder gives each call an identifier and keeps it; a store that
// refuses is logged and never panics or blocks the call it is about.
func TestTheRecorderKeepsEachCall(t *testing.T) {
	s := inmemory.New()
	r := usage.Recorder{Store: s}
	r.Record(context.Background(), environment.Call{
		Label: environment.Label{UserID: "usr_A", Feature: environment.FeatureChat},
		At:    time.Now(), Ended: environment.EndedAnswered, Input: 7,
	})
	got, _ := s.List(context.Background(), usage.Query{UserID: "usr_A"})
	if len(got) != 1 || len(got[0].ID) != 30 || got[0].Input != 7 {
		t.Fatalf("kept %+v, want one call with a 30 character id", got)
	}
	usage.Recorder{Store: failing{}}.Record(context.Background(), environment.Call{})
	usage.Recorder{}.Record(context.Background(), environment.Call{})
}

// A burst of calls is one notice and then one more when the quiet
// spell ends -- never one per call, and never losing the last.
func TestNoticesAreThrottled(t *testing.T) {
	var mu sync.Mutex
	told := map[string]int{}
	tell := usage.Throttled(80*time.Millisecond, func(userID string) {
		mu.Lock()
		told[userID]++
		mu.Unlock()
	})
	for i := 0; i < 10; i++ {
		tell("usr_A")
	}
	tell("usr_B")

	mu.Lock()
	if told["usr_A"] != 1 || told["usr_B"] != 1 {
		t.Errorf("told %v at once, want one each", told)
	}
	mu.Unlock()

	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if told["usr_A"] != 2 {
		t.Errorf("usr_A told %d times, want the first and the one after the spell", told["usr_A"])
	}
	if told["usr_B"] != 1 {
		t.Errorf("usr_B told %d times, want once", told["usr_B"])
	}
	if usage.Throttled(time.Second, nil) != nil {
		t.Error("nothing to tell should stay nothing")
	}
}
