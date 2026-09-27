// Package google holds a person's standing permission to reach their own
// Google account, and hands out clients that use it.
//
// Nothing here talks to Gmail or Calendar or anything else. This is the
// one thing all of those need and none of them should each solve: a
// refresh token that survives restarts, an access token kept current
// without anybody asking, and a clear answer when the permission has
// gone rather than a failure that looks like a bug in whatever tool
// happened to be running.
package google

import (
	"errors"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// Account : A Google account this assistant may act for.
//
// One per person. A second Google account would be a second row and a
// way to say which is meant, and there is no call for that yet.
type Account struct {
	// UserID : Whose account it is, in this assistant's terms.
	UserID string

	// Email : Which Google account it is, for showing on a screen.
	//
	// Kept because "connected" without saying to what is not worth
	// showing, and because somebody with two accounts needs to see
	// which one they granted.
	Email string

	// Subject : Google's own permanent identifier for the account.
	//
	// An address can change hands; this cannot. Compared on
	// reconnection so that granting a different account is noticed as a
	// different account rather than silently replacing the first.
	Subject string

	// Refresh : The standing permission. Never logged, never returned
	// by the API, never shown.
	Refresh logging.Secret

	// Scopes : What was actually granted, which is not always what was
	// asked for -- a person can decline part of a consent screen and
	// Google will still return a token.
	Scopes []string

	// ConnectedAt : When permission was first given.
	ConnectedAt time.Time
	// RefreshedAt : When the refresh token was last exchanged for a
	// working access token. A long silence here is the first sign that
	// something has quietly stopped.
	RefreshedAt *time.Time

	// BrokenAt, Broken : When the permission stopped working, and what
	// Google said. Nil while it works.
	//
	// Recorded rather than inferred at each call. A refresh token dies
	// for reasons nobody here can see -- the password changed, six
	// months passed unused, it was revoked from a settings page -- and
	// every tool that needs it would otherwise fail separately and
	// unhelpfully.
	BrokenAt *time.Time
	Broken   string
}

// Connected : Whether there is a usable permission.
func (a *Account) Connected() bool {
	return a != nil && strings.TrimSpace(a.Refresh.Reveal()) != "" && a.BrokenAt == nil
}

// Has : Whether this scope was granted.
func (a *Account) Has(scope string) bool {
	if a == nil {
		return false
	}
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Missing : Which of the wanted scopes were not granted, in order.
//
// A person can untick part of a consent screen, and Google issues a
// token for the rest without complaint. A tool that assumed otherwise
// would fail at the call with a permission error nobody could act on.
func (a *Account) Missing(wanted ...string) []string {
	var out []string
	for _, s := range wanted {
		if !a.Has(s) {
			out = append(out, s)
		}
	}
	return out
}

var (
	// ErrNotConnected : No Google account has been linked.
	ErrNotConnected = errors.New("google: no account is connected")

	// ErrNeedsReconnect : The permission was there and has stopped
	// working. Only the person can fix it, by granting it again.
	ErrNeedsReconnect = errors.New("google: the connection has expired and must be granted again")

	// ErrNotConfigured : No client credentials, so nothing can be
	// connected in the first place.
	ErrNotConfigured = errors.New("google: no client credentials are configured")

	// ErrWrongAccount : The account granted is not the one already
	// connected.
	ErrWrongAccount = errors.New("google: that is a different Google account")

	// ErrUnknownState : A callback arrived that this server did not
	// start, or started too long ago.
	ErrUnknownState = errors.New("google: that sign-in was not started here, or has expired")
)
