package google_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/google/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// at : A fixed moment, so stored times are the same every run.
func at() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }

// connected : A store holding a working permission.
func connected(t *testing.T, scopes ...string) *inmemory.Store {
	t.Helper()
	s := inmemory.New()
	err := s.Put(context.Background(), &google.Account{
		UserID: "usr_1", Email: "someone@gmail.com", Subject: "sub_1",
		Refresh: logging.Secret("refresh-1"), Scopes: scopes, ConnectedAt: at(),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	return s
}

// google_ : A Link pointed at a fake Google.
func google_(t *testing.T, store google.Store, handler http.HandlerFunc) (*google.Link, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	link, err := google.New(google.Config{
		ClientID: "id", ClientSecret: "secret",
		Redirect: "http://localhost:8080/v1/google/callback",
		HTTP:     server.Client(),
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/authorize",
			TokenURL: server.URL + "/token",
		},
		UserInfo: server.URL + "/userinfo",
	}, store, at)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return link, server
}

// Without a client there is nothing to connect, and that is said rather
// than failed.
func TestNothingConfigured(t *testing.T) {
	if _, err := google.New(google.Config{}, inmemory.New(), at); !errors.Is(err, google.ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

// The consent screen must ask for offline access and force the prompt.
//
// Without offline there is no refresh token at all. Without forcing it,
// Google returns one only the first time a person ever grants this
// client -- so reconnecting after a disconnect silently yields an
// access token that dies within the hour and nothing that outlives it.
func TestTheConsentScreenAsksForLastingPermission(t *testing.T) {
	link, _ := google_(t, inmemory.New(), nil)

	raw, state, err := link.Begin("usr_1")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	q := u.Query()

	if q.Get("access_type") != "offline" {
		t.Errorf("access_type = %q, want offline", q.Get("access_type"))
	}
	if q.Get("prompt") != "consent" {
		t.Errorf("prompt = %q, want consent", q.Get("prompt"))
	}
	if q.Get("state") != state {
		t.Errorf("state in the URL is not the one returned")
	}
	// Identity is always asked for: without it there is nothing to show
	// on a screen and no way to tell one account from another.
	if !strings.Contains(q.Get("scope"), "userinfo.email") {
		t.Errorf("scope = %q, want the email scope", q.Get("scope"))
	}
}

// A callback this server never started is refused, so one cannot be
// replayed or forged.
func TestAnUnknownCallbackIsRefused(t *testing.T) {
	link, _ := google_(t, inmemory.New(), nil)

	_, err := link.Complete(context.Background(), "never-issued", "code")
	if !errors.Is(err, google.ErrUnknownState) {
		t.Errorf("err = %v, want ErrUnknownState", err)
	}
}

// A state is spent once. The second use of the same callback is not a
// second connection.
func TestAStateWorksOnlyOnce(t *testing.T) {
	link, _ := google_(t, inmemory.New(), func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unused", http.StatusInternalServerError)
	})

	_, state, _ := link.Begin("usr_1")
	_, _ = link.Complete(context.Background(), state, "code")

	_, err := link.Complete(context.Background(), state, "code")
	if !errors.Is(err, google.ErrUnknownState) {
		t.Errorf("err = %v, want the state to have been spent", err)
	}
}

// A token that does not work any more is reported as needing to be
// granted again, and recorded so every tool does not discover it
// separately.
func TestAWithdrawnPermissionIsReportedOnce(t *testing.T) {
	store := connected(t)
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	})

	source, err := link.TokenSource(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("TokenSource: %v", err)
	}
	if _, err := source.Token(); !errors.Is(err, google.ErrNeedsReconnect) {
		t.Fatalf("err = %v, want ErrNeedsReconnect", err)
	}

	// Recorded, so the next caller is told without asking Google again.
	account, err := store.Get(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if account.BrokenAt == nil {
		t.Error("the broken connection was not recorded")
	}
	if account.Connected() {
		t.Error("a broken connection still reports as connected")
	}
	if !strings.Contains(account.Broken, "password") {
		t.Errorf("why = %q, want it to name the likely causes", account.Broken)
	}
}

// A refusal that is not a withdrawn permission must not mark a working
// connection as broken. A timeout or a 500 is worth trying again.
func TestATemporaryFailureDoesNotBreakTheConnection(t *testing.T) {
	store := connected(t)
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend is having a moment", http.StatusInternalServerError)
	})

	source, _ := link.TokenSource(context.Background(), "usr_1")
	if _, err := source.Token(); errors.Is(err, google.ErrNeedsReconnect) {
		t.Fatal("a passing failure was treated as a withdrawn permission")
	}

	account, _ := store.Get(context.Background(), "usr_1")
	if account.BrokenAt != nil {
		t.Errorf("marked broken by a temporary failure: %q", account.Broken)
	}
}

// A scope that was never granted is named, because the only fix is for
// the person to grant it and they cannot if nobody says which.
func TestAMissingScopeIsNamed(t *testing.T) {
	store := connected(t, "https://www.googleapis.com/auth/calendar.readonly")
	link, _ := google_(t, store, nil)

	_, err := link.TokenSource(context.Background(), "usr_1",
		"https://www.googleapis.com/auth/gmail.readonly")
	if err == nil {
		t.Fatal("a missing scope was not noticed")
	}
	if !strings.Contains(err.Error(), "gmail.readonly") {
		t.Errorf("err = %v, want it to name the scope", err)
	}
}

// Nothing connected is its own answer, not a failure.
func TestNothingConnected(t *testing.T) {
	link, _ := google_(t, inmemory.New(), nil)

	_, err := link.TokenSource(context.Background(), "usr_1")
	if !errors.Is(err, google.ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// A refresh token Google replaces is written down.
//
// Google occasionally issues a new one during a refresh. The copy in
// the database is the only one that survives a restart, so losing it
// strands the account at the next start with no sign of why.
func TestARotatedRefreshTokenIsStored(t *testing.T) {
	store := connected(t)
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-2","refresh_token":"refresh-2",` +
			`"token_type":"Bearer","expires_in":3600}`))
	})

	source, _ := link.TokenSource(context.Background(), "usr_1")
	if _, err := source.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}

	account, err := store.Get(context.Background(), "usr_1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := account.Refresh.Reveal(); got != "refresh-2" {
		t.Errorf("stored refresh token = %q, want the new one", got)
	}
	if account.RefreshedAt == nil {
		t.Error("a working refresh was not recorded")
	}
}

// Granting a different Google account is refused rather than absorbed.
// Replacing the first silently loses everything it was connected for.
func TestADifferentAccountIsRefused(t *testing.T) {
	store := connected(t)
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/userinfo") {
			_, _ = w.Write([]byte(`{"sub":"sub_2","email":"other@gmail.com"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r",` +
			`"token_type":"Bearer","expires_in":3600,"scope":"openid"}`))
	})

	_, state, _ := link.Begin("usr_1")
	_, err := link.Complete(context.Background(), state, "code")

	if !errors.Is(err, google.ErrWrongAccount) {
		t.Fatalf("err = %v, want ErrWrongAccount", err)
	}
	// And the one that was there is untouched.
	account, _ := store.Get(context.Background(), "usr_1")
	if account.Email != "someone@gmail.com" {
		t.Errorf("the connected account was replaced: %q", account.Email)
	}
}

// What Google actually granted is recorded, not what was asked for. A
// person can untick part of a consent screen and Google issues a token
// for the rest without complaint.
func TestOnlyWhatWasGrantedIsRecorded(t *testing.T) {
	store := inmemory.New()
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/userinfo") {
			_, _ = w.Write([]byte(`{"sub":"sub_1","email":"someone@gmail.com"}`))
			return
		}
		// Asked for calendar as well; only email came back.
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r",` +
			`"token_type":"Bearer","expires_in":3600,` +
			`"scope":"openid https://www.googleapis.com/auth/userinfo.email"}`))
	})

	_, state, _ := link.Begin("usr_1")
	account, err := link.Complete(context.Background(), state, "code")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if account.Has("https://www.googleapis.com/auth/calendar") {
		t.Error("a scope that was not granted is recorded as granted")
	}
	if !account.Has("https://www.googleapis.com/auth/userinfo.email") {
		t.Errorf("scopes = %v, want the granted one", account.Scopes)
	}
	if !account.Connected() {
		t.Error("a fresh connection does not report as connected")
	}
}

// A connection with no lasting permission is refused outright rather
// than stored, because it works for an hour and then looks like a bug.
func TestNoRefreshTokenIsRefused(t *testing.T) {
	store := inmemory.New()
	link, _ := google_(t, store, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer","expires_in":3600}`))
	})

	_, state, _ := link.Begin("usr_1")
	_, err := link.Complete(context.Background(), state, "code")
	if err == nil {
		t.Fatal("a connection with no refresh token was accepted")
	}
	if _, err := store.Get(context.Background(), "usr_1"); !errors.Is(err, google.ErrNotConnected) {
		t.Error("it was stored anyway")
	}
}
