// Package inmemory keeps model calls in memory, for tests and for a
// server run without a database.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
)

// Store : Calls, in memory.
type Store struct {
	mu      sync.Mutex
	calls   []usage.Call
	changed func(userID string)
}

// New : An empty store.
func New() *Store { return &Store{} }

// OnChange : Has the store say whose calls changed after each one kept.
func (s *Store) OnChange(f func(userID string)) *Store {
	s.changed = f
	return s
}

// Add : See usage.Store.
func (s *Store) Add(_ context.Context, c usage.Call) error {
	c.At = c.At.UTC()
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
	if s.changed != nil && c.UserID != "" {
		s.changed(c.UserID)
	}
	return nil
}

// theirs : Somebody's calls since a moment, in the order kept.
func (s *Store) theirs(userID string, since time.Time) []usage.Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []usage.Call
	for _, c := range s.calls {
		if c.UserID == userID && !c.At.Before(since) {
			out = append(out, c)
		}
	}
	return out
}

// Totals : See usage.Store.
func (s *Store) Totals(_ context.Context, userID string, since time.Time) (usage.Totals, error) {
	var t usage.Totals
	for _, c := range s.theirs(userID, since) {
		t.Add(c)
	}
	return t, nil
}

// ByFeature : See usage.Store.
func (s *Store) ByFeature(_ context.Context, userID string, since time.Time) ([]usage.ByFeature, error) {
	at := map[environment.Feature]int{}
	var out []usage.ByFeature
	for _, c := range s.theirs(userID, since) {
		i, ok := at[c.Feature]
		if !ok {
			i = len(out)
			at[c.Feature] = i
			out = append(out, usage.ByFeature{Feature: c.Feature})
		}
		out[i].Calls++
		out[i].Input += c.Input
		out[i].Output += c.Output
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Input != out[j].Input {
			return out[i].Input > out[j].Input
		}
		return out[i].Feature < out[j].Feature
	})
	return out, nil
}

// ByModel : See usage.Store.
func (s *Store) ByModel(_ context.Context, userID string, since time.Time) ([]usage.ByModel, error) {
	type key struct{ vendor, model string }
	at := map[key]int{}
	var out []usage.ByModel
	for _, c := range s.theirs(userID, since) {
		k := key{c.Vendor, c.Model}
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, usage.ByModel{Vendor: c.Vendor, Model: c.Model})
		}
		out[i].Calls++
		out[i].Input += c.Input
		out[i].Output += c.Output
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Input != out[j].Input {
			return out[i].Input > out[j].Input
		}
		if out[i].Vendor != out[j].Vendor {
			return out[i].Vendor < out[j].Vendor
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// List : See usage.Store.
func (s *Store) List(_ context.Context, q usage.Query) ([]usage.Call, error) {
	mine := s.theirs(q.UserID, q.Since)
	out := make([]usage.Call, 0, len(mine))
	for _, c := range mine {
		if q.Kind.Matches(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
	if limit := usage.Clamp(q.Limit); len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Every : See usage.Store.
func (s *Store) Every(_ context.Context, userID string, since time.Time) ([]usage.Call, error) {
	out := s.theirs(userID, since)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Forget : See usage.Store.
func (s *Store) Forget(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.calls[:0]
	var gone int64
	for _, c := range s.calls {
		if c.At.Before(before) {
			gone++
			continue
		}
		kept = append(kept, c)
	}
	s.calls = kept
	return gone, nil
}

var _ usage.Store = (*Store)(nil)
