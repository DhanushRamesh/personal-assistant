package google

import (
	"context"
	"time"
)

// Store : Where a person's permission is kept.
type Store interface {
	// Get : The account linked to this person, or ErrNotConnected.
	Get(ctx context.Context, userID string) (*Account, error)

	// Put : Stores the permission, replacing any already there.
	Put(ctx context.Context, a *Account) error

	// Refreshed : Notes that the permission still works, and clears any
	// earlier failure.
	//
	// Separate from Put because it happens on every token refresh and
	// must not be able to disturb the scopes or the address. Also
	// clears BrokenAt: a connection that has started working again is
	// working, and leaving the mark would send somebody to reconnect
	// something that is fine.
	Refreshed(ctx context.Context, userID string, at Timestamp) error

	// Rotated : Replaces the refresh token, which Google occasionally
	// issues anew during a refresh. Losing the new one strands the
	// account at the next restart.
	Rotated(ctx context.Context, userID, refresh string, at Timestamp) error

	// Broke : Records that the permission has stopped working and why,
	// so it is reported once and clearly rather than as a failure in
	// whatever happened to be asking.
	Broke(ctx context.Context, userID, why string, at Timestamp) error

	// Forget : Removes the permission entirely.
	Forget(ctx context.Context, userID string) error

	// Calendar, SetCalendar : Which calendar the assistant made for
	// itself, empty until it has made one.
	//
	// Kept beside the connection because it belongs to it: revoke the
	// permission and the calendar is no longer reachable, so the
	// identifier is worth nothing without the row it sits in.
	Calendar(ctx context.Context, userID string) (string, error)
	SetCalendar(ctx context.Context, userID, calendarID string) error
}

// Timestamp : A moment, in UTC.
type Timestamp = time.Time
