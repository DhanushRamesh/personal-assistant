//go:build evals

// Package evals prints the counted half of the description from the
// real database, and keeps nothing.
//
//	make evals-rhythm
//
// Owner: *"its only purpose make jarvis talk to me... commenting on
// my taste."* Whether it can is decided here, before any model is
// asked: if what somebody listens to is not in these lines, no bar
// and no prompt can put it in the remark.
//
// Written because a made-up fixture cannot find this class of fault.
// The ranking that hid four weeks of music was correct on every test
// it had, each of which built a handful of events by hand. The fault
// only appears at the ratio real devices report at -- one sensor
// talking a thousand times against a person listening a hundred --
// and that ratio is in the database and nowhere else.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/world/event"
	eventmysql "personal-assistant/internal/base/world/event/mysql"
)

// TestWhatTheCountedHalfSays : the rhythm as it stands right now.
func TestWhatTheCountedHalfSays(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	db, err := storage.Open(ctx, cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Skipf("no database to read: %v", err)
	}
	defer db.Close()

	userID := os.Getenv("EVAL_USER")
	if userID == "" {
		userID = busiest(ctx, t, db)
	}

	// The same read the builder makes, including what it leaves
	// out. A position reading every five minutes would be eight
	// thousand of the four thousand events allowed.
	did, err := eventmysql.New(db).Recent(ctx, userID, event.Query{
		Since: time.Now().Add(-profile.Habits),
		Limit: profile.Doings,
		Omit:  []string{event.Fixed},
	})
	if err != nil {
		t.Fatalf("reading what their devices reported: %v", err)
	}
	if len(did) == 0 {
		t.Skipf("%s has no events in the last four weeks", userID)
	}

	t.Logf("%d events read for %s\n\n%s", len(did), userID,
		profile.Rhythm(did, cfg.Assistant.Location))
}

// busiest : Whoever has reported the most, so the eval has something
// to print without being told who to look at.
func busiest(ctx context.Context, t *testing.T, db *storage.DB) string {
	t.Helper()
	var id string
	if err := db.WithContext(ctx).Raw(
		"SELECT user_id FROM events GROUP BY user_id ORDER BY COUNT(*) DESC LIMIT 1").
		Scan(&id).Error; err != nil || id == "" {
		t.Skip("nobody on this server has reported anything")
	}
	return id
}
