package memories_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/api/apitest"
	"github.com/DhanushRamesh/personal-assistant/internal/api/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
)

func kept(t *testing.T, s *inmemory.Store, user, tier, subject string, uses int, used *time.Time) {
	t.Helper()
	m, err := memory.New(user, memory.Tier(tier), subject, subject+" body")
	if err != nil {
		t.Fatalf("building a memory: %v", err)
	}
	m.Uses, m.LastUsedAt = uses, used
	if err := s.Create(context.Background(), m); err != nil {
		t.Fatalf("storing a memory: %v", err)
	}
}

func list(t *testing.T, e *apitest.Env) memories.ListResponse {
	t.Helper()
	rec := e.Do(t, http.MethodGet, "/v1/memories", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out memories.ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("reply unreadable: %v", err)
	}
	return out
}

func TestNothingRememberedIsAnEmptyList(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Memories: inmemory.New()})
	if out := list(t, e); out.Total != 0 || len(out.Memories) != 0 {
		t.Fatalf("expected nothing, got %d", out.Total)
	}
}

// The always-on ones are what every single prompt pays for, so they come
// first and are counted separately.
func TestAlwaysOnMemoriesComeFirstAndAreCounted(t *testing.T) {
	s := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Memories: s})
	recent := time.Now().Add(-time.Hour)

	kept(t, s, e.User.ID, "recall", "used recently", 5, &recent)
	kept(t, s, e.User.ID, "always", "in every prompt", 1, &recent)

	out := list(t, e)
	if out.Total != 2 || out.Always != 1 {
		t.Fatalf("total %d always %d, want 2 and 1", out.Total, out.Always)
	}
	if out.Memories[0].Tier != "always" {
		t.Fatalf("first is %q, want the always-on one", out.Memories[0].Tier)
	}
}

// A memory never given to the model is the one worth questioning, so it is
// counted rather than left to be spotted.
func TestUnusedMemoriesAreCounted(t *testing.T) {
	s := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Memories: s})
	used := time.Now()

	kept(t, s, e.User.ID, "recall", "never used", 0, nil)
	kept(t, s, e.User.ID, "recall", "used once", 1, &used)

	if out := list(t, e); out.Unused != 1 {
		t.Fatalf("unused = %d, want 1", out.Unused)
	}
}

// An unembedded memory is still stored and still found by wording, so
// nothing looks broken -- the only sign is that a paraphrase stops finding
// it. Worth stating outright.
func TestWhetherAMemoryIsSearchableIsShown(t *testing.T) {
	s := inmemory.New()
	e := apitest.NewWith(t, apitest.Options{Memories: s})
	kept(t, s, e.User.ID, "recall", "no vector", 0, nil)

	out := list(t, e)
	if len(out.Memories) != 1 {
		t.Fatalf("got %d memories", len(out.Memories))
	}
	if out.Memories[0].Searchable {
		t.Fatal("a memory with no embedding was reported as searchable")
	}
}
