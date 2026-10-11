//go:build evals

// Package evals runs the cross-domain name search against the real
// database, so what it finds is his own reminders, rules and memories
// rather than something invented to pass.
//
// No model is asked anything here and no API call is made. Everything
// this measures is counted, so it is cheap enough to run on every
// change to the matching.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"personal-assistant/internal/app/elsewhere"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	jobmysql "personal-assistant/internal/domain/job/mysql"
	memorymysql "personal-assistant/internal/domain/memory/mysql"
	remindmysql "personal-assistant/internal/domain/remind/mysql"
)

// asked : Things he might say, and the domain the tool that failed
// would have been looking in.
var asked = []struct{ said, except string }{
	{"water alarm", "reminder"},
	{"the water one", "reminder"},
	{"drink water", "rule"},
	{"good morning", "reminder"},
	{"the standup", "diary entry"},
	{"qwerty nonsense", "reminder"},
}

func TestWhatHisOwnDataAnswers(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(ctx, cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var userID string
	if err := db.DB.WithContext(ctx).
		Raw(`SELECT user_id FROM reminders ORDER BY created_at DESC LIMIT 1`).
		Scan(&userID).Error; err != nil || userID == "" {
		t.Skipf("no user to search for: %v", err)
	}

	// The local sources only. Tasks and the diary go to Google and
	// need a live token, which is not something an eval should
	// depend on to tell you whether matching works.
	search := elsewhere.Search{
		Logger: quiet,
		Sources: []elsewhere.Source{
			elsewhere.Reminders(remindmysql.New(db), cfg.Assistant.Location),
			elsewhere.Rules(jobmysql.New(db.DB), "rule", cfg.Assistant.Location),
			elsewhere.Remembered(memorymysql.New(db)),
		},
	}

	for _, a := range asked {
		found := search.Found(ctx, userID, a.said, a.except)
		if len(found) == 0 {
			t.Logf("%-20q nothing (looked everywhere but %s)", a.said, a.except)
			continue
		}
		for _, f := range found {
			state := "over"
			if f.Live {
				state = "live"
			}
			t.Logf("%-20q %s: %q %s (%s)", a.said, f.Kind, f.Named, f.When, state)
		}
	}
}
