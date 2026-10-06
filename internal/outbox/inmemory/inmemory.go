// Package inmemory keeps what is owed in memory, for tests.
//
// Not for the server. The whole reason the outbox exists is that
// the server may have been off over the moment, and a store that
// forgets on restart is empty at exactly the time it is needed.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/outbox"
)

// Store : What is owed, in memory.
type Store struct {
	mu    sync.Mutex
	owing map[string]outbox.Owing
}

// New : An empty store.
func New() *Store { return &Store{owing: map[string]outbox.Owing{}} }

// Owe : Writes one down.
func (s *Store) Owe(_ context.Context, o outbox.Owing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owing[o.ID] = o
	return nil
}

// Outstanding : Everything still to be delivered, oldest first.
func (s *Store) Outstanding(_ context.Context, _ time.Time, limit int) ([]outbox.Owing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []outbox.Owing
	for _, o := range s.owing {
		if o.State == outbox.Owed || o.State == outbox.Held {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Settle : Records how one ended.
func (s *Store) Settle(_ context.Context, id string, state outbox.State,
	tries int, why string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	o, ok := s.owing[id]
	if !ok {
		return nil
	}
	o.State, o.Tries, o.Why, o.At = state, tries, why, at
	s.owing[id] = o
	return nil
}

// Get : One record, for a test to look at how it ended.
func (s *Store) Get(id string) (outbox.Owing, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.owing[id]
	return o, ok
}
