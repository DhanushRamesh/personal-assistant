// Package inmemory keeps places in memory, for tests.
package inmemory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/domain/places"
)

// Store : Places, in memory.
type Store struct {
	mu      sync.Mutex
	places  map[string]places.Place
	seeded  map[string]bool
	changed func(userID string)
}

// New : An empty store.
func New() *Store {
	return &Store{places: map[string]places.Place{}, seeded: map[string]bool{}}
}

// OnChange : Has the store say whose places changed after each write.
func (s *Store) OnChange(f func(userID string)) *Store { s.changed = f; return s }

// List : Everything somebody keeps, by name.
func (s *Store) List(_ context.Context, userID string) ([]places.Place, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []places.Place{}
	for _, p := range s.places {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Get : One of theirs.
func (s *Store) Get(_ context.Context, userID, id string) (places.Place, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.places[id]
	if !ok || p.UserID != userID {
		return places.Place{}, places.ErrNotFound
	}
	return p, nil
}

// Add : Keeps a new one.
func (s *Store) Add(_ context.Context, p places.Place) error {
	s.mu.Lock()
	if s.taken(p) {
		s.mu.Unlock()
		return places.ErrTaken
	}
	s.places[p.ID] = p
	s.mu.Unlock()
	s.tell(p.UserID)
	return nil
}

// Change : Rewrites one of theirs.
func (s *Store) Change(_ context.Context, p places.Place) error {
	s.mu.Lock()
	was, ok := s.places[p.ID]
	if !ok || was.UserID != p.UserID {
		s.mu.Unlock()
		return places.ErrNotFound
	}
	if s.taken(p) {
		s.mu.Unlock()
		return places.ErrTaken
	}
	p.CreatedAt = was.CreatedAt
	s.places[p.ID] = p
	s.mu.Unlock()
	s.tell(p.UserID)
	return nil
}

// Remove : Forgets one of theirs.
func (s *Store) Remove(_ context.Context, userID, id string) error {
	s.mu.Lock()
	p, ok := s.places[id]
	if !ok || p.UserID != userID {
		s.mu.Unlock()
		return places.ErrNotFound
	}
	delete(s.places, id)
	s.mu.Unlock()
	s.tell(userID)
	return nil
}

// Seeded : Whether Home and Office were made for them.
func (s *Store) Seeded(_ context.Context, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seeded[userID], nil
}

// MarkSeeded : Records that they were.
func (s *Store) MarkSeeded(_ context.Context, userID string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seeded[userID] = true
	return nil
}

// taken : Whether another of the same person's places has this name.
// Called with the lock held.
func (s *Store) taken(p places.Place) bool {
	for id, other := range s.places {
		if id != p.ID && other.UserID == p.UserID && places.Same(other.Name, p.Name) {
			return true
		}
	}
	return false
}

// tell : Passes a write to the change hook.
func (s *Store) tell(userID string) {
	if s.changed != nil {
		s.changed(userID)
	}
}
