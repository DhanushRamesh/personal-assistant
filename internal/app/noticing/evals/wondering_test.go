//go:build evals

// Package evals asks the real judge what it would write, and keeps
// nothing.
//
// The pass is silent by design most hours, which makes a bar that is
// working and a bar that can never be cleared look exactly alike from
// the outside. This runs the judge against the real description and
// the real rules and prints what it asks for, so the silence can be
// read instead of guessed at.
//
//	make evals-wondering
//
// Nothing is written. No rule is made, no allowance is spent, and the
// writer is never called.
package evals

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/app/profile"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	jobmysql "personal-assistant/internal/domain/job/mysql"
	memorymysql "personal-assistant/internal/domain/memory/mysql"
	"personal-assistant/internal/domain/job"
)

// everyRule : Enough rules to be the whole list for anybody.
const everyRule = 200

// TestWhatHeWouldWriteIfHeCould : What each purpose asks for, right
// now, against the real description.
func TestWhatHeWouldWriteIfHeCould(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	db, err := storage.Open(ctx, cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Skipf("no database to read: %v", err)
	}
	defer db.Close()

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor,
		Model:  cmp(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
		Timeout: 3 * time.Minute,
		// Loud, unlike the database's. The gateway's own reason for
		// a refusal is logged here and sanitised before it reaches
		// the caller, so a quiet logger throws away the only copy of
		// what actually went wrong.
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	jobs := jobmysql.New(db.DB)
	reader := &profile.Reader{Memories: memorymysql.New(db)}

	userID := os.Getenv("EVAL_USER")
	if userID == "" {
		userID = anybody(ctx, t, db)
	}

	known, err := reader.Profile(ctx, userID)
	if err != nil || strings.TrimSpace(known) == "" {
		t.Skipf("nothing is known about %s yet, so nothing is worth a rule", userID)
	}

	mine, err := jobs.Mine(ctx, userID, "", everyRule, job.Active)
	if err != nil {
		t.Fatalf("reading his rules: %v", err)
	}
	var held, remarks, notices int
	for _, j := range mine {
		held++
		switch j.Kind {
		case noticing.Remarked:
			remarks++
		case noticing.Kind:
			notices++
		}
	}
	t.Logf("%d rules held: %d asked for, %d noticed, %d remarked",
		held, held-notices-remarks, notices, remarks)
	t.Logf("the description is %d characters", len(known))
	// EVAL_SHOW=1 prints the description the judge is reasoning
	// from. A refusal that says there was no pattern is only
	// readable against what it was actually given.
	if os.Getenv("EVAL_SHOW") == "1" {
		t.Logf("what it was given:\n%s", known)
	}

	// The raw answer as well as the thought. Wondered swallows an
	// unreadable answer on purpose -- a misread rule runs for ever
	// and a missed one costs nothing -- so a pass that failed and a
	// pass that declined are the same silence from outside, which
	// is the thing this file exists to tell apart.
	var raw string
	watcher := noticing.Watcher{
		Ask: func(ctx context.Context, system, ask string) (string, error) {
			out, err := asked(ctx, env, cfg, system, ask)
			raw = out
			if err != nil {
				t.Logf("the model refused: %v", err)
			}
			return out, err
		},
		Describe: func(context.Context, string) string { return known },
		Holding: func(context.Context, string) (string, int, error) {
			var b strings.Builder
			for _, j := range mine {
				b.WriteString(j.Name + ": " + j.Schedule + "\n")
			}
			return b.String(), len(mine), nil
		},
		Now: time.Now,
	}

	for _, purpose := range noticing.Purposes() {
		// The allowance is deliberately not consulted. What is being
		// read here is what the bar lets through, and a pass that
		// skipped the model because it wrote something on Tuesday
		// would print nothing and prove nothing.
		thought, err := watcher.Wondered(ctx, userID, purpose)
		if err != nil {
			t.Errorf("%s: could not weigh one up: %v", purpose.Kind, err)
			continue
		}
		if thought.Asked == "" {
			t.Logf("%-9s nothing\n          it said: %s",
				purpose.Kind, shown(raw))
			continue
		}
		t.Logf("%-9s %q\n          name: %s\n          because: %s",
			purpose.Kind, thought.Asked, thought.Name, thought.Why)
	}
}

// shown : A raw answer as it reads in a report, including none.
func shown(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "(nothing at all)"
	}
	return raw
}

// asked : One question to the slow model, drained to the end.
func asked(ctx context.Context, env *platformai.Environment, cfg config.Config,
	system, ask string) (string, error) {
	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, Purpose: environment.PurposeProfile,
		Vendor: cfg.PlatformAI.Vendor,
		Model:  cmp(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
		SystemPrompt: system,
	})
	if err != nil {
		return "", err
	}
	var last *environment.Message
	for msg := range stream {
		m := msg
		last = &m
	}
	if last == nil {
		return "", context.Canceled
	}
	// A refusal arrives as a message and not as an error from Run,
	// so a caller that returns last.Text alone turns every failure
	// into an unreadable answer, which the judge swallows as
	// "nothing". That is the whole bug this file was written to
	// find, and reproducing it here would have hidden it again.
	if last.Kind == environment.KindError {
		return "", fmt.Errorf("%s", last.Text)
	}
	return last.Text, nil
}

// cmp : The first of two that is not empty.
func cmp(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// anybody : Whoever has rules on this server, for a run with no user
// named.
func anybody(ctx context.Context, t *testing.T, db *storage.DB) string {
	t.Helper()
	var id string
	if err := db.WithContext(ctx).Raw(
		"SELECT user_id FROM jobs GROUP BY user_id ORDER BY COUNT(*) DESC LIMIT 1").
		Scan(&id).Error; err != nil || id == "" {
		t.Skip("nobody on this server has any rules")
	}
	return id
}
