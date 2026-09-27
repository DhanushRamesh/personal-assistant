// Package inmemory : Holds reminders in memory rather than a database.
//
// It exists so the firing loop can be exercised without MySQL, and it
// behaves as the MySQL store does wherever the difference would let a bug
// through: ownership is enforced, listings come back soonest first, and
// firing something that is no longer pending is refused.
//
// It is not durable and is not meant to be.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// Store : An in-memory remind.Store.
type Store struct {
	mu   sync.Mutex
	kept map[string]remind.Reminder
}

// New : An empty Store.
func New() *Store { return &Store{kept: map[string]remind.Reminder{}} }

// Create : Stores a new reminder.
func (s *Store) Create(_ context.Context, r *remind.Reminder) error {
	if err := r.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kept[r.ID] = *r
	return nil
}

// Get : Returns one reminder, or remind.ErrNotFound.
func (s *Store) Get(_ context.Context, userID, id string) (*remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.UserID != userID {
		return nil, remind.ErrNotFound
	}
	return &r, nil
}

// List : A person's reminders in the given states, soonest first.
func (s *Store) List(_ context.Context, userID string, states ...remind.Status) ([]remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wanted := map[remind.Status]bool{}
	for _, state := range states {
		wanted[state] = true
	}

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.UserID != userID || (len(wanted) > 0 && !wanted[r.Status]) {
			continue
		}
		out = append(out, r)
	}
	soonestFirst(out)
	return out, nil
}

// Cancel : Calls one off.
func (s *Store) Cancel(_ context.Context, userID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.UserID != userID {
		return remind.ErrNotFound
	}
	r.Status = remind.Cancelled
	r.UpdatedAt = time.Now().UTC()
	s.kept[id] = r
	return nil
}

// Due : Pending reminders whose time has come, soonest first.
func (s *Store) Due(_ context.Context, at time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.Status == remind.Pending && !r.DueAt.After(at) {
			out = append(out, r)
		}
	}
	soonestFirst(out)

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Fired : Records that a reminder was said. A zero next finishes it.
func (s *Store) Fired(_ context.Context, id string, at, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || (r.Status != remind.Pending && r.Status != remind.Held) {
		return remind.ErrNotFound
	}

	fired := at.UTC()
	r.LastFiredAt, r.Fires, r.UpdatedAt = &fired, r.Fires+1, fired
	if next.IsZero() {
		r.Status = remind.Done
	} else {
		r.DueAt = next.UTC()
	}
	s.kept[id] = r
	return nil
}

// Missed : Records that a reminder's time passed with nothing listening.
func (s *Store) Missed(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || (r.Status != remind.Pending && r.Status != remind.Held) {
		return remind.ErrNotFound
	}
	r.Status, r.UpdatedAt = remind.Missed, at.UTC()
	s.kept[id] = r
	return nil
}

// Unmentioned : Missed reminders the person has not been told about.
func (s *Store) Unmentioned(_ context.Context, userID string) ([]remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.UserID == userID && r.Status == remind.Missed && r.MentionedAt == nil {
			out = append(out, r)
		}
	}
	soonestFirst(out)
	return out, nil
}

// Mentioned : Records that a miss has been brought up.
func (s *Store) Mentioned(_ context.Context, ids []string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	when := at.UTC()
	for _, id := range ids {
		if r, ok := s.kept[id]; ok && r.MentionedAt == nil {
			r.MentionedAt = &when
			s.kept[id] = r
		}
	}
	return nil
}

// Hold : Keeps a reminder back because nobody was there to hear it.
func (s *Store) Hold(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.Status != remind.Pending {
		return remind.ErrNotFound
	}
	r.Status, r.UpdatedAt = remind.Held, at.UTC()
	s.kept[id] = r
	return nil
}

// Stale : Reminders held back since before the given moment.
func (s *Store) Stale(_ context.Context, before time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		// Judged on when it was due, not when it was held. A reminder
		// for ten o'clock is stale at eleven whether it was held at ten
		// or at half past.
		if r.Status == remind.Held && r.DueAt.Before(before) {
			out = append(out, r)
		}
	}
	soonestFirst(out)

	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Waiting : Reminders held back for somebody, oldest first.
func (s *Store) Waiting(_ context.Context, userID string) ([]remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.UserID == userID && r.Status == remind.Held {
			out = append(out, r)
		}
	}
	soonestFirst(out)
	return out, nil
}

// Replace : Writes a changed reminder over the stored one.
func (s *Store) Replace(_ context.Context, userID string, r *remind.Reminder, was remind.Status) error {
	if r == nil {
		return remind.ErrNotFound
	}
	if err := r.Valid(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	held, ok := s.kept[r.ID]
	if !ok || held.UserID != userID || held.Status != was {
		return remind.ErrNotFound
	}
	s.kept[r.ID] = *r
	return nil
}

// Snooze : Puts a reminder off until a later time.
func (s *Store) Snooze(_ context.Context, userID, id string, until time.Time) error {
	if until.IsZero() {
		return remind.ErrNoTime
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.UserID != userID {
		return remind.ErrNotFound
	}
	if err := remind.Snoozable(&r); err != nil {
		return err
	}

	r.DueAt, r.Status, r.UpdatedAt = until.UTC(), remind.Pending, time.Now().UTC()
	s.kept[id] = r
	return nil
}

// LastSpoken : What the person was told since the given moment.
func (s *Store) LastSpoken(_ context.Context, userID string, since time.Time) ([]remind.Reminder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]remind.Reminder, 0, len(s.kept))
	for _, r := range s.kept {
		if r.UserID != userID || r.LastFiredAt == nil || r.LastFiredAt.Before(since.UTC()) {
			continue
		}
		out = append(out, r)
	}
	newestFirst(out)

	if len(out) > remind.DefaultSpokenLimit {
		out = out[:remind.DefaultSpokenLimit]
	}
	return out, nil
}

// Reschedule : Moves a reminder to its next time without saying it.
func (s *Store) Reschedule(_ context.Context, id string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, ok := s.kept[id]
	if !ok || r.Status != remind.Pending {
		return remind.ErrNotFound
	}
	r.DueAt, r.UpdatedAt = next.UTC(), time.Now().UTC()
	s.kept[id] = r
	return nil
}

// soonestFirst : Orders reminders as the MySQL store does, so a test
// cannot pass here and fail there.
func soonestFirst(r []remind.Reminder) {
	sort.SliceStable(r, func(i, j int) bool {
		if !r[i].DueAt.Equal(r[j].DueAt) {
			return r[i].DueAt.Before(r[j].DueAt)
		}
		return r[i].ID < r[j].ID
	})
}

// newestFirst : Orders by when they were said, most recent first, as the
// MySQL store does.
func newestFirst(r []remind.Reminder) {
	sort.SliceStable(r, func(i, j int) bool {
		if !r[i].LastFiredAt.Equal(*r[j].LastFiredAt) {
			return r[i].LastFiredAt.After(*r[j].LastFiredAt)
		}
		return r[i].ID > r[j].ID
	})
}

// Ensure the in-memory store satisfies the interface it stands in for.
var _ remind.Store = (*Store)(nil)
