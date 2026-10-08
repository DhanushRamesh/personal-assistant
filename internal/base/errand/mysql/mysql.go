// Package mysql keeps errands, so what is owed to a device survives
// a restart.
//
// The retry queue this replaces was a map in memory. A restart
// emptied it without a word, including the alarms somebody had
// already been told would keep being tried.
package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/base/errand"
)

// Prefix : What an identifier here begins with.
const Prefix = "err_"

// row : The errands table, as GORM sees it.
type row struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	Kind      string    `gorm:"column:kind"`
	About     string    `gorm:"column:about"`
	Body      string    `gorm:"column:body"`
	AfterAt   time.Time `gorm:"column:after_at"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	State     string    `gorm:"column:state"`
	Tries     int       `gorm:"column:tries"`
	Why       string    `gorm:"column:why"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "errands" }

// Store : Errands, in MySQL.
type Store struct{ db *gorm.DB }

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A new identifier for an errand.
func ID() string { return Prefix + ulid.Make().String() }

// Add : Writes one down.
func (s *Store) Add(ctx context.Context, e errand.Errand) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("errand: there is nowhere to keep errands")
	}
	body := string(e.Body)
	if body == "" {
		body = "{}"
	}
	now := time.Now().UTC()
	r := row{
		ID: e.ID, UserID: e.UserID, Kind: string(e.Kind), About: e.About,
		Body: body, AfterAt: e.After.UTC(), ExpiresAt: e.Expires.UTC(),
		State: string(e.State), Tries: e.Tries, Why: clip(e.Why, 500),
		CreatedAt: now, UpdatedAt: now,
	}
	if r.State == "" {
		r.State = string(errand.Waiting)
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return fmt.Errorf("errand: %w", err)
	}
	return nil
}

// Due : What is owed and ready, oldest first.
func (s *Store) Due(ctx context.Context, at time.Time, limit int) ([]errand.Errand, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("errand: there is nowhere to read errands")
	}
	if limit <= 0 {
		limit = errand.Limit
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Where("state = ? AND after_at <= ?", string(errand.Waiting), at.UTC()).
		Order("after_at ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("errand: %w", err)
	}

	out := make([]errand.Errand, 0, len(rows))
	for _, r := range rows {
		out = append(out, assemble(r))
	}
	return out, nil
}

// Did : Records what came of one attempt.
func (s *Store) Did(ctx context.Context, id string, state errand.State,
	tries int, why string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("errand: there is nowhere to record errands")
	}
	err := s.db.WithContext(ctx).Model(&row{}).Where("id = ?", id).
		Updates(map[string]any{
			"state":      string(state),
			"tries":      tries,
			"why":        clip(why, 500),
			"updated_at": at.UTC(),
		}).Error
	if err != nil {
		return fmt.Errorf("errand: %w", err)
	}
	return nil
}

// Drop : Marks what is still waiting about a thing as superseded.
//
// What stops an alarm arriving for a time that has since moved.
func (s *Store) Drop(ctx context.Context, userID, about string, at time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("errand: there is nowhere to read errands")
	}
	if about == "" {
		return 0, nil
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ? AND about = ? AND state = ?", userID, about, string(errand.Waiting)).
		Updates(map[string]any{
			"state":      string(errand.Superseded),
			"updated_at": at.UTC(),
		})
	if out.Error != nil {
		return 0, fmt.Errorf("errand: %w", out.Error)
	}
	return int(out.RowsAffected), nil
}

// assemble : A row as an errand.
func assemble(r row) errand.Errand {
	return errand.Errand{
		ID: r.ID, UserID: r.UserID, Kind: errand.Kind(r.Kind), About: r.About,
		Body: json.RawMessage(r.Body), After: r.AfterAt, Expires: r.ExpiresAt,
		State: errand.State(r.State), Tries: r.Tries, Why: r.Why,
	}
}

// clip : A string cut to what the column holds.
func clip(s string, most int) string {
	if len(s) <= most {
		return s
	}
	return s[:most]
}
