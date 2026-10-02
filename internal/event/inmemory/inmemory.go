// Package inmemory keeps events in memory, for tests and for a server
// running without a database behind it.
package inmemory

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

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

// Recent : What happened, newest first by when it happened.
func (s *Store) Recent(_ context.Context, userID string, q event.Query) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []event.Event
	for _, e := range s.byUser[userID] {
		switch {
		case !q.Since.IsZero() && e.OccurredAt.Before(q.Since):
			continue
		case !q.Until.IsZero() && !e.OccurredAt.Before(q.Until):
			continue
		case q.Kind != "" && e.Kind != q.Kind:
			continue
		case q.Prefix != "" && !strings.HasPrefix(e.Kind, q.Prefix):
			continue
		case q.Contains != "" && !strings.Contains(
			strings.ToLower(string(e.Payload)), strings.ToLower(q.Contains)):
			continue
		case slices.Contains(q.Omit, e.Kind):
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Forget : Drops every reading of one kind older than a moment, for
// everybody.
func (s *Store) Forget(_ context.Context, kind string, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var gone int64
	for userID, events := range s.byUser {
		kept := events[:0]
		for _, e := range events {
			if e.Kind == kind && e.OccurredAt.Before(before) {
				gone++
				continue
			}
			kept = append(kept, e)
		}
		s.byUser[userID] = kept
	}
	return gone, nil
}

// Kinds : Which kinds exist, with how many of each.
func (s *Store) Kinds(_ context.Context, userID string) ([]event.Kind, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	by := map[string]*event.Kind{}
	for _, e := range s.byUser[userID] {
		k, ok := by[e.Kind]
		if !ok {
			by[e.Kind] = &event.Kind{
				Kind: e.Kind, Count: 1,
				First: e.OccurredAt, Last: e.OccurredAt,
			}
			continue
		}
		k.Count++
		if e.OccurredAt.Before(k.First) {
			k.First = e.OccurredAt
		}
		if e.OccurredAt.After(k.Last) {
			k.Last = e.OccurredAt
		}
	}

	out := make([]event.Kind, 0, len(by))
	for _, k := range by {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}
