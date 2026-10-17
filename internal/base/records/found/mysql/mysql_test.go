package mysql_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/chat"
	chatmysql "personal-assistant/internal/base/records/chat/mysql"
	"personal-assistant/internal/base/records/found"
	foundmysql "personal-assistant/internal/base/records/found/mysql"
	"personal-assistant/internal/base/records/user"
)

func discard() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// testEnv : The test database and nothing else, as every store test
// here does it, so these can only ever reach the test database and
// never the one the server uses.
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

func uniqueName() string {
	id := user.NewUserID()
	return "tester" + id[len(id)-12:]
}

// newStore : A store, a person and a conversation to find things in.
func newStore(t *testing.T) (*foundmysql.Store, string, string) {
	t.Helper()

	cfg, err := config.Load("", testEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !strings.HasSuffix(cfg.Database.Name, "_test") {
		t.Skipf("refusing to run against %q: the database tests need one whose name ends in _test", cfg.Database.Name)
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

	owner, err := user.NewUser(uniqueName(), "hash")
	if err != nil {
		t.Fatalf("user.NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	talk := chat.NewConversation(owner.ID, "Events")
	if err := chatmysql.NewRepository(db).CreateConversation(context.Background(), talk); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	return foundmysql.New(db), owner.ID, talk.ID
}

// A result put aside comes back whole, and says it was asked for.
func TestWhatWasPutAsideComesBackWhole(t *testing.T) {
	store, owner, talk := newStore(t)
	ctx := context.Background()

	body := "300 events today, as 298 entries once repeats are grouped.\n" +
		strings.Repeat("  01:31  battery.level value=77 via=phone\n", 60)

	kept, err := store.Keep(ctx, found.Found{
		UserID: owner, ConversationID: talk,
		Tool: "event_recent", Arguments: `{"hours":24}`,
		Head: found.Head(body), Body: body,
	})
	if err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if kept.ID == "" {
		t.Fatal("what was kept has no name to read it back by")
	}

	got, err := store.Read(ctx, owner, kept.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Body != body {
		t.Errorf("the body came back changed: %d characters, want %d", len(got.Body), len(body))
	}
	if got.Rereads != 1 {
		t.Errorf("a read was not counted: rereads is %d, want 1", got.Rereads)
	}
	if again, err := store.Read(ctx, owner, kept.ID); err != nil || again.Rereads != 2 {
		t.Errorf("the second read counted %d, want 2 (err %v)", again.Rereads, err)
	}
}

// Somebody else's identifier finds nothing, however well guessed.
func TestAnotherPersonCannotReadWhatWasFound(t *testing.T) {
	store, owner, talk := newStore(t)
	ctx := context.Background()

	kept, err := store.Keep(ctx, found.Found{
		UserID: owner, ConversationID: talk, Tool: "event_recent", Body: "a wall of events",
	})
	if err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if _, err := store.Read(ctx, "usr_someone_else", kept.ID); !errors.Is(err, found.ErrGone) {
		t.Errorf("another person read it: %v", err)
	}
}

// Asking for something forgotten says so, rather than coming back
// empty: nothing found is an answer, and a result that has gone is a
// reason to ask the tool again.
func TestAForgottenResultSaysSo(t *testing.T) {
	store, owner, talk := newStore(t)
	ctx := context.Background()

	kept, err := store.Keep(ctx, found.Found{
		UserID: owner, ConversationID: talk, Tool: "event_recent", Body: "a wall of events",
	})
	if err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if err := store.Forget(ctx, talk); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := store.Read(ctx, owner, kept.ID); !errors.Is(err, found.ErrGone) {
		t.Errorf("a forgotten result answered with %v, want ErrGone", err)
	}
}

// A head longer than the column is cut rather than refused: a tool
// whose first line runs on is not a reason to lose the body.
func TestALongFirstLineDoesNotLoseTheBody(t *testing.T) {
	store, owner, talk := newStore(t)
	ctx := context.Background()

	kept, err := store.Keep(ctx, found.Found{
		UserID: owner, ConversationID: talk, Tool: "mail_search",
		Head: strings.Repeat("a", 900), Body: "the whole thread",
	})
	if err != nil {
		t.Fatalf("Keep: %v", err)
	}
	got, err := store.Read(ctx, owner, kept.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Body != "the whole thread" {
		t.Errorf("the body was lost with the head: %q", got.Body)
	}
	if len([]rune(got.Head)) > 500 {
		t.Errorf("the head is %d runes, want it cut to the column", len([]rune(got.Head)))
	}
}
