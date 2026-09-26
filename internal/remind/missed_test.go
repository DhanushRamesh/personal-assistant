package remind_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
)

// missedOne : A reminder whose time passed with nothing able to say it.
func missedOne(t *testing.T, s *inmemory.Store, title string, due time.Time) *remind.Reminder {
	t.Helper()

	r, err := remind.New("usr_1", "", remind.ScopeUser, title, "time to "+title, due, remind.Once)
	if err != nil {
		t.Fatalf("remind.New: %v", err)
	}
	if err := s.Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Missed(context.Background(), r.ID, time.Now().UTC()); err != nil {
		t.Fatalf("Missed: %v", err)
	}
	return r
}

// Nothing missed says nothing. Almost every turn.
func TestNothingMissedAddsNothing(t *testing.T) {
	m := &remind.Missing{Store: inmemory.New(), Location: india}

	block, covered, err := m.Block(context.Background(), "usr_1")
	if err != nil || block != "" || len(covered) != 0 {
		t.Errorf("Block = %q, %v, %v, want nothing", block, covered, err)
	}
}

// A miss is brought up with what it was and when it was due.
func TestAMissIsBroughtUp(t *testing.T) {
	store := inmemory.New()
	missedOne(t, store, "call the roofer", at(2026, 9, 26, 10, 0))

	m := &remind.Missing{Store: store, Location: india}
	block, covered, err := m.Block(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	if len(covered) != 1 {
		t.Fatalf("covered %d, want 1", len(covered))
	}
	for _, want := range []string{"never said", "10:00 am", "call the roofer", "Begin your reply by telling the person"} {
		if !strings.Contains(block, want) {
			t.Errorf("block is missing %q:\n%s", want, block)
		}
	}
}

// Once, and not on every turn after. Being told every time about a thing
// that did not happen last week is worse than not being told.
func TestAMissIsBroughtUpOnlyOnce(t *testing.T) {
	store := inmemory.New()
	missedOne(t, store, "call the roofer", at(2026, 9, 26, 10, 0))

	m := &remind.Missing{Store: store, Location: india}
	ctx := context.Background()

	block, covered, _ := m.Block(ctx, "usr_1")
	if block == "" {
		t.Fatal("the first turn was told nothing")
	}
	if err := m.Told(ctx, covered); err != nil {
		t.Fatalf("Told: %v", err)
	}

	again, _, _ := m.Block(ctx, "usr_1")
	if again != "" {
		t.Errorf("the next turn was told again:\n%s", again)
	}
}

// Another person's miss is not mentioned to this one.
func TestAnotherPersonsMissIsNotMentioned(t *testing.T) {
	store := inmemory.New()
	missedOne(t, store, "their business", at(2026, 9, 26, 10, 0))

	m := &remind.Missing{Store: store, Location: india}
	if block, _, _ := m.Block(context.Background(), "usr_2"); block != "" {
		t.Errorf("another person's miss was mentioned:\n%s", block)
	}
}

// One that fired, or was called off, is not a miss.
func TestOnlyAMissIsMentioned(t *testing.T) {
	store := inmemory.New()
	ctx := context.Background()

	fired, _ := remind.New("usr_1", "", remind.ScopeUser, "Said", "this was said", at(2026, 9, 26, 10, 0), remind.Once)
	if err := store.Create(ctx, fired); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Fired(ctx, fired.ID, time.Now().UTC(), time.Time{}); err != nil {
		t.Fatalf("Fired: %v", err)
	}

	m := &remind.Missing{Store: store, Location: india}
	if block, _, _ := m.Block(ctx, "usr_1"); block != "" {
		t.Errorf("something that was said was mentioned as missed:\n%s", block)
	}
}

// Told is safe to call with nothing, since almost every turn has nothing.
func TestTellingNobodyAnythingIsFine(t *testing.T) {
	m := &remind.Missing{Store: inmemory.New()}

	if err := m.Told(context.Background(), nil); err != nil {
		t.Errorf("Told(nil): %v", err)
	}
}
