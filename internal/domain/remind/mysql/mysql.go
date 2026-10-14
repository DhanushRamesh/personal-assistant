// Package mysql stores reminders in MySQL.
package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/domain/remind"
)

// row : The reminders table, as GORM sees it.
//
// Kept separate from remind.Reminder so the domain type carries no
// persistence tags. The timestamps disable GORM's automatic ones, which
// would otherwise overwrite what the domain recorded.
type row struct {
	ID           string     `gorm:"column:id;primaryKey"`
	UserID       string     `gorm:"column:user_id"`
	ClientID     *string    `gorm:"column:client_id"`
	Scope        string     `gorm:"column:scope"`
	Title        string     `gorm:"column:title"`
	Body         string     `gorm:"column:body"`
	SaidLate     *string    `gorm:"column:said_late"`
	DueAt        time.Time  `gorm:"column:due_at"`
	Repeats      *string    `gorm:"column:repeats"`
	Status       string     `gorm:"column:status"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	AlarmAskedAt *time.Time `gorm:"column:alarm_asked_at"`
	LastFiredAt  *time.Time `gorm:"column:last_fired_at"`
	MentionedAt  *time.Time `gorm:"column:mentioned_at"`
	Fires        int        `gorm:"column:fires"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "reminders" }

// toReminder : Converts a stored row back into a reminder.
func (r *row) toReminder() remind.Reminder {
	out := remind.Reminder{
		ID:        r.ID,
		UserID:    r.UserID,
		ClientID:  value(r.ClientID),
		Scope:     remind.Scope(r.Scope),
		Title:     r.Title,
		Body:      r.Body,
		SaidLate:  value(r.SaidLate),
		DueAt:     r.DueAt.UTC(),
		Repeats:   remind.Repeat(value(r.Repeats)),
		Status:    remind.Status(r.Status),
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
		Fires:     r.Fires,
	}
	if r.LastFiredAt != nil {
		at := r.LastFiredAt.UTC()
		out.LastFiredAt = &at
	}
	if r.MentionedAt != nil {
		at := r.MentionedAt.UTC()
		out.MentionedAt = &at
	}
	if r.AlarmAskedAt != nil {
		at := r.AlarmAskedAt.UTC()
		out.AlarmAskedAt = &at
	}
	return out
}

// toRow : Converts a reminder into the row that stores it.
func toRow(r *remind.Reminder) *row {
	return &row{
		ID:           r.ID,
		UserID:       r.UserID,
		ClientID:     nullable(r.ClientID),
		Scope:        string(r.Scope),
		Title:        r.Title,
		Body:         r.Body,
		SaidLate:     nullable(r.SaidLate),
		DueAt:        r.DueAt.UTC(),
		Repeats:      nullable(string(r.Repeats)),
		Status:       string(r.Status),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		LastFiredAt:  r.LastFiredAt,
		MentionedAt:  r.MentionedAt,
		AlarmAskedAt: r.AlarmAskedAt,
		Fires:        r.Fires,
	}
}

// nullable : An empty string as a null column.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// value : A null column as an empty string.
func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Store : A remind.Store backed by MySQL.
type Store struct {
	db      *gorm.DB
	changed func(userID string)
}

// New : Returns a Store reading and writing through db.
func New(db *storage.DB) *Store { return &Store{db: db.DB} }

// Create : Stores a new reminder.
func (s *Store) create(ctx context.Context, r *remind.Reminder) error {
	if err := r.Valid(); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(toRow(r)).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("remind: %s already exists", r.ID)
		}
		return fmt.Errorf("remind: creating %s: %w", r.ID, err)
	}
	return nil
}

// Get : Returns one reminder, or remind.ErrNotFound.
func (s *Store) Get(ctx context.Context, userID, id string) (*remind.Reminder, error) {
	var got row
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Take(&got).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, remind.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("remind: reading %s: %w", id, err)
	}
	out := got.toReminder()
	return &out, nil
}

// List : A person's reminders in the given states, soonest first.
func (s *Store) List(ctx context.Context, userID string, states ...remind.Status) ([]remind.Reminder, error) {
	query := s.db.WithContext(ctx).Model(&row{}).Where("user_id = ?", userID)
	if len(states) > 0 {
		wanted := make([]string, 0, len(states))
		for _, state := range states {
			wanted = append(wanted, string(state))
		}
		query = query.Where("status IN ?", wanted)
	}

	var rows []row
	if err := query.Order("due_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("remind: listing: %w", err)
	}
	return toReminders(rows), nil
}

// Cancel : Calls one off. Cancelling one that has already happened is not
// an error: the caller wanted it not to happen, and it will not.
func (s *Store) cancel(ctx context.Context, userID, id string) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{
			"status":     string(remind.Cancelled),
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: cancelling %s: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Due : Pending reminders whose time has come, soonest first.
func (s *Store) Due(ctx context.Context, at time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	var rows []row
	err := s.db.WithContext(ctx).
		Where("status = ? AND due_at <= ?", string(remind.Pending), at.UTC()).
		Order("due_at ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what is due: %w", err)
	}
	return toReminders(rows), nil
}

// Fired : Records that a reminder was said. A zero next finishes it.
func (s *Store) fired(ctx context.Context, id string, at, next time.Time) error {
	changes := map[string]any{
		"last_fired_at": at.UTC(),
		"fires":         gorm.Expr("fires + 1"),
		"updated_at":    at.UTC(),
	}
	if next.IsZero() {
		changes["status"] = string(remind.Done)
	} else {
		changes["due_at"] = next.UTC()
	}

	// Pending or held, and nothing else: pending is the firing loop
	// saying it, held is a delivery to somebody who has just come back.
	// Both are one-way, so two passes still cannot both claim one.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status IN ?", id, []string{string(remind.Pending), string(remind.Held)}).
		Updates(changes)
	if out.Error != nil {
		return fmt.Errorf("remind: recording that %s fired: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Missed : Records that a reminder's time passed with nothing listening.
func (s *Store) missed(ctx context.Context, id string, at time.Time) error {
	// Pending or held. Pending is its time passing with nothing able to
	// say it; held is the firing loop giving up on one that waited too
	// long for somebody to come back. Both end the same way.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status IN ?", id, []string{string(remind.Pending), string(remind.Held)}).
		Updates(map[string]any{
			"status":     string(remind.Missed),
			"updated_at": at.UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: recording that %s was missed: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Unmentioned : Missed reminders the person has not been told about.
func (s *Store) Unmentioned(ctx context.Context, userID string) ([]remind.Reminder, error) {
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND mentioned_at IS NULL", userID, string(remind.Missed)).
		Order("due_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what was missed: %w", err)
	}
	return toReminders(rows), nil
}

// Mentioned : Records that a miss has been brought up.
func (s *Store) mentioned(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("id IN ? AND mentioned_at IS NULL", ids).
		Update("mentioned_at", at.UTC()).Error
	if err != nil {
		return fmt.Errorf("remind: recording that a miss was mentioned: %w", err)
	}
	return nil
}

// Armed : Records that the phone was asked for these alarms.
func (s *Store) armed(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := s.db.WithContext(ctx).Model(&row{}).
		Where("id IN ? AND alarm_asked_at IS NULL", ids).
		Update("alarm_asked_at", at.UTC()).Error
	if err != nil {
		return fmt.Errorf("remind: recording that an alarm was asked for: %w", err)
	}
	return nil
}

// Hold : Keeps a reminder back because nobody was there to hear it.
func (s *Store) hold(ctx context.Context, id string, at time.Time) error {
	// Only while pending, so the firing loop cannot hold one it has
	// already said, and two passes cannot both claim it.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status = ?", id, string(remind.Pending)).
		Updates(map[string]any{
			"status":     string(remind.Held),
			"updated_at": at.UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: holding %s back: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Stale : Reminders held back since before the given moment.
func (s *Store) Stale(ctx context.Context, before time.Time, limit int) ([]remind.Reminder, error) {
	if limit <= 0 {
		limit = remind.DefaultDueLimit
	}

	var rows []row
	// Judged on when it was due, not when it was held: a reminder for
	// ten o'clock is stale at eleven either way.
	err := s.db.WithContext(ctx).
		Where("status = ? AND due_at < ?", string(remind.Held), before.UTC()).
		Order("due_at ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what has been held too long: %w", err)
	}
	return toReminders(rows), nil
}

// Waiting : Reminders held back for somebody, oldest first.
func (s *Store) Waiting(ctx context.Context, userID string) ([]remind.Reminder, error) {
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND status = ?", userID, string(remind.Held)).
		Order("due_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what is being held: %w", err)
	}
	return toReminders(rows), nil
}

// Replace : Writes a changed reminder over the stored one.
func (s *Store) replace(ctx context.Context, userID string, r *remind.Reminder, was remind.Status) error {
	if r == nil {
		return remind.ErrNotFound
	}
	if err := r.Valid(); err != nil {
		return err
	}

	// Named columns, so that nothing this does not mean to touch is
	// written: not the identifier, not who owns it, not how many times
	// it has fired, and not when it last did.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ? AND status = ?", r.ID, userID, string(was)).
		Updates(map[string]any{
			"title":      r.Title,
			"body":       r.Body,
			"said_late":  nullable(r.SaidLate),
			"due_at":     r.DueAt.UTC(),
			"repeats":    nullable(string(r.Repeats)),
			"updated_at": r.UpdatedAt.UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: changing %s: %w", r.ID, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// Snooze : Puts a reminder off until a later time.
func (s *Store) snooze(ctx context.Context, userID, id string, until time.Time) error {
	if until.IsZero() {
		return remind.ErrNoTime
	}

	// Read first, so that a refusal can say which reason it was rather
	// than reporting no such reminder for a repeating one.
	r, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := remind.Snoozable(r); err != nil {
		return err
	}

	// Still in the state it was read in, so a firing that landed in
	// between is not undone.
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ? AND status = ?", id, userID, string(r.Status)).
		Updates(map[string]any{
			"due_at":     until.UTC(),
			"status":     string(remind.Pending),
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: putting %s off: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// LastSpoken : What the person was told since the given moment.
func (s *Store) LastSpoken(ctx context.Context, userID string, since time.Time) ([]remind.Reminder, error) {
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND last_fired_at IS NOT NULL AND last_fired_at >= ?", userID, since.UTC()).
		Order("last_fired_at DESC, id DESC").
		Limit(remind.DefaultSpokenLimit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("remind: reading what was just said: %w", err)
	}
	return toReminders(rows), nil
}

// Reschedule : Moves a reminder to its next time without saying it.
func (s *Store) reschedule(ctx context.Context, id string, next time.Time) error {
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND status = ?", id, string(remind.Pending)).
		Updates(map[string]any{
			"due_at":     next.UTC(),
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("remind: rescheduling %s: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return remind.ErrNotFound
	}
	return nil
}

// toReminders : Converts stored rows back into reminders.
func toReminders(rows []row) []remind.Reminder {
	out := make([]remind.Reminder, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toReminder())
	}
	return out
}

// Ensure the store satisfies the interface it exists to provide.
var _ remind.Store = (*Store)(nil)

// Create : See create; the change hook is told when it succeeds.
func (s *Store) Create(ctx context.Context, r *remind.Reminder) error {
	err := s.create(ctx, r)
	s.tell(err, r.UserID)
	return err
}

// Cancel : See cancel; the change hook is told when it succeeds.
func (s *Store) Cancel(ctx context.Context, userID, id string) error {
	err := s.cancel(ctx, userID, id)
	s.tell(err, userID)
	return err
}

// Fired : See fired; the change hook is told when it succeeds.
func (s *Store) Fired(ctx context.Context, id string, at, next time.Time) error {
	err := s.fired(ctx, id, at, next)
	s.tell(err, "")
	return err
}

// Missed : See missed; the change hook is told when it succeeds.
func (s *Store) Missed(ctx context.Context, id string, at time.Time) error {
	err := s.missed(ctx, id, at)
	s.tell(err, "")
	return err
}

// Mentioned : See mentioned; the change hook is told when it succeeds.
func (s *Store) Mentioned(ctx context.Context, ids []string, at time.Time) error {
	err := s.mentioned(ctx, ids, at)
	s.tell(err, "")
	return err
}

// Armed : See armed; the change hook is told when it succeeds.
func (s *Store) Armed(ctx context.Context, ids []string, at time.Time) error {
	err := s.armed(ctx, ids, at)
	s.tell(err, "")
	return err
}

// Hold : See hold; the change hook is told when it succeeds.
func (s *Store) Hold(ctx context.Context, id string, at time.Time) error {
	err := s.hold(ctx, id, at)
	s.tell(err, "")
	return err
}

// Replace : See replace; the change hook is told when it succeeds.
func (s *Store) Replace(ctx context.Context, userID string, r *remind.Reminder, was remind.Status) error {
	err := s.replace(ctx, userID, r, was)
	s.tell(err, userID)
	return err
}

// Snooze : See snooze; the change hook is told when it succeeds.
func (s *Store) Snooze(ctx context.Context, userID, id string, until time.Time) error {
	err := s.snooze(ctx, userID, id, until)
	s.tell(err, userID)
	return err
}

// Reschedule : See reschedule; the change hook is told when it succeeds.
func (s *Store) Reschedule(ctx context.Context, id string, next time.Time) error {
	err := s.reschedule(ctx, id, next)
	s.tell(err, "")
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
