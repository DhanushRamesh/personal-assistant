// Package mysql keeps what a tool found, out of the conversation.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"personal-assistant/internal/base/platform/storage"

	"personal-assistant/internal/base/records/found"
)

// Prefix : What an identifier here begins with.
//
// Short, because it is written into the conversation and read back out
// of it by a model: every character of it is paid for on every turn
// that carries the line.
const Prefix = "f_"

// row : The found table, as GORM sees it.
type row struct {
	ID             string    `gorm:"column:id;primaryKey"`
	UserID         string    `gorm:"column:user_id"`
	ConversationID string    `gorm:"column:conversation_id"`
	Tool           string    `gorm:"column:tool"`
	Arguments      string    `gorm:"column:arguments"`
	Head           string    `gorm:"column:head"`
	Body           string    `gorm:"column:body"`
	BodyBytes      int       `gorm:"column:body_bytes"`
	Rereads        int       `gorm:"column:rereads"`
	At             time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "found" }

// Store : A found.Store over MySQL.
type Store struct{ db *gorm.DB }

// New : Builds one.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// MaxHead : How long the kept line may be, matching the column.
const MaxHead = 500

// Keep : Writes one result aside, and hands it back with its name.
func (s *Store) Keep(ctx context.Context, f found.Found) (found.Found, error) {
	if f.ID == "" {
		f.ID = Prefix + ulid.Make().String()
	}
	if f.At.IsZero() {
		f.At = time.Now()
	}
	// Cut rather than refused. A head too long is a tool whose first
	// line runs on, which is not a reason to lose the body.
	if len(f.Head) > MaxHead {
		f.Head = strings.TrimSpace(f.Head[:MaxHead-1]) + "…"
	}

	r := row{
		ID: f.ID, UserID: f.UserID, ConversationID: f.ConversationID,
		Tool: f.Tool, Arguments: f.Arguments,
		Head: f.Head, Body: f.Body, BodyBytes: len(f.Body),
		At: f.At.UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return found.Found{}, fmt.Errorf("found: keeping what %s found: %w", f.Tool, err)
	}
	return f, nil
}

// Read : The whole result, counting that it was asked for.
//
// The count is raised before the body is returned and not after, so a
// read that the caller then drops is still counted: what is being
// measured is how often the line was not enough, which is true whether
// or not the answer was used.
func (s *Store) Read(ctx context.Context, userID, id string) (found.Found, error) {
	var r row
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", strings.TrimSpace(id), userID).
		First(&r).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return found.Found{}, found.ErrGone
	case err != nil:
		return found.Found{}, fmt.Errorf("found: reading %s: %w", id, err)
	}

	if err := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", r.ID).
		UpdateColumn("rereads", gorm.Expr("rereads + 1")).Error; err != nil {
		return found.Found{}, fmt.Errorf("found: counting a reread of %s: %w", id, err)
	}

	return found.Found{
		ID: r.ID, UserID: r.UserID, ConversationID: r.ConversationID,
		Tool: r.Tool, Arguments: r.Arguments,
		Head: r.Head, Body: r.Body, Rereads: r.Rereads + 1, At: r.At.UTC(),
	}, nil
}

// Forget : Everything kept for one conversation.
func (s *Store) Forget(ctx context.Context, conversationID string) error {
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Delete(&row{}).Error; err != nil {
		return fmt.Errorf("found: forgetting what was found in %s: %w", conversationID, err)
	}
	return nil
}
