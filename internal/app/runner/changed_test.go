package runner_test

import (
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// changes : Records every conversation the runner says has changed.
type changes struct {
	mu   sync.Mutex
	seen []string
}

func (c *changes) ConversationsChanged(_, conversationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, conversationID)
}

func (c *changes) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.seen)
}

func TestAConversationIsSaidToChangeWhenATurnStartsAndEnds(t *testing.T) {
	seen := &changes{}
	h := newHarness(t, &environment.Stub{}, runner.Options{Changes: seen})

	tk := h.submit(t, "who are you")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	deadline := time.Now().Add(2 * time.Second)
	for seen.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	seen.mu.Lock()
	defer seen.mu.Unlock()
	if len(seen.seen) < 2 {
		t.Fatalf("said changed %d times, want at least twice: when it started and when it ended", len(seen.seen))
	}
	for _, id := range seen.seen {
		if id != tk.ConversationID {
			t.Errorf("said %q changed, want %q", id, tk.ConversationID)
		}
	}
}
