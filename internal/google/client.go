package google

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Client : An HTTP client that signs every request as the person, and
// keeps its own access token current.
//
// This is what a tool asks for. It never sees the refresh token and
// never has to think about expiry.
func (l *Link) Client(ctx context.Context, userID string) (*http.Client, error) {
	source, err := l.TokenSource(ctx, userID)
	if err != nil {
		return nil, err
	}
	return oauth2.NewClient(l.with(ctx), source), nil
}

// TokenSource : Access tokens for the person, refreshed as needed and
// written back when Google changes anything.
//
// Requires every scope in wanted, and says which are missing rather
// than letting the call fail later with a permission error nobody can
// act on: the answer to a missing scope is to grant it, which is
// something only the person can do and only if they are told.
func (l *Link) TokenSource(ctx context.Context, userID string, wanted ...string) (oauth2.TokenSource, error) {
	if l == nil {
		return nil, ErrNotConfigured
	}

	account, err := l.store.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !account.Connected() {
		if account != nil && account.BrokenAt != nil {
			return nil, fmt.Errorf("%w: %s", ErrNeedsReconnect, account.Broken)
		}
		return nil, ErrNotConnected
	}
	if missing := account.Missing(wanted...); len(missing) > 0 {
		return nil, fmt.Errorf("google: %s was never granted permission for %s",
			account.Email, strings.Join(missing, ", "))
	}

	base := l.cfg.oauth().TokenSource(l.with(ctx), &oauth2.Token{
		RefreshToken: account.Refresh.Reveal(),
	})
	return &persisting{
		link:   l,
		userID: userID,
		had:    account.Refresh.Reveal(),
		source: base,
	}, nil
}

// persisting : A token source that writes down what it learns.
//
// Three things have to be recorded and none of them are the caller's
// business. That the connection still works, so a long silence is
// visible as one. That Google has issued a new refresh token, which it
// does occasionally and which is lost at the next restart otherwise.
// And that the permission has died, which happens for reasons invisible
// from here -- the password changed, six months passed, somebody
// revoked it from a settings page -- and which every tool would
// otherwise report separately and unhelpfully.
type persisting struct {
	link   *Link
	userID string
	had    string
	source oauth2.TokenSource
}

// Token : A current access token.
func (p *persisting) Token() (*oauth2.Token, error) {
	ctx := context.Background()

	token, err := p.source.Token()
	if err != nil {
		if dead(err) {
			why := plainly(err)
			if mark := p.link.store.Broke(ctx, p.userID, why, p.link.now()); mark != nil {
				p.link.log(ctx, "cannot record that a Google connection expired", mark)
			}
			return nil, fmt.Errorf("%w: %s", ErrNeedsReconnect, why)
		}
		return nil, err
	}

	at := p.link.now()
	if refresh := token.RefreshToken; refresh != "" && refresh != p.had {
		// Rotated. Written down before anything uses the token,
		// because the one in the database is the only copy that
		// survives a restart.
		if err := p.link.store.Rotated(ctx, p.userID, refresh, at); err != nil {
			p.link.log(ctx, "cannot store a rotated Google refresh token", err)
		} else {
			p.had = refresh
		}
	}
	if err := p.link.store.Refreshed(ctx, p.userID, at); err != nil {
		p.link.log(ctx, "cannot record a Google refresh", err)
	}
	return token, nil
}

// dead : Whether this error means the permission is gone for good.
//
// invalid_grant is Google's answer to a refresh token that no longer
// exists, and no amount of retrying will change it. Everything else --
// a timeout, a 500, no network -- is worth trying again and must not
// mark a working connection as broken.
func dead(err error) bool {
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		return false
	}
	if retrieve.ErrorCode == "invalid_grant" {
		return true
	}
	// Older responses put it only in the body.
	return retrieve.ErrorCode == "" &&
		strings.Contains(string(retrieve.Body), "invalid_grant")
}

// plainly : Why the permission died, in words worth showing somebody.
//
// Google says "Token has been expired or revoked" and leaves the person
// to guess. The three real causes are worth naming, because two of them
// are things they did and will recognise.
func plainly(err error) string {
	const said = "The permission was withdrawn. That happens if you changed your " +
		"Google password, removed this app at myaccount.google.com, or did not " +
		"use it for six months."

	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) && retrieve.ErrorDescription != "" {
		return said + " Google said: " + retrieve.ErrorDescription
	}
	return said
}

// log : Records a failure that is nobody's to handle.
func (l *Link) log(ctx context.Context, msg string, err error) {
	if l.Logger != nil {
		l.Logger.WarnContext(ctx, msg, slog.Any("error", err))
	}
}

// Account : What is connected, for showing on a screen. The refresh
// token is not part of it and never leaves this package.
func (l *Link) Account(ctx context.Context, userID string) (*Account, error) {
	if l == nil {
		return nil, ErrNotConfigured
	}
	return l.store.Get(ctx, userID)
}

// Calendar : Which calendar the assistant made for itself, empty until
// it has made one.
//
// Passed through from the store rather than held here, because it
// belongs to the connection: revoke the permission and the identifier
// is worth nothing.
func (l *Link) Calendar(ctx context.Context, userID string) (string, error) {
	if l == nil {
		return "", ErrNotConfigured
	}
	return l.store.Calendar(ctx, userID)
}

// SetCalendar : Records which calendar the assistant made.
func (l *Link) SetCalendar(ctx context.Context, userID, calendarID string) error {
	if l == nil {
		return ErrNotConfigured
	}
	return l.store.SetCalendar(ctx, userID, calendarID)
}

// Disconnect : Forgets the permission.
//
// Only here. Telling Google to revoke it as well would be tidier, but a
// failure there must not leave a token stored that this says is gone --
// and the person can revoke it themselves at myaccount.google.com,
// which is the only place that actually ends Google's side of it.
func (l *Link) Disconnect(ctx context.Context, userID string) error {
	if l == nil {
		return ErrNotConfigured
	}
	return l.store.Forget(ctx, userID)
}

// Wanted : Every scope this server is configured to ask for.
func (l *Link) Wanted() []string {
	if l == nil {
		return nil
	}
	return l.cfg.wanted()
}
