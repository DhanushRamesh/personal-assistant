// Package inmemory keeps events in memory, for tests and for a server
// running without a database behind it.
package inmemory

import (
	"context"
	"sync"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// Store : Events, in memory.
type Store struct {
	mu sync.Mutex
	// byUser : Everything stored, in the order it was stored.
	byUser map[string][]*event.Event
	// keys : Which deduplication keys each person has already sent.
	keys map[string]map[string]bool
}

// New : An empty store.
func New() *Store {
	return &Store{
		byUser: make(map[string][]*event.Event),
		keys:   make(map[string]map[string]bool),
	}
}

// Record : Stores what is not stored already, and says which keys were
// already known.
func (s *Store) Record(_ context.Context, userID string, events []*event.Event) (stored, seen []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.keys[userID] == nil {
		s.keys[userID] = make(map[string]bool)
	}
	for _, e := range events {
		if s.keys[userID][e.DedupeKey] {
			seen = append(seen, e.DedupeKey)
			continue
		}
		s.keys[userID][e.DedupeKey] = true
		s.byUser[userID] = append(s.byUser[userID], e)
		stored = append(stored, e.DedupeKey)
	}
	return stored, seen, nil
}

// All : Everything stored for one person, in the order it was stored.
func (s *Store) All(userID string) []*event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*event.Event(nil), s.byUser[userID]...)
}
