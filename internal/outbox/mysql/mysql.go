// Package mysql keeps what the assistant owes somebody to say.
//
// Durable by requirement rather than by preference. The case the
// outbox exists for is the server having been off over the moment,
// and a queue held in memory is empty at exactly the time it is
// needed.
package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/outbox"
)

// Prefix : What an identifier here begins with.
const Prefix = "owe_"

// row : The owed table, as GORM sees it.
type row struct {
	ID      string    `gorm:"column:id;primaryKey"`
	UserID  string    `gorm:"column:user_id"`
	Kind    string    `gorm:"column:kind"`
	Say     string    `gorm:"column:say"`
	Due     time.Time `gorm:"column:due"`
	Expires time.Time `gorm:"column:expires"`
	State   string    `gorm:"column:state"`
	Tries   int       `gorm:"column:tries"`
	Why     string    `gorm:"column:why"`
	At      time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "owed" }

// Store : What is owed, in MySQL.
type Store struct{ db *gorm.DB }

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A new identifier for something owed.
func ID() string { return Prefix + ulid.Make().String() }

// Owe : Writes one down, before anything is said.
func (s *Store) Owe(ctx context.Context, o outbox.Owing) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("outbox: there is nowhere to keep what is owed")
	}
	if o.ID == "" {
		o.ID = ID()
	}
	r := row{
		ID:      o.ID,
		UserID:  o.UserID,
		Kind:    string(o.Kind),
		Say:     clip(o.Text, 2000),
		Due:     o.Due.UTC(),
		Expires: o.Expires.UTC(),
		State:   string(o.State),
		Tries:   o.Tries,
		Why:     clip(o.Why, 500),
		At:      o.At.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	return nil
}

// Outstanding : Everything still to be delivered, oldest first.
//
// Held ones come back as well as owed ones: a thing kept because
// nobody was in the room is still owed, and the only difference is
// that it has been looked at once.
func (s *Store) Outstanding(ctx context.Context, _ time.Time, limit int) ([]outbox.Owing, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("outbox: there is nowhere to read what is owed")
	}
	if limit <= 0 {
		limit = outbox.Limit
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Where("state IN ?", []string{string(outbox.Owed), string(outbox.Held)}).
		Order("due ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}

	out := make([]outbox.Owing, 0, len(rows))
	for _, r := range rows {
		out = append(out, outbox.Owing{
			ID:      r.ID,
			UserID:  r.UserID,
			Kind:    conversation.Kind(r.Kind),
			Text:    r.Say,
			Due:     r.Due.UTC(),
			Expires: r.Expires.UTC(),
			State:   outbox.State(r.State),
			Tries:   r.Tries,
			Why:     r.Why,
			At:      r.At.UTC(),
		})
	}
	return out, nil
}

// Settle : Records how one ended, and why if it went wrong.
func (s *Store) Settle(ctx context.Context, id string, state outbox.State,
	tries int, why string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("outbox: there is nowhere to record what was said")
	}
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"state": string(state),
			"tries": tries,
			"why":   clip(why, 500),
			"at":    at.UTC(),
		}).Error
	if err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	return nil
}

// clip : Keeps a value inside its column.
//
// Truncated rather than refused. Losing the tail of a sentence is
// a worse answer than silence only in theory; losing the
// announcement because the reason it failed was long is worse in
// practice.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
