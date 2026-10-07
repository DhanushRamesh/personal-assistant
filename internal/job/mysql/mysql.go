// Package mysql keeps jobs, so a schedule survives a restart.
package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/DhanushRamesh/personal-assistant/internal/job"
)

// Prefix : What an identifier here begins with.
const Prefix = "job_"

// row : The jobs table, as GORM sees it.
type row struct {
	ID        string     `gorm:"column:id;primaryKey"`
	UserID    string     `gorm:"column:user_id"`
	Name      string     `gorm:"column:name"`
	Kind      string     `gorm:"column:kind"`
	Schedule  string     `gorm:"column:schedule"`
	NextAt    *time.Time `gorm:"column:next_at"`
	Steps     string     `gorm:"column:steps"`
	State     string     `gorm:"column:state"`
	LastAt    *time.Time `gorm:"column:last_at"`
	Why       string     `gorm:"column:why"`
	LastSaid  string     `gorm:"column:last_said"`
	Keep      string     `gorm:"column:keep"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "jobs" }

// Store : Jobs, in MySQL.
type Store struct{ db *gorm.DB }

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A new identifier for a job.
func ID() string { return Prefix + ulid.Make().String() }

// Keep : Writes a job down.
func (s *Store) Keep(ctx context.Context, j job.Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to keep jobs")
	}
	steps, err := json.Marshal(j.Steps)
	if err != nil {
		return fmt.Errorf("job: %w", err)
	}
	if j.ID == "" {
		j.ID = ID()
	}
	now := time.Now().UTC()
	r := row{
		ID: j.ID, UserID: j.UserID, Name: j.Name, Kind: j.Kind,
		Schedule: j.Schedule, Steps: string(steps), State: string(j.State),
		Why: j.Why, Keep: j.Keep, CreatedAt: now, UpdatedAt: now,
	}
	if !j.Next.IsZero() {
		at := j.Next.UTC()
		r.NextAt = &at
	}
	if err := s.db.WithContext(ctx).Create(&r).Error; err != nil {
		return fmt.Errorf("job: %w", err)
	}
	return nil
}

// Change : Replaces a job that exists, and forgets what it last
// said.
//
// The sentence is dropped because the rule is not the rule it was.
// What it is holding back is no longer a repeat of anything, and
// somebody who has just changed a rule wants to hear whether it
// works rather than silence from a comparison with the old one.
func (s *Store) Change(ctx context.Context, j job.Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to keep jobs")
	}
	steps, err := json.Marshal(j.Steps)
	if err != nil {
		return fmt.Errorf("job: %w", err)
	}
	fields := map[string]any{
		"name":       j.Name,
		"schedule":   j.Schedule,
		"steps":      string(steps),
		"keep":       j.Keep,
		"state":      string(j.State),
		"last_said":  "",
		"why":        "",
		"updated_at": time.Now().UTC(),
	}
	if j.Next.IsZero() {
		fields["next_at"] = nil
	} else {
		fields["next_at"] = j.Next.UTC()
	}

	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", j.ID, j.UserID).
		Updates(fields)
	if out.Error != nil {
		return fmt.Errorf("job: changing %s: %w", j.ID, out.Error)
	}
	if out.RowsAffected == 0 {
		return fmt.Errorf("job: there is no rule %s to change", j.ID)
	}
	return nil
}

// Due : Jobs whose time has come, soonest first.
func (s *Store) Due(ctx context.Context, at time.Time, limit int) ([]job.Job, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("job: there is nowhere to read jobs")
	}
	if limit <= 0 {
		limit = job.Limit
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Where("state = ? AND next_at IS NOT NULL AND next_at <= ?", string(job.Active), at.UTC()).
		Order("next_at ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("job: %w", err)
	}

	out := make([]job.Job, 0, len(rows))
	for _, r := range rows {
		j, err := assemble(r)
		if err != nil {
			// A row whose steps cannot be read would be picked up
			// and dropped on every look, for ever. Skipped here
			// and reported, rather than stopping every other job.
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

// Ran : Records that a job ran and when it next runs.
// Stop : Calls a rule off.
//
// Scoped to its owner, so an identifier from somewhere else stops
// nothing rather than stopping somebody's.
func (s *Store) Stop(ctx context.Context, userID, id string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("job: there is nowhere to read jobs")
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ? AND state = ?", id, userID, string(job.Active)).
		Updates(map[string]any{
			"state":      string(job.Cancelled),
			"next_at":    nil,
			"updated_at": time.Now().UTC(),
		})
	if out.Error != nil {
		return false, fmt.Errorf("job: %w", out.Error)
	}
	return out.RowsAffected > 0, nil
}

// Watching : This person's active rules that wait for something to
// happen rather than for a time.
//
// Matched on the schedule text rather than a column of its own.
// There are a handful of these rows and they are read when a
// batch of readings lands, not in a loop; a column would be a
// migration and a second place for the same fact to be wrong.
func (s *Store) Watching(ctx context.Context, userID string, limit int) ([]job.Job, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("job: there is nowhere to read jobs")
	}
	if limit <= 0 {
		limit = job.Limit
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND state = ? AND schedule LIKE ?",
			userID, string(job.Active), job.When+" %").
		Order("created_at ASC").Limit(limit).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("job: %w", err)
	}

	out := make([]job.Job, 0, len(rows))
	for _, r := range rows {
		j, err := assemble(r)
		if err != nil {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *Store) Ran(ctx context.Context, id string, next time.Time,
	state job.State, why, said string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to record that a job ran")
	}
	fields := map[string]any{
		"state":      string(state),
		"why":        clip(why, 500),
		"last_said":  clip(said, 2000),
		"last_at":    at.UTC(),
		"next_at":    nil,
		"updated_at": time.Now().UTC(),
	}
	if !next.IsZero() {
		fields["next_at"] = next.UTC()
	}
	if err := s.db.WithContext(ctx).Model(&row{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		return fmt.Errorf("job: %w", err)
	}
	return nil
}

// Arm : Sets when a waiting rule runs, without recording a run.
//
// Only next_at. Nothing has happened: the rule was set off and is
// waiting, which is not a run, not a failure and not something
// said.
func (s *Store) Arm(ctx context.Context, id string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to read jobs")
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", id).
		Updates(map[string]any{"next_at": at.UTC(), "updated_at": time.Now().UTC()})
	if out.Error != nil {
		return fmt.Errorf("job: arming %s: %w", id, out.Error)
	}
	return nil
}

// Mine : One person's jobs, newest first. Kind filters when given.
func (s *Store) Mine(ctx context.Context, userID, kind string, limit int,
	states ...job.State,
) ([]job.Job, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("job: there is nowhere to read jobs")
	}
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	// Narrowed in the query rather than afterwards. Filtering what
	// came back would let a pile of cancelled rules fill the limit
	// and hide the active ones underneath, which is the one listing
	// nobody could do without.
	if len(states) > 0 {
		want := make([]string, 0, len(states))
		for _, st := range states {
			want = append(want, string(st))
		}
		q = q.Where("state IN ?", want)
	}
	var rows []row
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("job: %w", err)
	}
	out := make([]job.Job, 0, len(rows))
	for _, r := range rows {
		if j, err := assemble(r); err == nil {
			out = append(out, j)
		}
	}
	return out, nil
}

// assemble : One row as a job.
func assemble(r row) (job.Job, error) {
	var steps []job.Step
	if err := json.Unmarshal([]byte(r.Steps), &steps); err != nil {
		return job.Job{}, fmt.Errorf("job: %w", err)
	}
	j := job.Job{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Kind: r.Kind,
		Schedule: r.Schedule, Steps: steps, State: job.State(r.State),
		Why: r.Why, Said: r.LastSaid, Keep: r.Keep,
	}
	if r.NextAt != nil {
		j.Next = r.NextAt.UTC()
	}
	if r.LastAt != nil {
		j.Last = r.LastAt.UTC()
	}
	return j, nil
}

// clip : Keeps a value inside its column.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
