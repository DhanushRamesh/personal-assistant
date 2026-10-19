// Package mysql keeps model calls in MySQL.
package mysql

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
)

// row : The model_calls table, as GORM sees it.
type row struct {
	ID             string    `gorm:"column:id;primaryKey"`
	UserID         *string   `gorm:"column:user_id"`
	Feature        string    `gorm:"column:feature"`
	Detail         *string   `gorm:"column:detail"`
	Vendor         string    `gorm:"column:vendor"`
	Model          string    `gorm:"column:model"`
	InputTokens    int       `gorm:"column:input_tokens"`
	OutputTokens   int       `gorm:"column:output_tokens"`
	DurationMS     int       `gorm:"column:duration_ms"`
	Ended          string    `gorm:"column:ended"`
	ChatID         *string   `gorm:"column:chat_id"`
	ConversationID *string   `gorm:"column:conversation_id"`
	At             time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "model_calls" }

// Widths of the columns a caller's words go into, so a long rule name
// is cut rather than refused.
const (
	featureWidth = 40
	detailWidth  = 80
	vendorWidth  = 40
	modelWidth   = 80
)

// Store : Model calls, in MySQL.
type Store struct {
	db      *gorm.DB
	changed func(userID string)
}

// New : A store over an open database.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// OnChange : Has the store say whose calls changed after each one kept.
func (s *Store) OnChange(f func(userID string)) *Store {
	if s != nil {
		s.changed = f
	}
	return s
}

// Add : See usage.Store.
func (s *Store) Add(ctx context.Context, c usage.Call) error {
	r := row{
		ID:             c.ID,
		UserID:         nullable(c.UserID, 0),
		Feature:        cut(string(c.Feature), featureWidth),
		Detail:         nullable(c.Detail, detailWidth),
		Vendor:         cut(c.Vendor, vendorWidth),
		Model:          cut(c.Model, modelWidth),
		InputTokens:    c.Input,
		OutputTokens:   c.Output,
		DurationMS:     int(c.Took.Milliseconds()),
		Ended:          string(c.Ended),
		ChatID:         nullable(c.ChatID, 0),
		ConversationID: nullable(c.ConversationID, 0),
		At:             c.At.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return fmt.Errorf("keeping a model call: %w", err)
	}
	if s.changed != nil && c.UserID != "" {
		s.changed(c.UserID)
	}
	return nil
}

// theirs : Somebody's calls since a moment.
func (s *Store) theirs(ctx context.Context, userID string, since time.Time) *gorm.DB {
	return s.db.WithContext(ctx).Model(&row{}).
		Where("user_id = ? AND at >= ?", userID, since.UTC())
}

// Totals : See usage.Store.
func (s *Store) Totals(ctx context.Context, userID string, since time.Time) (usage.Totals, error) {
	var got struct {
		Calls, Input, Output, Chat, Failed int
	}
	err := s.theirs(ctx, userID, since).Select(
		"COUNT(*) AS calls, "+
			"COALESCE(SUM(input_tokens), 0) AS input, "+
			"COALESCE(SUM(output_tokens), 0) AS output, "+
			"COALESCE(SUM(feature IN ?), 0) AS chat, "+
			"COALESCE(SUM(ended = ?), 0) AS failed",
		usage.ChatFeatures(), string(environment.EndedFailed)).
		Scan(&got).Error
	if err != nil {
		return usage.Totals{}, fmt.Errorf("adding up model calls: %w", err)
	}
	return usage.Totals{
		Calls: got.Calls, Input: got.Input, Output: got.Output,
		Chat: got.Chat, Background: got.Calls - got.Chat, Failed: got.Failed,
	}, nil
}

// ByFeature : See usage.Store.
func (s *Store) ByFeature(ctx context.Context, userID string, since time.Time) ([]usage.ByFeature, error) {
	var rows []struct {
		Feature              string
		Calls, Input, Output int
	}
	err := s.theirs(ctx, userID, since).
		Select("feature, COUNT(*) AS calls, SUM(input_tokens) AS input, SUM(output_tokens) AS output").
		Group("feature").Order("input DESC, feature ASC").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("model calls by feature: %w", err)
	}
	out := make([]usage.ByFeature, 0, len(rows))
	for _, r := range rows {
		out = append(out, usage.ByFeature{Feature: environment.Feature(r.Feature),
			Calls: r.Calls, Input: r.Input, Output: r.Output})
	}
	return out, nil
}

// ByModel : See usage.Store.
func (s *Store) ByModel(ctx context.Context, userID string, since time.Time) ([]usage.ByModel, error) {
	var rows []struct {
		Vendor, Model        string
		Calls, Input, Output int
	}
	err := s.theirs(ctx, userID, since).
		Select("vendor, model, COUNT(*) AS calls, SUM(input_tokens) AS input, SUM(output_tokens) AS output").
		Group("vendor, model").Order("input DESC, vendor ASC, model ASC").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("model calls by model: %w", err)
	}
	out := make([]usage.ByModel, 0, len(rows))
	for _, r := range rows {
		out = append(out, usage.ByModel{Vendor: r.Vendor, Model: r.Model,
			Calls: r.Calls, Input: r.Input, Output: r.Output})
	}
	return out, nil
}

// List : See usage.Store.
func (s *Store) List(ctx context.Context, q usage.Query) ([]usage.Call, error) {
	query := s.theirs(ctx, q.UserID, q.Since)
	switch q.Kind {
	case usage.KindChat:
		query = query.Where("feature IN ?", usage.ChatFeatures())
	case usage.KindBackground:
		query = query.Where("feature NOT IN ?", usage.ChatFeatures())
	case usage.KindFailed:
		query = query.Where("ended = ?", string(environment.EndedFailed))
	}
	var rows []row
	if err := query.Order("at DESC, id DESC").Limit(usage.Clamp(q.Limit)).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing model calls: %w", err)
	}
	out := make([]usage.Call, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toCall())
	}
	return out, nil
}

// Every : See usage.Store.
func (s *Store) Every(ctx context.Context, userID string, since time.Time) ([]usage.Call, error) {
	var rows []row
	if err := s.theirs(ctx, userID, since).Order("at, id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading model calls: %w", err)
	}
	out := make([]usage.Call, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toCall())
	}
	return out, nil
}

// Forget : See usage.Store.
func (s *Store) Forget(ctx context.Context, before time.Time) (int64, error) {
	out := s.db.WithContext(ctx).Where("at < ?", before.UTC()).Delete(&row{})
	if out.Error != nil {
		return 0, fmt.Errorf("forgetting old model calls: %w", out.Error)
	}
	return out.RowsAffected, nil
}

// toCall : A row as the domain's shape.
func (r row) toCall() usage.Call {
	return usage.Call{ID: r.ID, Call: environment.Call{
		Label: environment.Label{
			UserID:         value(r.UserID),
			Feature:        environment.Feature(r.Feature),
			Detail:         value(r.Detail),
			ChatID:         value(r.ChatID),
			ConversationID: value(r.ConversationID),
		},
		Vendor: r.Vendor,
		Model:  r.Model,
		Input:  r.InputTokens,
		Output: r.OutputTokens,
		At:     r.At.UTC(),
		Took:   time.Duration(r.DurationMS) * time.Millisecond,
		Ended:  environment.Ended(r.Ended),
	}}
}

// nullable : s as a nullable column, cut to width when width is set.
func nullable(s string, width int) *string {
	if s == "" {
		return nil
	}
	if width > 0 {
		s = cut(s, width)
	}
	return &s
}

// value : A nullable column as a string.
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// cut : s, no longer than width characters.
func cut(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width])
}

var _ usage.Store = (*Store)(nil)
