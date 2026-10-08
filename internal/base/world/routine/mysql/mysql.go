// Package mysql keeps routine facts in the routines table.
package mysql

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/oklog/ulid/v2"

	"personal-assistant/internal/base/world/routine"
)

// Prefix : What a routine identifier begins with, so one is
// recognisable on sight in a log or a row.
const Prefix = "rtn_"

// row : The routines table, as GORM sees it.
type row struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	Subject   string    `gorm:"column:subject"`
	What      string    `gorm:"column:what"`
	Hour      int       `gorm:"column:hour"`
	Days      int       `gorm:"column:days"`
	Seen      int       `gorm:"column:seen"`
	At        time.Time `gorm:"column:at"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "routines" }

// Store : Routine facts in MySQL.
type Store struct{ db *gorm.DB }

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A fresh identifier.
func ID() string { return Prefix + ulid.Make().String() }

// Keep : Replaces everything known about this person.
//
// In one transaction, and whole. A place somebody stopped going to
// has to stop answering conditions, and merging would leave it
// answering for ever.
func (s *Store) Keep(ctx context.Context, userID string, facts []routine.Fact) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("routine: there is nowhere to keep these")
	}
	if userID == "" {
		return fmt.Errorf("routine: nobody to keep these for")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&row{}).Error; err != nil {
			return fmt.Errorf("routine: clearing: %w", err)
		}
		if len(facts) == 0 {
			return nil
		}
		now := time.Now().UTC()
		rows := make([]row, 0, len(facts))
		for _, f := range facts {
			rows = append(rows, row{
				ID: ID(), UserID: userID, Subject: f.Subject, What: string(f.What),
				Hour: f.Hour, Days: f.Days, Seen: f.Over,
				At: f.At.UTC(), CreatedAt: now, UpdatedAt: now,
			})
		}
		// Two readings of the same place and end in one batch would
		// be two answers to one condition. The last wins, which is
		// the same thing the unique key says.
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&rows).Error; err != nil {
			return fmt.Errorf("routine: keeping: %w", err)
		}
		return nil
	})
}

// All : Everything known about this person.
func (s *Store) All(ctx context.Context, userID string) ([]routine.Fact, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("routine: there is nowhere to read these")
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("subject, what").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("routine: %w", err)
	}
	out := make([]routine.Fact, 0, len(rows))
	for _, r := range rows {
		out = append(out, routine.Fact{
			UserID: r.UserID, Subject: r.Subject, What: routine.What(r.What),
			Hour: r.Hour, Days: r.Days, Over: r.Seen, At: r.At.UTC(),
		})
	}
	return out, nil
}
