package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"personal-assistant/internal/domain/job"
)

// runRow : One run of one rule, as the table holds it.
type runRow struct {
	ID       string    `gorm:"column:id;primaryKey"`
	UserID   string    `gorm:"column:user_id"`
	JobID    string    `gorm:"column:job_id"`
	Name     string    `gorm:"column:name"`
	Schedule string    `gorm:"column:schedule"`
	At       time.Time `gorm:"column:at"`
	TookMS   int       `gorm:"column:took_ms"`
	Outcome  string    `gorm:"column:outcome"`
	Why      string    `gorm:"column:why"`
	Said     *string   `gorm:"column:said"`
	Steps    []byte    `gorm:"column:steps"`
}

func (runRow) TableName() string { return "runs" }

// Runs : Every run of every rule.
//
// Its own store rather than a method on the jobs one, because what
// it holds outlives the rules it is about: a rule rewritten or
// deleted keeps its history, and a row here is never read by the
// engine. Nothing in a run is state.
type Runs struct{ db *gorm.DB }

// NewRuns : The run journal over a database.
func NewRuns(db *gorm.DB) *Runs { return &Runs{db: db} }

// Wrote : Records one run.
//
// Best effort by intention. A journal that could stop a rule from
// running would be a rule that stops working because the thing
// watching it broke, which is the wrong way round -- so this
// returns its error and the runner only logs it.
func (s *Runs) Wrote(ctx context.Context, r job.Record) error {
	steps, err := json.Marshal(r.Steps)
	if err != nil {
		// A trace that will not marshal must not lose the run it
		// describes. The row is worth keeping without it.
		steps = []byte("[]")
	}
	var said *string
	if r.Said != "" {
		said = &r.Said
	}
	row := runRow{
		ID: ID(), UserID: r.UserID, JobID: r.JobID,
		Name: r.Name, Schedule: r.Schedule, At: r.At.UTC(),
		TookMS:  int(r.Took.Milliseconds()),
		Outcome: string(Came(r)), Why: r.Why, Said: said, Steps: steps,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("writing down a run: %w", err)
	}
	return nil
}

// Came : What a run came to, in one word.
//
// The same four words the remarks page uses, so that a person
// reading one and then the other is not learning two vocabularies
// for the same four things.
func Came(r job.Record) string {
	switch {
	case r.Failed:
		return "failed"
	case r.Said != "":
		return "said"
	case r.Stopped:
		return "declined"
	}
	return "held"
}

// Of : What one rule has done lately, newest first.
//
// By job_id and not by name, which is the whole reason the id is
// written down: a rule that has been renamed or rewritten is still
// the same rule, and comparing what it did before a change with
// what it does after is what this table is for.
func (s *Runs) Of(ctx context.Context, jobID string, since time.Time, limit int) ([]job.Record, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var rows []runRow
	q := s.db.WithContext(ctx).Where("job_id = ?", jobID)
	if !since.IsZero() {
		q = q.Where("at >= ?", since.UTC())
	}
	if err := q.Order("at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading what a rule did: %w", err)
	}
	out := make([]job.Record, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toRecord())
	}
	return out, nil
}

// Lately : What every rule has done, newest first. For the page
// that shows the engine working.
func (s *Runs) Lately(ctx context.Context, userID string, since time.Time, limit int) ([]job.Record, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var rows []runRow
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if !since.IsZero() {
		q = q.Where("at >= ?", since.UTC())
	}
	if err := q.Order("at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("reading what the rules did: %w", err)
	}
	out := make([]job.Record, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toRecord())
	}
	return out, nil
}

// Forget : Drops runs older than a moment, and says how many went.
//
// At roughly sixteen hundred a day this is a third of a million
// rows a year, every one of them about a minute nobody will ever
// ask about again. A month is long enough to compare a rule with
// itself before and after a change, which is what it is for.
func (s *Runs) Forget(ctx context.Context, before time.Time) (int64, error) {
	out := s.db.WithContext(ctx).Where("at < ?", before.UTC()).Delete(&runRow{})
	if out.Error != nil {
		return 0, fmt.Errorf("forgetting old runs: %w", out.Error)
	}
	return out.RowsAffected, nil
}

// toRecord : A row as the engine's own shape.
func (r runRow) toRecord() job.Record {
	rec := job.Record{
		UserID: r.UserID, JobID: r.JobID, Name: r.Name,
		Schedule: r.Schedule, At: r.At, Took: time.Duration(r.TookMS) * time.Millisecond,
		Why:     r.Why,
		Stopped: r.Outcome == "declined",
		Failed:  r.Outcome == "failed",
	}
	if r.Said != nil {
		rec.Said = *r.Said
	}
	_ = json.Unmarshal(r.Steps, &rec.Steps)
	return rec
}
