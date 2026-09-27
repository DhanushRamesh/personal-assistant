package remind

import (
	"context"
	"strings"
	"time"
)

// Change : What to alter on a reminder.
//
// Every field is a pointer so that "leave it alone" and "set it to
// nothing" are different things. Clearing the past-tense wording and
// not mentioning it are both reasonable and must not be confused.
type Change struct {
	Title    *string
	Body     *string
	SaidLate *string
	DueAt    *time.Time
	Repeats  *Repeat
}

// Empty : Whether this asks for nothing.
func (c Change) Empty() bool {
	return c.Title == nil && c.Body == nil && c.SaidLate == nil &&
		c.DueAt == nil && c.Repeats == nil
}

// Apply : The reminder as it would be after the change, or why it may
// not be made.
//
// Validated as a whole rather than field by field, so a change that
// would leave a reminder with no name or nothing to say is refused
// before it is written rather than after.
func (c Change) Apply(r *Reminder) (*Reminder, error) {
	if r == nil {
		return nil, ErrNotFound
	}
	if err := Amendable(r); err != nil {
		return nil, err
	}

	out := *r
	if c.Title != nil {
		out.Title = strings.TrimSpace(*c.Title)
	}
	if c.Body != nil {
		out.Body = strings.TrimSpace(*c.Body)
	}
	if c.SaidLate != nil {
		out.SaidLate = strings.TrimSpace(*c.SaidLate)
	}
	if c.DueAt != nil {
		out.DueAt = c.DueAt.UTC()
	}
	if c.Repeats != nil {
		out.Repeats = *c.Repeats
	}
	out.UpdatedAt = now()

	if err := out.Valid(); err != nil {
		return nil, err
	}
	return &out, nil
}

// ErrNotAmendable : A reminder in a state that cannot be changed.
var ErrNotAmendable = errNotAmendable{}

type errNotAmendable struct{}

func (errNotAmendable) Error() string {
	return "remind: only one that is waiting, or being held, can be changed"
}

// Amendable : Whether a reminder may be changed.
//
// Waiting or held. A finished one is history and changing it would
// rewrite what was said; a cancelled one was called off on purpose.
// Neither is a thing to edit -- they are a thing to set again.
func Amendable(r *Reminder) error {
	switch {
	case r == nil:
		return ErrNotFound
	case r.Status != Pending && r.Status != Held:
		return ErrNotAmendable
	}
	return nil
}

// Amend : Changes a reminder, or reports why it could not be.
//
// Reads it, works out what it would become, and writes that. The check
// and the write are separate, so a reminder that fired in between is
// refused rather than quietly resurrected.
func Amend(ctx context.Context, store Store, userID, id string, c Change) (*Reminder, error) {
	if store == nil {
		return nil, ErrNotFound
	}
	if c.Empty() {
		return nil, ErrNothingToChange
	}

	existing, err := store.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}

	changed, err := c.Apply(existing)
	if err != nil {
		return nil, err
	}
	if err := store.Replace(ctx, userID, changed, existing.Status); err != nil {
		return nil, err
	}
	return changed, nil
}

// ErrNothingToChange : An amendment that asks for nothing.
var ErrNothingToChange = errNothingToChange{}

type errNothingToChange struct{}

func (errNothingToChange) Error() string { return "remind: nothing was given to change" }
