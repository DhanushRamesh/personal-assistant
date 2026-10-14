// Package mysql keeps what the assistant considered saying.
package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"personal-assistant/internal/base/world/notice"
)

// row : The remarks table, as GORM sees it.
type row struct {
	ID      string    `gorm:"column:id;primaryKey"`
	UserID  string    `gorm:"column:user_id"`
	At      time.Time `gorm:"column:at"`
	Label   string    `gorm:"column:label"`
	Reason  string    `gorm:"column:reason"`
	Outcome string    `gorm:"column:outcome"`
	Said    *string   `gorm:"column:said"`
	Because string    `gorm:"column:because"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "remarks" }

// Store : Reads and writes what it considered saying.
type Store struct {
	db      *gorm.DB
	changed func(userID string)
}

// New : Builds one over the given database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// Note : Writes down one thing it considered saying.
func (s *Store) note(ctx context.Context, n notice.Noted) error {
	var said *string
	if n.Said != "" {
		kept := n.Said
		said = &kept
	}
	at := n.At
	if at.IsZero() {
		at = time.Now()
	}
	r := row{
		ID: Prefix + ulid.Make().String(), UserID: n.UserID, At: at.UTC(),
		Label: n.Label, Reason: string(n.Reason), Outcome: string(n.Outcome),
		Said: said, Because: n.Because,
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return fmt.Errorf("notice: writing what it considered saying: %w", err)
	}
	return nil
}

// Prefix : What a remark's identifier begins with.
const Prefix = "rmk_"

// Recent : What it has considered saying, newest first.
func (s *Store) Recent(ctx context.Context, userID string, limit int) ([]notice.Noted, error) {
	if limit <= 0 || limit > Most {
		limit = Most
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("notice: reading what it considered saying: %w", err)
	}

	out := make([]notice.Noted, 0, len(rows))
	for _, r := range rows {
		said := ""
		if r.Said != nil {
			said = *r.Said
		}
		out = append(out, notice.Noted{
			UserID: r.UserID, At: r.At.UTC(), Label: r.Label,
			Reason: notice.Reason(r.Reason), Outcome: notice.Outcome(r.Outcome),
			Said: said, Because: r.Because,
		})
	}
	return out, nil
}

// Most : The most rows one read returns. A page, not a history.
const Most = 200

// Note : See note; the change hook is told when it succeeds.
func (s *Store) Note(ctx context.Context, n notice.Noted) error {
	err := s.note(ctx, n)
	s.tell(err, n.UserID)
	return err
}

// OnChange : Has the store say, after each write that succeeds, whose
// data changed -- empty when the write does not know, as one by id
// alone does -- so the clients showing it can read it again. The live
// hub, given it in main.
func (s *Store) OnChange(f func(userID string)) *Store {
	if s != nil {
		s.changed = f
	}
	return s
}

// tell : Passes a write that succeeded to the change hook.
func (s *Store) tell(err error, userID string) {
	if err == nil && s != nil && s.changed != nil {
		s.changed(userID)
	}
}
