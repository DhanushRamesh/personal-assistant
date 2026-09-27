// Package inmemory keeps a person's Google permission in memory, for
// tests and for a server running without a database.
package inmemory

import (
	"context"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// Store : Permissions held in a map.
type Store struct {
	mu        sync.Mutex
	kept      map[string]google.Account
	calendars map[string]string
}

// New : An empty store.
func New() *Store {
	return &Store{kept: map[string]google.Account{}, calendars: map[string]string{}}
}

// Get : The account linked to this person.
func (s *Store) Get(_ context.Context, userID string) (*google.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, ok := s.kept[userID]
	if !ok {
		return nil, google.ErrNotConnected
	}
	// A copy, so a caller holding it cannot change what is stored.
	a.Scopes = append([]string(nil), a.Scopes...)
	return &a, nil
}

// Put : Stores the permission, replacing any already there.
func (s *Store) Put(_ context.Context, a *google.Account) error {
	if a == nil {
		return google.ErrNotConnected
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := *a
	kept.Scopes = append([]string(nil), a.Scopes...)
	s.kept[a.UserID] = kept
	return nil
}

// Refreshed : Notes that the permission still works, and clears any
// earlier failure.
func (s *Store) Refreshed(_ context.Context, userID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a, ok := s.kept[userID]; ok {
		when := at.UTC()
		a.RefreshedAt = &when
		a.BrokenAt, a.Broken = nil, ""
		s.kept[userID] = a
	}
	return nil
}

// Rotated : Replaces the refresh token.
func (s *Store) Rotated(_ context.Context, userID, refresh string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a, ok := s.kept[userID]; ok {
		when := at.UTC()
		a.Refresh = logging.Secret(refresh)
		a.RefreshedAt = &when
		a.BrokenAt, a.Broken = nil, ""
		s.kept[userID] = a
	}
	return nil
}

// Broke : Records that the permission has stopped working.
func (s *Store) Broke(_ context.Context, userID, why string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a, ok := s.kept[userID]; ok {
		when := at.UTC()
		a.BrokenAt, a.Broken = &when, why
		s.kept[userID] = a
	}
	return nil
}

// Forget : Removes the permission entirely.
func (s *Store) Forget(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.kept, userID)
	return nil
}

// Calendar : Which calendar the assistant made for itself.
func (s *Store) Calendar(_ context.Context, userID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.kept[userID]; !ok {
		return "", google.ErrNotConnected
	}
	return s.calendars[userID], nil
}

// SetCalendar : Records which calendar the assistant made.
func (s *Store) SetCalendar(_ context.Context, userID, calendarID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calendars[userID] = calendarID
	return nil
}

// Store implements the interface it is for.
var _ google.Store = (*Store)(nil)
