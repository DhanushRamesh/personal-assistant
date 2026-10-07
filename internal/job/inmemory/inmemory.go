// Package inmemory keeps jobs in memory, for tests.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
)

// Store : Jobs, in memory.
type Store struct {
	mu   sync.Mutex
	jobs map[string]job.Job
}

// New : An empty store.
func New() *Store { return &Store{jobs: map[string]job.Job{}} }

// Add : Puts one in.
func (s *Store) Add(j job.Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
}

// Keep : Writes one, as the real store does.
func (s *Store) Keep(_ context.Context, j job.Job) error {
	s.Add(j)
	return nil
}

// Mine : This person's rules of a kind, newest first. No states is
// every state, as it is in the real one.
func (s *Store) Mine(_ context.Context, userID, kind string, limit int,
	states ...job.State,
) ([]job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	want := map[job.State]bool{}
	for _, st := range states {
		want[st] = true
	}

	var out []job.Job
	for _, j := range s.jobs {
		if len(want) > 0 && !want[j.State] {
			continue
		}
		if j.UserID == userID && (kind == "" || j.Kind == kind) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID > out[k].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get : One job, for a test to look at how it ended.
func (s *Store) Get(id string) (job.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok
}

// Due : Jobs whose time has come, soonest first.
func (s *Store) Due(_ context.Context, at time.Time, limit int) ([]job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []job.Job
	for _, j := range s.jobs {
		// A job with no next time is not due and never will be:
		// it waits on a reading, not on the clock. The same
		// "next_at IS NOT NULL" the real store asks for.
		if j.State == job.Active && !j.Next.IsZero() && !j.Next.After(at) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Next.Before(out[k].Next) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Watching : This person's active rules that wait for something to
// happen rather than for a time.
func (s *Store) Watching(_ context.Context, userID string, limit int) ([]job.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []job.Job
	for _, j := range s.jobs {
		if j.State == job.Active && j.UserID == userID && job.Watches(j.Schedule) {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Stop : Calls a rule off.
func (s *Store) Stop(_ context.Context, userID, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok || j.UserID != userID || j.State != job.Active {
		return false, nil
	}
	j.State, j.Next = job.Cancelled, time.Time{}
	s.jobs[id] = j
	return true, nil
}

// Ran : Records that a job ran.
func (s *Store) Ran(_ context.Context, id string, next time.Time,
	state job.State, why, said string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return nil
	}
	j.Next, j.State, j.Why, j.Last, j.Said = next, state, why, at, said
	s.jobs[id] = j
	return nil
}
