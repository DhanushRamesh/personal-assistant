// Package mysql keeps the places a person draws, in MySQL.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/domain/places"
)

// row : The places table, as GORM sees it.
type row struct {
	ID        string    `gorm:"column:id;primaryKey"`
	UserID    string    `gorm:"column:user_id"`
	Name      string    `gorm:"column:name"`
	Lat       float64   `gorm:"column:lat"`
	Lng       float64   `gorm:"column:lng"`
	RadiusM   int       `gorm:"column:radius_m"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "places" }

// seededRow : The places_seeded table, as GORM sees it.
type seededRow struct {
	UserID   string    `gorm:"column:user_id;primaryKey"`
	SeededAt time.Time `gorm:"column:seeded_at"`
}

// TableName : Names the table this row maps to.
func (seededRow) TableName() string { return "places_seeded" }

// Store : Places, in MySQL.
type Store struct {
	db      *gorm.DB
	changed func(userID string)
}

// New : A store over an open database.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// OnChange : Has the store say, after each write that succeeds, whose
// places changed, so the clients showing them can read them again.
func (s *Store) OnChange(f func(userID string)) *Store {
	if s != nil {
		s.changed = f
	}
	return s
}

// List : Everything somebody keeps, by name.
func (s *Store) List(ctx context.Context, userID string) ([]places.Place, error) {
	var rows []row
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("name ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("places: %w", err)
	}
	out := make([]places.Place, 0, len(rows))
	for _, r := range rows {
		out = append(out, assemble(r))
	}
	return out, nil
}

// Get : One of theirs.
func (s *Store) Get(ctx context.Context, userID, id string) (places.Place, error) {
	var r row
	err := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return places.Place{}, places.ErrNotFound
	}
	if err != nil {
		return places.Place{}, fmt.Errorf("places: %w", err)
	}
	return assemble(r), nil
}

// Add : Keeps a new one.
func (s *Store) Add(ctx context.Context, p places.Place) error {
	r := disassemble(p)
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return places.ErrTaken
		}
		return fmt.Errorf("places: %w", err)
	}
	s.tell(p.UserID)
	return nil
}

// Change : Rewrites one of theirs.
func (s *Store) Change(ctx context.Context, p places.Place) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", p.ID, p.UserID).
		Updates(map[string]any{
			"name":       p.Name,
			"lat":        p.Lat,
			"lng":        p.Lng,
			"radius_m":   p.Radius,
			"updated_at": p.UpdatedAt.UTC(),
		})
	if out.Error != nil {
		if errors.Is(out.Error, gorm.ErrDuplicatedKey) {
			return places.ErrTaken
		}
		return fmt.Errorf("places: %w", out.Error)
	}
	if out.RowsAffected == 0 {
		// Nothing matched, or what matched already held exactly this.
		if _, err := s.Get(ctx, p.UserID, p.ID); err != nil {
			return err
		}
	}
	s.tell(p.UserID)
	return nil
}

// Remove : Forgets one of theirs.
func (s *Store) Remove(ctx context.Context, userID, id string) error {
	out := s.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&row{})
	if out.Error != nil {
		return fmt.Errorf("places: %w", out.Error)
	}
	if out.RowsAffected == 0 {
		return places.ErrNotFound
	}
	s.tell(userID)
	return nil
}

// Seeded : Whether Home and Office were made for them.
func (s *Store) Seeded(ctx context.Context, userID string) (bool, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&seededRow{}).
		Where("user_id = ?", userID).Count(&n).Error; err != nil {
		return false, fmt.Errorf("places: %w", err)
	}
	return n > 0, nil
}

// MarkSeeded : Records that they were. Marking twice is harmless.
func (s *Store) MarkSeeded(ctx context.Context, userID string, at time.Time) error {
	r := seededRow{UserID: userID, SeededAt: at.UTC()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&r).Error; err != nil {
		return fmt.Errorf("places: %w", err)
	}
	return nil
}

// tell : Passes a write that succeeded to the change hook.
func (s *Store) tell(userID string) {
	if s != nil && s.changed != nil {
		s.changed(userID)
	}
}

// assemble : A row as a place.
func assemble(r row) places.Place {
	return places.Place{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Lat: r.Lat, Lng: r.Lng,
		Radius: r.RadiusM, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// disassemble : A place as a row.
func disassemble(p places.Place) row {
	return row{
		ID: p.ID, UserID: p.UserID, Name: p.Name, Lat: p.Lat, Lng: p.Lng,
		RadiusM: p.Radius, CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}
