// Package mysql_test covers the parts of storing a Google permission that a
// type checker cannot: what a second write leaves behind.
package mysql_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
	googlemysql "github.com/DhanushRamesh/personal-assistant/internal/google/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/logging"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

// testEnv : A dedicated database, never the one the server uses.
func testEnv(key string) (string, bool) {
	switch key {
	case "ASSISTANT_DATABASE_NAME":
		if name := os.Getenv("ASSISTANT_TEST_DATABASE"); name != "" {
			return name, true
		}
		return "assistant_test", true
	case "ASSISTANT_DATABASE_PASSWORD":
		if pw := os.Getenv("ASSISTANT_DATABASE_PASSWORD"); pw != "" {
			return pw, true
		}
		return "friday_dev", true
	}
	return "", false
}

// newStore : Opens the test database and returns a store and a person to
// hang a permission on, skipping when MySQL is not reachable so the suite
// still runs on a bare checkout.
//
// A person is made because google_accounts refers to one, and a permission
// belonging to nobody cannot be stored.
func newStore(t *testing.T) (*googlemysql.Store, string) {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.HasSuffix(cfg.Database.Name, "_test") {
		t.Skipf("refusing to run against %q", cfg.Database.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(ctx, cfg.Database, discard, storage.Options{})
	if err != nil {
		t.Skipf("MySQL not reachable, skipping: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := storage.Migrate(ctx, db, discard); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	name := fmt.Sprintf("keeper%d", time.Now().UnixNano())
	user, err := chat.NewUser(name, "hash-not-used-here")
	if err != nil {
		t.Fatalf("NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return googlemysql.New(db), user.ID
}

// TestPutKeepsTheCalendar : Granting permission again leaves the recorded
// calendar alone.
//
// The calendar the assistant made is written down because nothing can
// rediscover it: the permission that allows the assistant its own calendar
// forbids listing them. Clearing it on a second grant means the next write
// makes a second calendar, and everything in the first becomes unreachable.
// Two calendars named Jarvis, and a birthday in the one nothing reads.
func TestPutKeepsTheCalendar(t *testing.T) {
	store, user := newStore(t)
	ctx := context.Background()

	const diary = "abc123@group.calendar.google.com"

	first := &google.Account{
		UserID:      user,
		Email:       "someone@example.com",
		Subject:     "sub-1",
		Refresh:     logging.Secret("refresh-one"),
		Scopes:      []string{"openid"},
		ConnectedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := store.Put(ctx, first); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.SetCalendar(ctx, user, diary); err != nil {
		t.Fatalf("SetCalendar: %v", err)
	}

	// The same person grants permission again, with another scope. This is
	// what happens every time the consent screen is completed, which in
	// testing mode is every seven days.
	again := &google.Account{
		UserID:      user,
		Email:       "someone@example.com",
		Subject:     "sub-1",
		Refresh:     logging.Secret("refresh-two"),
		Scopes:      []string{"openid", "https://www.googleapis.com/auth/calendar.readonly"},
		ConnectedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := store.Put(ctx, again); err != nil {
		t.Fatalf("Put again: %v", err)
	}

	kept, err := store.Calendar(ctx, user)
	if err != nil {
		t.Fatalf("Calendar: %v", err)
	}
	if kept != diary {
		t.Errorf("calendar after a second grant = %q, want %q: a cleared "+
			"calendar makes the next write create another one", kept, diary)
	}

	// The permission itself must still have been replaced.
	got, err := store.Get(ctx, user)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Refresh.Reveal() != "refresh-two" {
		t.Errorf("refresh token = %q, want the new one", got.Refresh.Reveal())
	}
	if len(got.Scopes) != 2 {
		t.Errorf("scopes = %v, want both", got.Scopes)
	}
}
