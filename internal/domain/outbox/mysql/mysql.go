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

	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/domain/outbox"
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
	At      time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "owed" }

// copy : One client's copy of one announcement.
type copyRow struct {
	ID       string    `gorm:"column:id;primaryKey"`
	OwedID   string    `gorm:"column:owed_id"`
	ClientID string    `gorm:"column:client_id"`
	State    string    `gorm:"column:state"`
	Tries    int       `gorm:"column:tries"`
	Why      string    `gorm:"column:why"`
	At       time.Time `gorm:"column:at"`
}

// TableName : Names the table this row maps to.
func (copyRow) TableName() string { return "owed_clients" }

// Store : What is owed, in MySQL.
type Store struct{ db *gorm.DB }

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A new identifier for something owed.
func ID() string { return Prefix + ulid.Make().String() }

// Publish : Writes an announcement down and gives every named
// client its own copy.
//
// One transaction. An announcement with no copies can never be
// delivered or explained, and a copy with no announcement is a row
// pointing at nothing -- neither half is worth having alone.
func (s *Store) Publish(ctx context.Context, o outbox.Owing, clients []string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("outbox: there is nowhere to keep what is owed")
	}
	if len(clients) == 0 {
		// Nobody is subscribed. Said out loud rather than written
		// down and forgotten: an announcement with no audience is
		// a watcher shouting into a server with no clients, and
		// the silence that follows should have a reason.
		return fmt.Errorf("outbox: nothing is subscribed to announcements")
	}
	if o.ID == "" {
		o.ID = ID()
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r := row{
			ID: o.ID, UserID: o.UserID, Kind: string(o.Kind),
			Say: clip(o.Text, 2000), Due: o.Due.UTC(),
			Expires: o.Expires.UTC(), At: o.At.UTC(),
		}
		if err := tx.Create(&r).Error; err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
		copies := make([]copyRow, 0, len(clients))
		for _, c := range clients {
			copies = append(copies, copyRow{
				ID: ID(), OwedID: o.ID, ClientID: c,
				State: string(outbox.Pending), At: o.At.UTC(),
			})
		}
		if err := tx.Create(&copies).Error; err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
		return nil
	})
}

// Pending : What one client still owes somebody, oldest first.
func (s *Store) Pending(ctx context.Context, clientID string, limit int) ([]outbox.Delivery, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("outbox: there is nowhere to read what is owed")
	}
	if limit <= 0 {
		limit = outbox.Limit
	}

	// Flat, and deliberately not the copyRow embedded with the
	// joined columns beside it. GORM's Scan does not look inside an
	// embedded struct without being told to, so every field of the
	// copy came back zero while the joined ones were right: the
	// announcement had its words and no identifier, and a client
	// cannot say it has said a thing it cannot name.
	//
	// Nothing caught it because nothing collected. The endpoint has
	// been answering with blank identifiers since it was written.
	var found []struct {
		ID       string
		OwedID   string
		ClientID string
		State    string
		Tries    int
		Why      string
		At       time.Time
		Kind     string
		Say      string
		UserID   string
		Due      time.Time
		Expires  time.Time
	}
	err := s.db.WithContext(ctx).
		Table("owed_clients").
		Select("owed_clients.id, owed_clients.owed_id, owed_clients.client_id, "+
			"owed_clients.state, owed_clients.tries, owed_clients.why, owed_clients.at, "+
			"owed.kind, owed.say, owed.user_id, owed.due, owed.expires").
		Joins("JOIN owed ON owed.id = owed_clients.owed_id").
		Where("owed_clients.client_id = ? AND owed_clients.state = ?", clientID, string(outbox.Pending)).
		Order("owed.due ASC").
		Limit(limit).
		Scan(&found).Error
	if err != nil {
		return nil, fmt.Errorf("outbox: %w", err)
	}

	out := make([]outbox.Delivery, 0, len(found))
	for _, f := range found {
		out = append(out, outbox.Delivery{
			ID:       f.ID,
			ClientID: f.ClientID,
			State:    outbox.State(f.State),
			Tries:    f.Tries,
			Why:      f.Why,
			At:       f.At.UTC(),
			Owing: outbox.Owing{
				ID:      f.OwedID,
				UserID:  f.UserID,
				Kind:    conversation.Kind(f.Kind),
				Text:    f.Say,
				Due:     f.Due.UTC(),
				Expires: f.Expires.UTC(),
			},
		})
	}
	return out, nil
}

// Mark : Records what one client did about one announcement.
func (s *Store) Mark(ctx context.Context, clientID, owedID string,
	state outbox.State, tries int, why string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("outbox: there is nowhere to record what was said")
	}
	err := s.db.WithContext(ctx).Model(&copyRow{}).
		Where("owed_id = ? AND client_id = ?", owedID, clientID).
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
