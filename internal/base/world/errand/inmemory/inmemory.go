// Package inmemory keeps errands in memory, for tests.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"personal-assistant/internal/base/world/errand"
)

// Store : Errands, in memory.
type Store struct {
	mu      sync.Mutex
	errands map[string]errand.Errand
	order   []string
}

// New : An empty store.
func New() *Store { return &Store{errands: map[string]errand.Errand{}} }

// Add : Writes one down.
func (s *Store) Add(_ context.Context, e errand.Errand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, had := s.errands[e.ID]; !had {
		s.order = append(s.order, e.ID)
	}
	if e.State == "" {
		e.State = errand.Waiting
	}
	s.errands[e.ID] = e
	return nil
}

// Due : What is owed and ready, oldest first.
func (s *Store) Due(_ context.Context, at time.Time, limit int) ([]errand.Errand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []errand.Errand
	for _, id := range s.order {
		e := s.errands[id]
		if e.State == errand.Waiting && !e.After.After(at) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, k int) bool { return out[i].After.Before(out[k].After) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Did : Records what came of one attempt.
func (s *Store) Did(_ context.Context, id string, state errand.State,
	tries int, why string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.errands[id]
	if !ok {
		return nil
	}
	e.State, e.Tries, e.Why = state, tries, why
	s.errands[id] = e
	return nil
}

// Drop : Marks what is still waiting about a thing as superseded.
func (s *Store) Drop(_ context.Context, userID, about string, _ time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if about == "" {
		return 0, nil
	}
	var dropped int
	for id, e := range s.errands {
		if e.UserID == userID && e.About == about && e.State == errand.Waiting {
			e.State = errand.Superseded
			s.errands[id] = e
			dropped++
		}
	}
	return dropped, nil
}

// Get : One errand, for a test to look at how it ended.
func (s *Store) Get(id string) (errand.Errand, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.errands[id]
	return e, ok
}

// All : Every errand, in the order they were written.
func (s *Store) All() []errand.Errand {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]errand.Errand, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.errands[id])
	}
	return out
}
