// Package mysql keeps what was decided about each entry in the diary.
package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DhanushRamesh/personal-assistant/internal/coming"
)

// Prefix : What an identifier here begins with.
const Prefix = "cmg_"

// row : The coming table, as GORM sees it.
type row struct {
	ID      string    `gorm:"column:id;primaryKey"`
	UserID  string    `gorm:"column:user_id"`
	EventID string    `gorm:"column:event_id"`
	Shape   string    `gorm:"column:shape"`
	Title   string    `gorm:"column:title"`
	Starts  time.Time `gorm:"column:starts"`
	Leads   string    `gorm:"column:leads"`
	Said    int       `gorm:"column:said"`
	At      time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "coming" }

// lead : One moment, as it is stored.
//
// Seconds rather than a Go duration string, because what is in the
// column should be readable by anything that opens the table.
type lead struct {
	Seconds int64  `json:"seconds"`
	Why     string `json:"why"`
}

// Store : Reads and writes judgements about the diary.
type Store struct{ db *gorm.DB }

// New : Builds one over the given database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// Held : Every judgement for entries that have not started.
func (s *Store) Held(ctx context.Context, userID string, from time.Time) ([]coming.Judged, error) {
	var rows []row
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND starts >= ?", userID, from.UTC()).
		Order("starts").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("coming: reading what was judged: %w", err)
	}

	out := make([]coming.Judged, 0, len(rows))
	for _, r := range rows {
		j, err := r.judged()
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// Keep : Writes a judgement, replacing any for the same entry.
//
// Replacing rather than adding, because a re-judgement is a
// correction and two opinions about one entry would both come due.
// The count of what has been said goes back to zero with it: the
// entry has changed, so the warnings already given were about
// something else.
func (s *Store) Keep(ctx context.Context, j coming.Judged) error {
	leads := make([]lead, 0, len(j.Leads))
	for _, l := range j.Leads {
		leads = append(leads, lead{Seconds: int64(l.Before / time.Second), Why: l.Why})
	}
	encoded, err := json.Marshal(leads)
	if err != nil {
		return fmt.Errorf("coming: writing the moments: %w", err)
	}

	at := j.At
	if at.IsZero() {
		at = time.Now()
	}
	r := row{
		ID: Prefix + ulid.Make().String(), UserID: j.UserID, EventID: j.EventID,
		Shape: j.Shape, Title: j.Title, Starts: j.Starts.UTC(),
		Leads: string(encoded), Said: j.Said, At: at.UTC(),
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "event_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"shape", "title", "starts", "leads", "said", "at"}),
	}).Create(&r).Error; err != nil {
		return fmt.Errorf("coming: keeping a judgement: %w", err)
	}
	return nil
}

// Spoke : Records that one more of an entry's moments was said.
func (s *Store) Spoke(ctx context.Context, userID, eventID string, said int) error {
	if err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ? AND event_id = ?", userID, eventID).
		Update("said", said).Error; err != nil {
		return fmt.Errorf("coming: recording a warning: %w", err)
	}
	return nil
}

// Drop : Forgets judgements for entries no longer in the diary.
//
// Anything that has started is forgotten with them: it has nothing
// left to say, and the table is kept to the size of the window
// rather than growing a row for every appointment the person ever
// had.
func (s *Store) Drop(ctx context.Context, userID string, keep []string) error {
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if len(keep) > 0 {
		q = q.Where("event_id NOT IN ?", keep)
	}
	if err := q.Delete(&row{}).Error; err != nil {
		return fmt.Errorf("coming: forgetting what left the diary: %w", err)
	}
	return nil
}

// judged : One row, as the rest of the server reads it.
func (r row) judged() (coming.Judged, error) {
	var stored []lead
	if r.Leads != "" {
		if err := json.Unmarshal([]byte(r.Leads), &stored); err != nil {
			return coming.Judged{}, fmt.Errorf("coming: reading the moments: %w", err)
		}
	}
	leads := make([]coming.Lead, 0, len(stored))
	for _, l := range stored {
		leads = append(leads, coming.Lead{
			Before: time.Duration(l.Seconds) * time.Second, Why: l.Why,
		})
	}
	return coming.Judged{
		UserID: r.UserID, EventID: r.EventID, Shape: r.Shape,
		Title: r.Title, Starts: r.Starts.UTC(), Leads: leads,
		Said: r.Said, At: r.At.UTC(),
	}, nil
}
