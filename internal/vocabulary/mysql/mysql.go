// Package mysql stores the names heard in conversation.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/DhanushRamesh/personal-assistant/internal/storage"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// separator : What divides one term from the next in storage.
//
// A newline rather than a comma, because a term may hold a space and
// the files this ends up in are one term per line as well. Nothing
// has to be escaped: a term with a newline in it would not have
// survived vocabulary.Clean.
const separator = "\n"

// row : The vocabularies table, as GORM sees it.
type row struct {
	UserID    string    `gorm:"column:user_id;primaryKey"`
	Terms     string    `gorm:"column:terms"`
	WrittenAt time.Time `gorm:"column:written_at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "vocabularies" }

// Store : The names, in MySQL.
type Store struct {
	db *gorm.DB
}

// New : A store over a database handle.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Put : Replaces one person's list.
//
// Written whole every time. The alternative -- adding what is new and
// leaving the rest -- is how a list of names ends up holding somebody
// who was mentioned once a year ago.
func (s *Store) Put(ctx context.Context, userID string, terms []string, at time.Time) error {
	if userID == "" {
		return fmt.Errorf("vocabulary: no user")
	}
	r := row{
		UserID:    userID,
		Terms:     strings.Join(terms, separator),
		WrittenAt: at.UTC(),
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"terms", "written_at"}),
	}).Create(&r).Error
	if err != nil {
		return fmt.Errorf("vocabulary: storing the names: %w", err)
	}
	return nil
}

// Get : One person's list, empty when they have none.
func (s *Store) Get(ctx context.Context, userID string) ([]string, time.Time, error) {
	var r row
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("vocabulary: reading the names: %w", err)
	}
	return split(r.Terms), r.WrittenAt.UTC(), nil
}

// Everyone : Every list at once, without repeats, oldest write first.
//
// The union rather than one person's, because a microphone cannot
// tell who is about to speak. The time returned is the oldest of the
// writes, so that a list which has partly gone stale reads as stale.
func (s *Store) Everyone(ctx context.Context) ([]string, time.Time, error) {
	var rows []row
	if err := s.db.WithContext(ctx).Order("written_at").Find(&rows).Error; err != nil {
		return nil, time.Time{}, fmt.Errorf("vocabulary: reading the names: %w", err)
	}

	seen := map[string]bool{}
	var terms []string
	var oldest time.Time
	for _, r := range rows {
		if oldest.IsZero() || r.WrittenAt.Before(oldest) {
			oldest = r.WrittenAt
		}
		for _, term := range split(r.Terms) {
			if folded := strings.ToLower(term); !seen[folded] {
				seen[folded] = true
				terms = append(terms, term)
			}
		}
	}
	return terms, oldest.UTC(), nil
}

// split : The stored text back into terms, with blanks dropped.
//
// A person whose list is empty is stored as an empty string, which
// naive splitting would read back as one term that is nothing.
func split(stored string) []string {
	var terms []string
	for _, term := range strings.Split(stored, separator) {
		if term = strings.TrimSpace(term); term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

// Store implements the interface the builder is given.
var _ vocabulary.Store = (*Store)(nil)
