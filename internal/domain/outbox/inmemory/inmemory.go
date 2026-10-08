// Package inmemory keeps what is owed in memory, for tests.
//
// Not for the server. The whole reason the outbox exists is that
// the server may have been off over the moment, and a store that
// forgets on restart is empty at exactly the time it is needed.
package inmemory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"personal-assistant/internal/domain/outbox"
)

// Store : What is owed, in memory.
type Store struct {
	mu        sync.Mutex
	announced map[string]outbox.Owing
	copies    []outbox.Delivery
}

// New : An empty store.
func New() *Store { return &Store{announced: map[string]outbox.Owing{}} }

// Publish : Writes an announcement down and gives every named
// client its own copy.
func (s *Store) Publish(_ context.Context, o outbox.Owing, clients []string) error {
	if len(clients) == 0 {
		return fmt.Errorf("outbox: nothing is subscribed to announcements")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.announced[o.ID] = o
	for _, c := range clients {
		s.copies = append(s.copies, outbox.Delivery{
			ID:       o.ID + ":" + c,
			Owing:    o,
			ClientID: c,
			State:    outbox.Pending,
			At:       o.At,
		})
	}
	return nil
}

// Pending : What one client still owes somebody, oldest first.
func (s *Store) Pending(_ context.Context, clientID string, limit int) ([]outbox.Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []outbox.Delivery
	for _, d := range s.copies {
		if d.ClientID == clientID && d.State == outbox.Pending {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Owing.Due.Before(out[j].Owing.Due) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Mark : Records what one client did about one announcement.
func (s *Store) Mark(_ context.Context, clientID, owedID string,
	state outbox.State, tries int, why string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.copies {
		if s.copies[i].ClientID != clientID || s.copies[i].Owing.ID != owedID {
			continue
		}
		s.copies[i].State, s.copies[i].Tries = state, tries
		s.copies[i].Why, s.copies[i].At = why, at
		return nil
	}
	return nil
}

// Copies : Every client's copy, for a test to look at how each
// one ended.
func (s *Store) Copies() []outbox.Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]outbox.Delivery(nil), s.copies...)
}
