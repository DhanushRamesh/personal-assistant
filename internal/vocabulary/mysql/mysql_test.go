package mysql_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	chatmysql "github.com/DhanushRamesh/personal-assistant/internal/chat/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
	vocabularymysql "github.com/DhanushRamesh/personal-assistant/internal/vocabulary/mysql"
)

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

// uniqueName : A username no other test will take.
func uniqueName() string {
	id := chat.NewUserID()
	return "tester" + id[len(id)-12:]
}

// newStore : Opens the test database, migrates it, and returns a store
// with a user to own the names. It skips when MySQL is not reachable,
// so the suite still runs on a bare checkout.
func newStore(t *testing.T) (*vocabularymysql.Store, string) {
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

	owner, err := chat.NewUser(uniqueName(), "hash")
	if err != nil {
		t.Fatalf("chat.NewUser: %v", err)
	}
	if err := chatmysql.NewRepository(db).CreateUser(context.Background(), owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return vocabularymysql.New(db), owner.ID
}

// when : A fixed time to write at.
func when() time.Time { return time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC) }

// A list survives the round trip in the order it was written.
func TestAListComesBackAsItWentIn(t *testing.T) {
	s, user := newStore(t)
	want := []string{"Alekhya", "Chintada", "Toofy", "air conditioner"}

	if err := s.Put(context.Background(), user, want, when()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, at, err := s.Get(context.Background(), user)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	if !at.Equal(when()) {
		t.Errorf("written at %v, want %v", at, when())
	}
}

// TestWritingAgainReplaces : A second write replaces the first.
//
// The names somebody uses change, and a list that only ever grew would
// keep priming the decoder for somebody who came up once in March --
// at the cost of one of the hundred slots Deepgram allows.
func TestWritingAgainReplaces(t *testing.T) {
	s, user := newStore(t)
	ctx := context.Background()

	if err := s.Put(ctx, user, []string{"Alekhya", "Zomato"}, when()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	later := when().Add(24 * time.Hour)
	if err := s.Put(ctx, user, []string{"Chintada"}, later); err != nil {
		t.Fatalf("Put again: %v", err)
	}

	got, at, err := s.Get(ctx, user)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got, ",") != "Chintada" {
		t.Errorf("got %v, want only the second list", got)
	}
	if !at.Equal(later) {
		t.Errorf("written at %v, want %v", at, later)
	}
}

// Somebody with no list reads back as nothing rather than as an error.
func TestNobodyWithNoListIsNotAFailure(t *testing.T) {
	s, _ := newStore(t)
	got, at, err := s.Get(context.Background(), "usr_nobody")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
	if !at.IsZero() {
		t.Errorf("got a time %v for a list that was never written", at)
	}
}

// An empty list reads back as no terms, not as one term that is blank.
// A blank keyterm would be sent to Deepgram as a word to expect.
func TestAnEmptyListHasNoTermsInIt(t *testing.T) {
	s, user := newStore(t)
	if err := s.Put(context.Background(), user, nil, when()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _, err := s.Get(context.Background(), user)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want nothing", got)
	}
}

// TestEveryoneIsTheUnion : Everybody's names come back together, once
// each.
//
// What reads this is a microphone, which cannot tell who is about to
// speak: a list narrowed to one person would mishear the other.
func TestEveryoneIsTheUnion(t *testing.T) {
	one, oneID := newStore(t)
	two, twoID := newStore(t)
	ctx := context.Background()

	// Toofy is in both, because a pet belongs to a household rather
	// than to one person in it.
	if err := one.Put(ctx, oneID, []string{"Alekhya", "Toofy"}, when()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := two.Put(ctx, twoID, []string{"Toofy", "Zomato"}, when()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, _, err := one.Everyone(ctx)
	if err != nil {
		t.Fatalf("Everyone: %v", err)
	}
	for _, want := range []string{"Alekhya", "Toofy", "Zomato"} {
		if !has(got, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
	if repeated(got) {
		t.Errorf("a name appears twice in %v", got)
	}
}

// has : Whether a term is in a list.
func has(terms []string, want string) bool {
	for _, t := range terms {
		if t == want {
			return true
		}
	}
	return false
}

// repeated : Whether any term appears more than once, ignoring case.
func repeated(terms []string) bool {
	seen := map[string]bool{}
	for _, t := range terms {
		folded := strings.ToLower(t)
		if seen[folded] {
			return true
		}
		seen[folded] = true
	}
	return false
}
