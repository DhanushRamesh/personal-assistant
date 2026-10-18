//go:build evals

// Package evals asks the real judge what it would write down, against
// what this person actually said, and keeps nothing.
//
// A dry run on purpose. The pass as the server runs it is handed the
// diary and the lists and may write to them, and a first run that goes
// straight into somebody's real calendar is not a measurement, it is a
// result you cannot take back. Here it is given no tools at all and
// asked to say what it would have written, which tests the half that
// can be wrong in an interesting way: whether it can tell an
// arrangement from small talk, and whether it resists the pull to find
// something on a day that had nothing.
//
// What this cannot test is the ordering -- read the days, then write
// what is missing -- and therefore duplicates, which is the other way
// this pass can go wrong. That wants the tools.
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

	"personal-assistant/internal/app/catching"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/chat/mysql"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/base/thinking/prompt"
)

// instead : What replaces the writing, since this run has no hands.
//
// The strict last block is not decoration. Read loosely, the first
// version of this eval failed four of seven: the model had judged every
// one correctly and said so, naming each thing it was rejecting
// alongside its reason, and the checker matched those names. A judge
// that explains itself is not a judge that got it wrong, and only a
// form that separates the working from the conclusion can tell them
// apart.
const instead = "You have no way to write anything in this run. " +
	"Do everything else exactly as described, and in place of writing, " +
	"end your answer with the things you would have written, one per line, " +
	"each line starting with WRITE: and then diary or list, then what it " +
	"would say, then for when. Nothing may follow those lines. " +
	"If there is nothing to write, end with the single line WRITE: nothing."

// TestWhatItWouldWriteDown : The judge against this person's real
// messages, keeping nothing.
func TestWhatItWouldWriteDown(t *testing.T) {
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
		Vendor:  cfg.PlatformAI.Vendor,
		Model:   either(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
		Timeout: 3 * time.Minute,
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	chats := mysql.NewRepository(db)
	userID := os.Getenv("EVAL_USER")
	if userID == "" {
		userID = anybody(ctx, t, db)
	}

	// Further back than a pass looks, when asked for, because a day
	// and a half of a quiet week has nothing in it to judge.
	back := catching.Since
	if days := os.Getenv("EVAL_DAYS"); days != "" {
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err == nil && n > 0 {
			back = time.Duration(n) * 24 * time.Hour
		}
	}

	where := cfg.Assistant.Location
	if where == nil {
		where = time.UTC
	}
	said, err := chats.SaidSince(ctx, userID, time.Now().Add(-back), catching.Messages)
	if err != nil {
		t.Fatalf("reading what they said: %v", err)
	}
	var theirs []conversationMessage
	for _, m := range said {
		if string(m.Role) == "user" && strings.TrimSpace(m.Content) != "" {
			theirs = append(theirs, conversationMessage{m.At, m.Content})
		}
	}
	if len(theirs) == 0 {
		t.Skipf("%s said nothing in the last %v", userID, back)
	}
	t.Logf("%d messages over %v, from %s", len(theirs), back,
		theirs[0].At.In(where).Format("Monday 2 January"))

	shown := catching.Shown(said, where)
	if os.Getenv("EVAL_SHOW") == "1" {
		t.Logf("what it is given:\n%s", shown)
	}

	answer, err := ask(ctx, env, prompt.Block(catching.Judge, prompt.Text(instead)),
		prompt.Block(
			prompt.Text(fmt.Sprintf("It is %s. Here is everything they said, oldest first.",
				time.Now().In(where).Format("Monday 2 January 2006, 3:04 pm"))),
			shown))
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	t.Logf("\n%s\n", strings.TrimSpace(answer))
}

// conversationMessage : Just enough of one to count and date.
type conversationMessage struct {
	At   time.Time
	Text string
}

// ask : One question, start to finish.
func ask(ctx context.Context, env *platformai.Environment, system, say string) (string, error) {
	stream, err := env.Run(ctx, environment.Request{
		Prompt: say, SystemPrompt: system, Purpose: environment.PurposeProfile,
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
		return "", fmt.Errorf("the provider said nothing")
	}
	if last.Kind == environment.KindError {
		return "", fmt.Errorf("%s", last.Text)
	}
	return last.Text, nil
}

// either : The first non-empty of two.
func either(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// anybody : Whoever has spoken most recently, so the eval runs without
// being told whose messages to read.
func anybody(ctx context.Context, t *testing.T, db *storage.DB) string {
	t.Helper()
	var id string
	if err := db.WithContext(ctx).Raw(
		"SELECT user_id FROM conversations GROUP BY user_id ORDER BY MAX(updated_at) DESC LIMIT 1").
		Scan(&id).Error; err != nil || id == "" {
		t.Skip("nobody on this server has said anything")
	}
	return id
}

// planted : A day with three things in it that must be found, and four
// that must not.
//
// The control for the run above. "Nothing is missing" is the answer
// this pass should give on most days, which makes it also the answer a
// lazy judge gives every day, and the two are indistinguishable from
// outside. So the same judge is shown a day where the answer is known.
var planted = []struct {
	Said string
	Want bool
	Why  string
}{
	{"amma has a scan at the hospital on the 23rd at four", true, "a thing and a time"},
	{"the insurance runs out on the first, I have to renew it", true, "a date of its own"},
	{"I need to buy a new charger before the trip", true, "to do, with no hour -- a list"},
	{"it rained all day today", false, "a time word that places the sentence"},
	{"I was so tired yesterday evening", false, "nothing was arranged"},
	{"what is in my diary tomorrow", false, "a question, not an arrangement"},
	{"I might go to Bangalore sometime next month", false, "wondered about, not happening"},
}

// TestItFindsWhatIsThereAndLeavesWhatIsNot : The judge against a day
// whose answer is known.
func TestItFindsWhatIsThereAndLeavesWhatIsNot(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor:  cfg.PlatformAI.Vendor,
		Model:   either(cfg.PlatformAI.SlowModel, cfg.PlatformAI.Model),
		Timeout: 3 * time.Minute,
	}, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	var b strings.Builder
	b.WriteString("Thursday 16 October\n")
	for i, c := range planted {
		b.WriteString(fmt.Sprintf("  %d:00 pm  %s\n", (i%8)+1, c.Said))
	}

	answer, err := ask(context.Background(), env,
		prompt.Block(catching.Judge, prompt.Text(instead)),
		prompt.Block(
			prompt.Text("It is Friday 17 October 2026, 9:00 am. Here is everything they said, oldest first."),
			b.String()))
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	t.Logf("\n%s\n", strings.TrimSpace(answer))

	// The conclusion only. Everything above it is the model showing
	// its working, which names the things it decided against.
	low := strings.ToLower(wrote(answer))
	t.Logf("what it would write:\n%s", wrote(answer))
	var wrong int
	for _, c := range planted {
		found := strings.Contains(low, token(c.Said))
		if found != c.Want {
			wrong++
			t.Errorf("%-50q found=%v, want %v -- %s", c.Said, found, c.Want, c.Why)
		}
	}
	t.Logf("%d of %d judged correctly", len(planted)-wrong, len(planted))
}

// token : The word a line naming this thing would have to contain.
func token(said string) string {
	switch {
	case strings.Contains(said, "scan"):
		return "scan"
	case strings.Contains(said, "insurance"):
		return "insurance"
	case strings.Contains(said, "charger"):
		return "charger"
	case strings.Contains(said, "rained"):
		return "rain"
	case strings.Contains(said, "tired"):
		return "tired"
	case strings.Contains(said, "diary"):
		return "diary tomorrow"
	default:
		return "bangalore"
	}
}

// wrote : The WRITE: lines at the end of a dry run, and nothing else.
func wrote(answer string) string {
	var out []string
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimLeft(strings.TrimSpace(line), "-*# ")
		if strings.HasPrefix(strings.ToUpper(line), "WRITE:") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
