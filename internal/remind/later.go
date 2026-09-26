package remind

import (
	"context"
	"time"
)

// Later : Puts a reminder off until a later time.
//
// Where a reminder that repeats is put off, the series is left exactly
// where it was and a single one-off is made instead. Moving its due time
// would move every turn after it: a daily seven o'clock put off ten
// minutes is ten past seven tomorrow, and twenty past the day after.
//
// The one-off it made is returned, or nil where the reminder itself was
// moved. Both are a reminder that will go off at until; the difference is
// what the person has to be told, and only they can tell them.
func Later(ctx context.Context, store Store, r *Reminder, until time.Time) (*Reminder, error) {
	if store == nil || r == nil {
		return nil, ErrNotFound
	}

	if r.Repeats != Once {
		one, err := New(r.UserID, r.ClientID, r.Scope, r.Title, r.Body, until, Once)
		if err != nil {
			return nil, err
		}
		if err := store.Create(ctx, one); err != nil {
			return nil, err
		}
		return one, nil
	}

	return nil, store.Snooze(ctx, r.UserID, r.ID, until)
}
