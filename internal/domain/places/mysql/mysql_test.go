package mysql_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	chatmysql "personal-assistant/internal/base/records/chat/mysql"
	"personal-assistant/internal/base/records/user"
	"personal-assistant/internal/domain/places"
	placesmysql "personal-assistant/internal/domain/places/mysql"
	"personal-assistant/internal/domain/places/storetest"
)

// The same cases the in-memory store runs. Both must answer alike, or a
// test passes against one and the server runs on the other.
func TestItBehavesLikeAStore(t *testing.T) {
	storetest.Run(t, func(t *testing.T) (places.Store, string, string) {
		db := open(t)
		return placesmysql.New(db), owner(t, db), owner(t, db)
	})
}

// discard : A logger that writes nowhere.
func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testEnv : The settings the database tests run against. A dedicated
// database, never the one the server uses.
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

// open : The test database, migrated.
func open(t *testing.T) *storage.DB {
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
	db, err := storage.Open(ctx, cfg.Database, discard(), storage.Options{})
	if err != nil {
		t.Skipf("MySQL not reachable at %s, skipping: %v", cfg.Database.SafeAddr(), err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(ctx, db, discard()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// owner : A fresh user to own places.
func owner(t *testing.T, db *storage.DB) string {
	t.Helper()
	id := user.NewUserID()
	u, err := user.NewUser("tester"+id[len(id)-12:], "hash")
	if err != nil {
		t.Fatalf("user.NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}
