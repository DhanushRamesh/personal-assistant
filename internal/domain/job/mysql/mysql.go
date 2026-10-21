// Package mysql keeps jobs, so a schedule survives a restart.
package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"personal-assistant/internal/domain/job"
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
	Because   string     `gorm:"column:because"`
	LastSaid  string     `gorm:"column:last_said"`
	Keep      string     `gorm:"column:keep"`
	Anywhere  bool       `gorm:"column:anywhere"`
	Cool      string     `gorm:"column:cool"`
	QuietAt   *time.Time `gorm:"column:quiet_until"`
	Resume    int        `gorm:"column:resume_at_step"`
	When      *string    `gorm:"column:when"`
	Perform   *string    `gorm:"column:perform"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

// TableName : Names the table this row maps to.
func (row) TableName() string { return "jobs" }

// Store : Jobs, in MySQL.
type Store struct {
	db      *gorm.DB
	changed func(userID string)
}

// New : A store over an open database.
func New(db *gorm.DB) *Store { return &Store{db: db} }

// ID : A new identifier for a job.
func ID() string { return Prefix + ulid.Make().String() }

// Keep : Writes a job down.
func (s *Store) keep(ctx context.Context, j job.Job) error {
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
	when, perform, err := written(j)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	r := row{
		ID: j.ID, UserID: j.UserID, Name: j.Name, Kind: j.Kind,
		Schedule: j.Schedule, Steps: string(steps), State: string(j.State),
		When: when, Perform: perform,
		Why: j.Why, Because: j.Because, Keep: j.Keep, Cool: j.Cool,
		Anywhere:  j.Anywhere,
		CreatedAt: now, UpdatedAt: now,
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
func (s *Store) change(ctx context.Context, j job.Job) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to keep jobs")
	}
	steps, err := json.Marshal(j.Steps)
	if err != nil {
		return fmt.Errorf("job: %w", err)
	}
	when, perform, err := written(j)
	if err != nil {
		return err
	}
	fields := map[string]any{
		"name":       j.Name,
		"schedule":   j.Schedule,
		"steps":      string(steps),
		"when":       when,
		"perform":    perform,
		"keep":       j.Keep,
		"anywhere":   j.Anywhere,
		"cool":       j.Cool,
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

// Quiet : Puts a rule aside until a moment, or lets it run again.
//
// The zero time clears it. Nothing else about the rule is touched:
// being quiet is not being cancelled, and it has to come back as
// it was.
func (s *Store) quiet(ctx context.Context, userID, id string, until time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to read jobs")
	}
	var when *time.Time
	if !until.IsZero() {
		at := until.UTC()
		when = &at
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"quiet_until": when, "updated_at": time.Now().UTC()})
	if out.Error != nil {
		return fmt.Errorf("job: quieting %s: %w", id, out.Error)
	}
	if out.RowsAffected == 0 {
		return fmt.Errorf("job: there is no rule %s", id)
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
		// A rule being quiet is not read at all, rather than read
		// and then refused: the clock asks this question every two
		// seconds and the ones put aside should cost it nothing.
		Where("quiet_until IS NULL OR quiet_until <= ?", at.UTC()).
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
func (s *Store) stop(ctx context.Context, userID, id string) (bool, error) {
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
		// Either shape: a rule that only watches, and one that
		// watches as well as coming round on a clock. The second
		// does not begin with the word, so matching only the start
		// would leave it never woken by anything arriving.
		Where("user_id = ? AND state = ? AND (schedule LIKE ? OR schedule LIKE ?)",
			userID, string(job.Active), job.When+" %", "%"+job.Also+job.When+" %").
		// Nor off a reading landing. A rule put aside for a week
		// is put aside from everything, not only from the clock.
		Where("quiet_until IS NULL OR quiet_until <= ?", time.Now().UTC()).
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

func (s *Store) ran(ctx context.Context, id string, next time.Time,
	state job.State, why, said string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to record that a job ran")
	}
	fields := map[string]any{
		"state":     string(state),
		"why":       clip(why, 500),
		"last_said": clip(said, 2000),
		"last_at":   at.UTC(),
		"next_at":   nil,
		// A run that finished is not part-way through anything.
		// Cleared here rather than by the runner so that a rule
		// which stops for any reason -- a failed lookup, a check
		// that withheld -- starts from the top next time, and a
		// wait never leaves a rule stuck in the middle of itself.
		"resume_at_step": 0,
		"updated_at":     time.Now().UTC(),
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
func (s *Store) arm(ctx context.Context, id string, at time.Time) error {
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

// Waits : Stops a rule part-way through, to carry on at a moment.
func (s *Store) waits(ctx context.Context, id string, at time.Time, step int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to read jobs")
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"next_at":        at.UTC(),
			"resume_at_step": step,
			"updated_at":     time.Now().UTC(),
		})
	if out.Error != nil {
		return fmt.Errorf("job: setting %s to carry on: %w", id, out.Error)
	}
	return nil
}

// Forgets : Clears what a rule last said.
//
// Only the sentence. Not the run, not the next time, not the
// state: the rule has not done anything, it has stopped being
// about something.
func (s *Store) forgets(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to read jobs")
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ? AND last_said <> ''", id).
		Updates(map[string]any{"last_said": "", "updated_at": time.Now().UTC()})
	if out.Error != nil {
		return fmt.Errorf("job: letting %s forget: %w", id, out.Error)
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
//
// The structured halves where the row has them, and worked out
// from the written ones where it does not. The engine knows one
// shape; the table holds two, and a rule written before the shape
// existed goes on working whether or not anybody converts it.
func assemble(r row) (job.Job, error) {
	var steps []job.Step
	if err := json.Unmarshal([]byte(r.Steps), &steps); err != nil {
		return job.Job{}, fmt.Errorf("job: %w", err)
	}
	j := job.Job{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Kind: r.Kind,
		Schedule: r.Schedule, Steps: steps, State: job.State(r.State),
		Why: r.Why, Because: r.Because, Said: r.LastSaid, Keep: r.Keep,
		Cool: r.Cool, Anywhere: r.Anywhere,
		Resume: r.Resume, Made: r.CreatedAt.UTC(),
	}
	if err := shape(&j, r); err != nil {
		return job.Job{}, err
	}
	if r.QuietAt != nil {
		j.Quiet = r.QuietAt.UTC()
	}
	if r.NextAt != nil {
		j.Next = r.NextAt.UTC()
	}
	if r.LastAt != nil {
		j.Last = r.LastAt.UTC()
	}
	return j, nil
}

// shape : A job's structured halves, from the row or from the
// sentences.
//
// Both columns or neither. A rule half converted would be a rule
// whose conditions came from one shape and whose steps came from
// the other, and there is no reading of that which is the rule
// anybody wrote.
func shape(j *job.Job, r row) error {
	if r.When != nil && r.Perform != nil {
		if err := json.Unmarshal([]byte(*r.When), &j.When); err != nil {
			return fmt.Errorf("job: reading when: %w", err)
		}
		if err := json.Unmarshal([]byte(*r.Perform), &j.Perform); err != nil {
			return fmt.Errorf("job: reading perform: %w", err)
		}
		return nil
	}
	when, acts, err := job.Reshape(*j)
	if err != nil {
		return fmt.Errorf("job: %q cannot be read in the new shape: %w", r.Name, err)
	}
	j.When, j.Perform = when, acts
	return nil
}

// written : A job's structured halves, as the columns hold them.
func written(j job.Job) (*string, *string, error) {
	when, err := json.Marshal(j.When)
	if err != nil {
		return nil, nil, fmt.Errorf("job: writing when: %w", err)
	}
	acts := j.Perform
	if acts == nil {
		acts = []job.Act{}
	}
	perform, err := json.Marshal(acts)
	if err != nil {
		return nil, nil, fmt.Errorf("job: writing perform: %w", err)
	}
	a, b := string(when), string(perform)
	return &a, &b, nil
}

// clip : Keeps a value inside its column.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// Keep : See keep; the change hook is told when it succeeds.
func (s *Store) Keep(ctx context.Context, j job.Job) error {
	err := s.keep(ctx, j)
	s.tell(err, j.UserID)
	return err
}

// Change : See change; the change hook is told when it succeeds.
func (s *Store) Change(ctx context.Context, j job.Job) error {
	err := s.change(ctx, j)
	s.tell(err, j.UserID)
	return err
}

// Quiet : See quiet; the change hook is told when it succeeds.
func (s *Store) Quiet(ctx context.Context, userID, id string, until time.Time) error {
	err := s.quiet(ctx, userID, id, until)
	s.tell(err, userID)
	return err
}

// Stop : See stop; the change hook is told when it succeeds.
func (s *Store) Stop(ctx context.Context, userID, id string) (bool, error) {
	stopped, err := s.stop(ctx, userID, id)
	s.tell(err, userID)
	return stopped, err
}

// Ran : See ran; the change hook is told when it succeeds.
func (s *Store) Ran(ctx context.Context, id string, next time.Time,
	state job.State, why, said string, at time.Time) error {
	err := s.ran(ctx, id, next, state, why, said, at)
	s.tell(err, "")
	return err
}

// Arm : See arm; the change hook is told when it succeeds.
func (s *Store) Arm(ctx context.Context, id string, at time.Time) error {
	err := s.arm(ctx, id, at)
	s.tell(err, "")
	return err
}

// Waits : See waits; the change hook is told when it succeeds.
func (s *Store) Waits(ctx context.Context, id string, at time.Time, step int) error {
	err := s.waits(ctx, id, at, step)
	s.tell(err, "")
	return err
}

// Asks : Leaves a rule waiting to be allowed to go on, at this step.
//
// No next time: next_at is cleared, so the clock passes over it until
// somebody answers. See job.Asking.
func (s *Store) Asks(ctx context.Context, id string, step int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("job: there is nowhere to read jobs")
	}
	out := s.db.WithContext(ctx).Model(&row{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"state":          string(job.Asking),
			"next_at":        nil,
			"resume_at_step": step,
			"updated_at":     time.Now().UTC(),
		})
	if out.Error != nil {
		err := fmt.Errorf("job: leaving %s waiting to be allowed: %w", id, out.Error)
		s.tell(err, "")
		return err
	}
	s.tell(nil, "")
	return nil
}

// Forgets : See forgets; the change hook is told when it succeeds.
func (s *Store) Forgets(ctx context.Context, id string) error {
	err := s.forgets(ctx, id)
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
