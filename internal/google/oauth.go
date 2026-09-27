package google

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// Identity : The scopes asked for on every connection, whatever else is
// wanted.
//
// Without an address there is nothing to show on a screen but the word
// "connected", and no way to tell reconnecting the same account from
// granting a different one by mistake.
var Identity = []string{
	"openid",
	"https://www.googleapis.com/auth/userinfo.email",
}

// StateFor : How long a started sign-in may take to come back.
//
// Long enough to read a consent screen properly, short enough that a
// link left in a browser overnight cannot be completed by whoever finds
// the laptop.
const StateFor = 10 * time.Minute

// Config : What is needed to ask Google for permission.
type Config struct {
	// ClientID and ClientSecret : From the Google Cloud console, for an
	// OAuth client of type Web application.
	ClientID     string
	ClientSecret logging.Secret

	// Redirect : Where Google sends the person back, which must match
	// a redirect URI registered on that client exactly. Loopback is the
	// one address Google allows without HTTPS.
	Redirect string

	// Scopes : What to ask for beyond Identity. Empty asks for nothing
	// more, which is a working connection that can do nothing -- useful
	// for proving the plumbing before any tool depends on it.
	Scopes []string

	// HTTP : The client used to talk to Google. Optional.
	HTTP *http.Client

	// Endpoint : Where Google's authorisation and token services are.
	// The zero value uses the real ones.
	//
	// Present so a test can stand a server in Google's place and
	// exercise the answers that matter -- a withdrawn permission, a
	// rotated token, a passing failure -- against real HTTP rather
	// than a stubbed token source. Without it the tests reached
	// accounts.google.com and passed for the wrong reasons.
	Endpoint oauth2.Endpoint

	// UserInfo : Where to ask which account a token belongs to. Empty
	// uses Google's.
	UserInfo string
}

// Configured : Whether there is enough here to connect anything.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.ClientID) != "" &&
		strings.TrimSpace(c.ClientSecret.Reveal()) != "" &&
		strings.TrimSpace(c.Redirect) != ""
}

// wanted : Every scope to ask for, identity first and no duplicates.
func (c Config) wanted() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(Identity)+len(c.Scopes))
	for _, s := range append(append([]string{}, Identity...), c.Scopes...) {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// oauth : The x/oauth2 configuration this describes.
func (c Config) oauth() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret.Reveal(),
		RedirectURL:  c.Redirect,
		Scopes:       c.wanted(),
		Endpoint:     c.endpoint(),
	}
}

// endpoint : Google's token service, or whatever stands in for it.
func (c Config) endpoint() oauth2.Endpoint {
	if c.Endpoint.TokenURL != "" {
		return c.Endpoint
	}
	return googleoauth.Endpoint
}

// userinfo : Where to ask which account a token belongs to.
func (c Config) userinfo() string {
	if strings.TrimSpace(c.UserInfo) != "" {
		return c.UserInfo
	}
	return "https://openidconnect.googleapis.com/v1/userinfo"
}

// Link : Starts and finishes the granting of permission, and hands out
// clients that use it.
type Link struct {
	cfg   Config
	store Store
	now   func() time.Time

	// Logger : Where failures nobody can handle are recorded. Optional.
	Logger *slog.Logger

	mu      sync.Mutex
	pending map[string]start
}

// start : A sign-in this server began, waiting to be completed.
type start struct {
	userID string
	at     time.Time
}

// New : Builds a Link, or reports that nothing is configured.
func New(cfg Config, store Store, now func() time.Time) (*Link, error) {
	if !cfg.Configured() {
		return nil, ErrNotConfigured
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Link{cfg: cfg, store: store, now: now, pending: map[string]start{}}, nil
}

// Begin : The address to send somebody to, and the state that will come
// back with them.
//
// AccessTypeOffline and "consent" together are what produce a refresh
// token. Without offline there is no refresh token at all; without
// forcing the prompt Google returns one only the first time a person
// ever grants this client, so the second connection after a disconnect
// silently yields an access token that dies in an hour.
func (l *Link) Begin(userID string) (string, string, error) {
	if l == nil {
		return "", "", ErrNotConfigured
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("google: making a state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(raw)

	l.mu.Lock()
	l.forgetStale()
	l.pending[state] = start{userID: userID, at: l.now()}
	l.mu.Unlock()

	return l.cfg.oauth().AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	), state, nil
}

// forgetStale : Drops sign-ins nobody came back from. Caller holds the
// lock.
func (l *Link) forgetStale() {
	cutoff := l.now().Add(-StateFor)
	for k, v := range l.pending {
		if v.at.Before(cutoff) {
			delete(l.pending, k)
		}
	}
}

// Complete : Finishes a sign-in and stores the permission.
//
// The state is checked and spent, so a callback cannot be replayed and
// one this server never started is refused.
func (l *Link) Complete(ctx context.Context, state, code string) (*Account, error) {
	if l == nil {
		return nil, ErrNotConfigured
	}

	l.mu.Lock()
	l.forgetStale()
	began, ok := l.pending[state]
	delete(l.pending, state)
	l.mu.Unlock()
	if !ok {
		return nil, ErrUnknownState
	}

	ctx = l.with(ctx)
	token, err := l.cfg.oauth().Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google: exchanging the code: %w", err)
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		// Nothing durable came back, so this would work until the
		// access token expired and then look like a bug. Said plainly
		// instead.
		return nil, errors.New("google: no lasting permission was returned; " +
			"remove this app at myaccount.google.com and grant it again")
	}

	who, err := l.identify(ctx, token)
	if err != nil {
		return nil, err
	}

	// A different account than the one already connected is a mistake
	// worth stopping rather than absorbing. Replacing it silently loses
	// everything the first one was for.
	if existing, err := l.store.Get(ctx, began.userID); err == nil && existing.Subject != "" &&
		existing.Subject != who.Subject {
		return nil, fmt.Errorf("%w: %s is connected, not %s",
			ErrWrongAccount, existing.Email, who.Email)
	}

	at := l.now()
	account := &Account{
		UserID:      began.userID,
		Email:       who.Email,
		Subject:     who.Subject,
		Refresh:     logging.Secret(token.RefreshToken),
		Scopes:      granted(token),
		ConnectedAt: at,
		RefreshedAt: &at,
	}
	if err := l.store.Put(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

// granted : The scopes Google says were actually given.
//
// Read from the response rather than assumed from the request: a person
// can untick part of a consent screen and Google issues a token for the
// rest without complaint.
func granted(token *oauth2.Token) []string {
	raw, _ := token.Extra("scope").(string)
	return strings.Fields(raw)
}

// who : The little Google says about an account without any further
// scope.
type who struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
}

// identify : Which account this token belongs to.
func (l *Link) identify(ctx context.Context, token *oauth2.Token) (who, error) {
	var out who

	client := l.cfg.oauth().Client(ctx, token)
	resp, err := client.Get(l.cfg.userinfo())
	if err != nil {
		return out, fmt.Errorf("google: asking which account this is: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("google: asking which account this is: %s", resp.Status)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("google: reading which account this is: %w", err)
	}
	return out, nil
}

// with : The context the oauth2 package should use for its own calls.
func (l *Link) with(ctx context.Context) context.Context {
	if l.cfg.HTTP == nil {
		return ctx
	}
	return context.WithValue(ctx, oauth2.HTTPClient, l.cfg.HTTP)
}
