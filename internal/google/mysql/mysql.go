// Package mysql keeps a person's Google permission in the database.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// row : One person's permission, as stored.
type row struct {
	UserID       string     `gorm:"column:user_id;primaryKey"`
	Email        string     `gorm:"column:email"`
	Subject      string     `gorm:"column:subject"`
	RefreshToken string     `gorm:"column:refresh_token"`
	Scopes       string     `gorm:"column:scopes"`
	CalendarID   *string    `gorm:"column:calendar_id"`
	ConnectedAt  time.Time  `gorm:"column:connected_at"`
	RefreshedAt  *time.Time `gorm:"column:refreshed_at"`
	BrokenAt     *time.Time `gorm:"column:broken_at"`
	Broken       *string    `gorm:"column:broken"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "google_accounts" }

// Store : The database-backed store.
type Store struct{ db *gorm.DB }

// New : Builds a store over the given database.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Get : The account linked to this person.
func (s *Store) Get(ctx context.Context, userID string) (*google.Account, error) {
	var r row
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, google.ErrNotConnected
	}
	if err != nil {
		return nil, fmt.Errorf("google: reading the connection: %w", err)
	}
	return r.toAccount(), nil
}

// toAccount : Converts a stored row back.
func (r *row) toAccount() *google.Account {
	out := &google.Account{
		UserID:      r.UserID,
		Email:       r.Email,
		Subject:     r.Subject,
		Refresh:     logging.Secret(r.RefreshToken),
		ConnectedAt: r.ConnectedAt.UTC(),
	}
	if r.Scopes != "" {
		out.Scopes = strings.Fields(r.Scopes)
	}
	if r.RefreshedAt != nil {
		at := r.RefreshedAt.UTC()
		out.RefreshedAt = &at
	}
	if r.BrokenAt != nil {
		at := r.BrokenAt.UTC()
		out.BrokenAt = &at
	}
	if r.Broken != nil {
		out.Broken = *r.Broken
	}
	return out
}

// Put : Stores the permission, replacing any already there.
func (s *Store) Put(ctx context.Context, a *google.Account) error {
	if a == nil {
		return google.ErrNotConnected
	}
	r := row{
		UserID:       a.UserID,
		Email:        a.Email,
		Subject:      a.Subject,
		RefreshToken: a.Refresh.Reveal(),
		Scopes:       strings.Join(a.Scopes, " "),
		ConnectedAt:  a.ConnectedAt.UTC(),
		RefreshedAt:  a.RefreshedAt,
	}
	err := s.db.WithContext(ctx).Save(&r).Error
	if err != nil {
		return fmt.Errorf("google: storing the connection: %w", err)
	}
	return nil
}

// Refreshed : Notes that the permission still works, and clears any
// earlier failure.
func (s *Store) Refreshed(ctx context.Context, userID string, at time.Time) error {
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"refreshed_at": at.UTC(),
			"broken_at":    nil,
			"broken":       nil,
		}).Error
	if err != nil {
		return fmt.Errorf("google: recording a refresh: %w", err)
	}
	return nil
}

// Rotated : Replaces the refresh token with the one Google just issued.
func (s *Store) Rotated(ctx context.Context, userID, refresh string, at time.Time) error {
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"refresh_token": refresh,
			"refreshed_at":  at.UTC(),
			"broken_at":     nil,
			"broken":        nil,
		}).Error
	if err != nil {
		return fmt.Errorf("google: storing a rotated token: %w", err)
	}
	return nil
}

// Broke : Records that the permission has stopped working.
func (s *Store) Broke(ctx context.Context, userID, why string, at time.Time) error {
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{"broken_at": at.UTC(), "broken": why}).Error
	if err != nil {
		return fmt.Errorf("google: recording a lost connection: %w", err)
	}
	return nil
}

// Forget : Removes the permission entirely.
func (s *Store) Forget(ctx context.Context, userID string) error {
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&row{}).Error
	if err != nil {
		return fmt.Errorf("google: forgetting the connection: %w", err)
	}
	return nil
}

// Calendar : Which calendar the assistant made for itself.
func (s *Store) Calendar(ctx context.Context, userID string) (string, error) {
	var r row
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", google.ErrNotConnected
	}
	if err != nil {
		return "", fmt.Errorf("google: reading which calendar is ours: %w", err)
	}
	if r.CalendarID == nil {
		return "", nil
	}
	return *r.CalendarID, nil
}

// SetCalendar : Records which calendar the assistant made.
func (s *Store) SetCalendar(ctx context.Context, userID, calendarID string) error {
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ?", userID).
		Update("calendar_id", calendarID).Error
	if err != nil {
		return fmt.Errorf("google: recording which calendar is ours: %w", err)
	}
	return nil
}

// Store implements the interface it is for.
var _ google.Store = (*Store)(nil)
