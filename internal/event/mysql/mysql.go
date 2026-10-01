// Package mysql stores events in MySQL.
package mysql

import (
	"context"
	"fmt"
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
