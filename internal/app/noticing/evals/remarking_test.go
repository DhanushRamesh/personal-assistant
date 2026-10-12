//go:build evals

package evals

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/noticing"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// What a rule says when it speaks, against the rule's own words.
//
// Owner, shown a remark at 9:27 one morning -- "The phone was locked
// just before one this morning, sir -- that is a short night." --
// said: *"it was supposed to be a good morning one."*
//
// The rule had asked for three things and got one. "Greet them for
// the time of day" produced no greeting. "Say roughly how long the
// gap was in hours" produced a clock time instead of a length, which
// is also how it slipped past "round to the nearest half hour and
// never give minutes": a timestamp has no minutes to round.
//
// The first version of this measured the rule's wording alone, and
// said both the old and the new greeted three times out of three. It
// was right and it was useless: the rule was never the problem. What
// swallowed the greeting was noticing.Interrupting, which told it not
// to open with a word of its own, and which lived in package main
// where no test could reach it.
//
// So it is shared now and included here. This is still not the whole
// production prompt -- the two bars for speaking are also in main --
// but it holds the two things that were actually in conflict.

// greets : The remark opens with a greeting for the time of day.
var greets = regexp.MustCompile(`(?i)^\W*(good (morning|afternoon|evening)|morning, )`)

// clockTime : A time of day, which is what it is not to give.
var clockTime = regexp.MustCompile(`(?i)\b\d{1,2}[:.]\d{2}\b|\b(just (before|after) )?(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s*(o'clock|am|pm|in the morning|this morning)\b`)

// lasted : A length of time, which is what it is to give.
var lasted = regexp.MustCompile(`(?i)\b(about|around|a little (over|under)|nearly|just under|just over)?\s*\w+(\s+and\s+a\s+half)?\s+hours?\b`)

// was : The rule's wording before it was rewritten.
const was = "They have just unlocked the phone after a long stretch with it locked, " +
	"which almost always means they were asleep. The first lookup shows when it was " +
	"locked; the clock says what time it is now. The second shows calls missed in that " +
	"time. Greet them for the time of day -- morning, afternoon or evening -- say " +
	"roughly how long the gap was in hours, and remark on it as a person would: a short " +
	"night is worth noticing, a long one is worth saying so. Then, only if somebody " +
	"tried to reach them while they slept, say who and when. Round to the nearest half " +
	"hour and never give minutes. Three sentences at most. If the gap is under five " +
	"hours, say nothing at all."

// looked : What the rule's two lookups handed back on the morning it
// went wrong, as the runner renders them.
const looked = "event_recent returned, just now:\n" +
	"screen.locked -- 12:52 am today\n\n" +
	"event_recent returned, just now:\n" +
	"No calls were missed."

func TestAMorningRemarkGreetsAndSaysHowLong(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// Nine in the morning, which is when it happened and what the
	// greeting has to be chosen from.
	morning := time.Date(2026, 10, 12, 9, 27, 0, 0, cfg.Assistant.Location)
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken) +
		" " + conversation.Now(morning)

	// The writer's own opening, which is the rest of what wraps a
	// rule's words in production. It is quoted rather than shared
	// because it still lives in package main; if it turns out to be
	// what matters, it moves next.
	const opener = "You are deciding whether to interrupt somebody, and then, only if you " +
		"are, writing the one sentence they will hear. The deciding is the part that " +
		"matters. Saying nothing is always available, costs them nothing, and is the " +
		"right answer more often than not."

	for _, wording := range []struct{ name, ask string }{
		{"rule alone", was},
		{"with the opener", opener + "\n\n" + was},
		{"now, with opener", opener + "\n\n" + stored(t, cfg)},
	} {
		var greeted, said int
		const runs = 3
		for i := 0; i < runs; i++ {
			ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
			out, err := asked(ctx, env, cfg, system,
				wording.ask+"\n\n"+noticing.Interrupting+"\n\n"+looked)
			stop()
			if err != nil {
				t.Fatalf("%s: %v", wording.name, err)
			}
			out = strings.TrimSpace(out)
			ok := greets.MatchString(out)
			length := lasted.MatchString(out) && !clockTime.MatchString(out)
			if ok {
				greeted++
			}
			if length {
				said++
			}
			t.Logf("  %-13s greeting %-3v length %-3v  %s",
				wording.name, ok, length, out)
		}
		t.Logf("%-14s greeted %d of %d, gave a length rather than a clock time %d of %d",
			wording.name, greeted, runs, said, runs)
	}
}

// stored : The rule's wording as it is in the database right now, so
// this cannot drift from what actually runs.
func stored(t *testing.T, cfg config.Config) string {
	t.Helper()
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()

	db, err := storage.Open(ctx, cfg.Database,
		slog.New(slog.NewTextHandler(io.Discard, nil)), storage.Options{})
	if err != nil {
		t.Skipf("no database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var ask sql.NullString
	err = db.DB.WithContext(ctx).Raw(
		`SELECT JSON_UNQUOTE(JSON_EXTRACT(perform, '$[2].ask'))
		   FROM jobs WHERE name = 'slept and woke' LIMIT 1`).Scan(&ask).Error
	if err != nil || !ask.Valid || ask.String == "" {
		t.Skipf("the rule is not there to read: %v", err)
	}
	return ask.String
}
