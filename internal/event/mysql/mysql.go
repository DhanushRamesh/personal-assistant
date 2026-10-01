// Package mysql stores events in MySQL.
package mysql

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// row : The events table, as GORM sees it.
//
// Kept separate from event.Event so the domain type carries no
// persistence tags.
type row struct {
	ID         string    `gorm:"column:id;primaryKey"`
	UserID     string    `gorm:"column:user_id"`
	Source     string    `gorm:"column:source"`
	Device     string    `gorm:"column:device"`
	Kind       string    `gorm:"column:kind"`
	OccurredAt time.Time `gorm:"column:occurred_at"`
	ReceivedAt time.Time `gorm:"column:received_at"`
	Payload    string    `gorm:"column:payload"`
	DedupeKey  string    `gorm:"column:dedupe_key"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "events" }

func toRow(e *event.Event) row {
	return row{
		ID:         e.ID,
		UserID:     e.UserID,
		Source:     e.Source,
		Device:     e.Device,
		Kind:       e.Kind,
		OccurredAt: e.OccurredAt.UTC(),
		ReceivedAt: e.ReceivedAt.UTC(),
		Payload:    string(e.Payload),
		DedupeKey:  e.DedupeKey,
	}
}

// toEvent : Converts a stored row back into an event.
func (r *row) toEvent() event.Event {
	return event.Event{
		ID:         r.ID,
		UserID:     r.UserID,
		Source:     r.Source,
		Device:     r.Device,
		Kind:       r.Kind,
		OccurredAt: r.OccurredAt.UTC(),
		ReceivedAt: r.ReceivedAt.UTC(),
		Payload:    []byte(r.Payload),
		DedupeKey:  r.DedupeKey,
	}
}

// Store : Events, in MySQL.
type Store struct{ db *gorm.DB }

// New : Builds the store.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Record : Stores events that are not stored already, and says which were
// already there.
//
// Two steps rather than one. The insert alone would be enough to keep the
// table correct -- the unique key sees to that -- but it reports a count
// and not which ones, and the phone needs to know exactly what it may
// delete from its spool. Deleting on a count means deleting the wrong
// rows the first time two batches overlap.
//
// So the keys already present are read first and reported, and the insert
// still ignores duplicates. The read can go stale between the two, which
// is the point of leaving the insert able to cope: two batches racing
// produce one row and two honest answers.
func (s *Store) Record(ctx context.Context, userID string, events []*event.Event) (stored, seen []string, err error) {
	if len(events) == 0 {
		return nil, nil, nil
	}

	keys := make([]string, 0, len(events))
	for _, e := range events {
		keys = append(keys, e.DedupeKey)
	}

	var already []string
	if err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ? AND dedupe_key IN ?", userID, keys).
		Pluck("dedupe_key", &already).Error; err != nil {
		return nil, nil, fmt.Errorf("reading which events are known: %w", err)
	}

	known := make(map[string]bool, len(already))
	for _, k := range already {
		known[k] = true
	}

	// Also within the batch itself. A spool flushed twice without being
	// truncated carries the same key twice in one request, and a bulk
	// insert of two identical keys fails as a whole rather than skipping
	// one of them.
	fresh := make([]row, 0, len(events))
	for _, e := range events {
		if known[e.DedupeKey] {
			seen = append(seen, e.DedupeKey)
			continue
		}
		known[e.DedupeKey] = true
		fresh = append(fresh, toRow(e))
		stored = append(stored, e.DedupeKey)
	}

	if len(fresh) == 0 {
		return stored, seen, nil
	}

	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&fresh).Error; err != nil {
		return nil, nil, fmt.Errorf("storing events: %w", err)
	}
	return stored, seen, nil
}

// Recent : What happened, newest first.
//
// Ordered by when it happened rather than when it arrived. A phone that
// spent the morning without a signal delivers the morning at teatime, and
// ordering by arrival would put a whole day in the wrong place.
func (s *Store) Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error) {
	db := s.db.WithContext(ctx).Model(&row{}).Where("user_id = ?", userID)
	if !q.Since.IsZero() {
		db = db.Where("occurred_at >= ?", q.Since.UTC())
	}
	if !q.Until.IsZero() {
		db = db.Where("occurred_at < ?", q.Until.UTC())
	}
	if q.Kind != "" {
		db = db.Where("kind = ?", q.Kind)
	}
	if q.Prefix != "" {
		// Escaped, because a kind is a dotted name and the model writes
		// it: an underscore is a wildcard to LIKE and a legal character
		// in a kind, so "battery_low." unescaped would match things it
		// should not.
		db = db.Where("kind LIKE ?", like(q.Prefix)+"%")
	}
	if q.Contains != "" {
		// The whole payload as text, not one named field. Payloads have
		// no fixed shape -- any device may invent one -- so searching a
		// field this code knows about would silently miss everything
		// written by something newer.
		db = db.Where("CAST(payload AS CHAR) LIKE ?", "%"+like(q.Contains)+"%")
	}
	if q.Limit > 0 {
		db = db.Limit(q.Limit)
	}

	var rows []row
	if err := db.Order("occurred_at desc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading events: %w", err)
	}

	out := make([]event.Event, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toEvent())
	}
	return out, nil
}

// like : A string with LIKE's wildcards made literal.
func like(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

// Kinds : Which kinds exist, with how many of each and when each was last
// seen.
//
// Counted in the database rather than by reading every row: the point of
// this is to describe a table that will grow for years, and a count that
// has to load it to work itself out stops being answerable long before the
// table stops being useful.
func (s *Store) Kinds(ctx context.Context, userID string) ([]event.Kind, error) {
	var rows []struct {
		Kind  string
		N     int64
		First time.Time
		Last  time.Time
	}
	err := s.db.WithContext(ctx).Model(&row{}).
		Select("kind, count(*) as n, min(occurred_at) as first, max(occurred_at) as last").
		Where("user_id = ?", userID).
		Group("kind").Order("n desc").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("counting event kinds: %w", err)
	}

	out := make([]event.Kind, 0, len(rows))
	for _, r := range rows {
		out = append(out, event.Kind{
			Kind: r.Kind, Count: r.N,
			First: r.First.UTC(), Last: r.Last.UTC(),
		})
	}
	return out, nil
}
