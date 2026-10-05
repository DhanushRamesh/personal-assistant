//go:build evals

// Package evals asks a real model to greet real arrivals.
//
// Not a fixed set of situations somebody invented. It samples moments
// out of what the phone actually reported over the last four weeks,
// builds exactly what the server would have built at that moment, and
// asks the real model what it would have said.
//
// The point is accuracy rather than coverage. A greeting has no single
// right answer, so nothing here asserts the words; it asserts the
// handful of things that are wrong whatever the words are -- a name
// nobody reported, an inference stated as a fact, an accusation, a
// count read out loud -- and prints the rest for a person to read.
//
// Behind a build tag because it costs real calls to a real service:
//
//	make evals-greeting
package evals

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	eventmysql "github.com/DhanushRamesh/personal-assistant/internal/event/mysql"
	"github.com/DhanushRamesh/personal-assistant/internal/greet"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/storage"
)

var (
	samples = flag.Int("samples", 8, "how many arrivals to sample")
	seed    = flag.Uint64("seed", 0, "0 picks one and prints it, so a run can be repeated")
)

// Ordinary, OrdinaryMost, Most : The same bounds the server uses, so
// what the model sees here is what it would have seen at the door.
const (
	Ordinary     = 28 * 24 * time.Hour
	OrdinaryMost = 3000
	Pace         = 1500 * time.Millisecond
)

// asserted : Turning an inference into a fact. The whole of the rule
// about distances is that a thing which several observations merely
// point at is asked about, never stated.
var asserted = []string{
	"you went", "you were at", "you watched", "you attended",
	"you visited", "you spent", "you had dinner", "you made it to",
}

// accused : Putting an absence to somebody as though they owed an
// account of it. The failure an earlier attempt at the absence rule
// produced, and the reason it was reverted.
var accused = []string{
	"you didn't", "you did not", "you never", "you missed",
	"you skipped", "you failed", "instead of",
}

// greeted : A salutation. The server says the opening itself; one
// here is the model greeting them twice.
var greeted = []string{
	"good morning", "good afternoon", "good evening",
	"welcome back", "hello,", "hi,",
}

// counted : A number read out loud. "Never read out a count" -- it is
// speech, not a report.
//
// Narrow on purpose, twice over. A number is not a count when it is
// part of a name -- the first run flagged "How was Meesaya Murukku
// 2, sir?", which is the title of the film and the best greeting in
// the sample -- and a number word is not a count when it is the
// hour, which flagged "just before six this morning".
//
// What is actually forbidden is a tally: three of something, four
// times, twice. Those are what turn a greeting into a report.
var counted = regexp.MustCompile(
	`\b\d+\s+\w+|\b(two|three|four|five|six|seven|eight|nine|ten)\s+\w+s\b|\b(times|twice)\b`)

func TestGreetingRealArrivals(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))

	db, err := storage.Open(t.Context(), cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	events := eventmysql.New(db)

	var user string
	if err := db.DB.Raw("SELECT id FROM users ORDER BY created_at LIMIT 1").Scan(&user).Error; err != nil || user == "" {
		t.Fatalf("no user to greet: %v", err)
	}
	var profile string
	db.DB.Raw("SELECT body FROM memories WHERE user_id = ? AND subject = ? LIMIT 1",
		user, "What I have noticed about them").Scan(&profile)

	where := cfg.Assistant.Location
	if where == nil {
		where = time.UTC
	}
	diary := loadDiary(t, where)

	if *seed == 0 {
		*seed = rand.Uint64()
	}
	t.Logf("seed %d -- rerun this exact sample with -seed %d", *seed, *seed)
	dice := rand.New(rand.NewPCG(*seed, 0x9E3779B97F4A7C15))

	env := provider(t, cfg, quiet)
	now := time.Now().In(where)

	// Only where there is anything to sample. Uniform over four weeks
	// put the first eight arrivals in September, where this phone had
	// never reported anything, and eight empty windows measure
	// nothing -- the first run of this made exactly that mistake.
	var first time.Time
	if err := db.DB.Raw("SELECT MIN(occurred_at) FROM events WHERE user_id = ?", user).
		Scan(&first).Error; err != nil || first.IsZero() {
		t.Fatalf("nothing has ever been reported: %v", err)
	}
	first = first.In(where)
	span := now.Sub(first)
	if span < 12*time.Hour {
		t.Skipf("only %v of history; not enough to sample", span.Round(time.Hour))
	}
	t.Logf("sampling arrivals between %s and now (%v of history)",
		first.Format("Mon 2 Jan 3:04 pm"), span.Round(time.Hour))

	var spoke, silent, flagged int
	for i := 1; i <= *samples; i++ {
		// An arrival somewhere in the history, with between two and
		// ten hours of not having spoken behind it.
		at := first.Add(time.Duration(dice.Int64N(int64(span))))
		since := at.Add(-time.Duration(2+dice.Int64N(9)) * time.Hour)

		told, n := compose(t, events, user, profile, diary, at, since, where)
		if i > 1 {
			time.Sleep(Pace)
		}
		said, took := ask(t, env, cfg, told)

		fmt.Printf("\n── %d · arrived %s · %d events since %s\n",
			i, at.Format("Mon 2 Jan 3:04 pm"), n, since.Format("3:04 pm"))
		for _, a := range told.Were {
			fmt.Printf("   diary while out: %s at %s\n", a.What, a.When.In(where).Format("3:04 pm"))
		}
		if said == "" {
			silent++
			fmt.Printf("   → (nothing)   %v\n", took)
			continue
		}
		spoke++
		fmt.Printf("   → %q   %v\n", said, took)
		if why := wrong(said, told); len(why) > 0 {
			flagged++
			for _, w := range why {
				fmt.Printf("   ✗ %s\n", w)
				t.Errorf("sample %d: %s -- %q", i, w, said)
			}
		}
	}
	t.Logf("%d sampled: %d spoke, %d stayed quiet, %d had something wrong with them",
		*samples, spoke, silent, flagged)
}

// wrong : The things that are wrong whatever the words are.
func wrong(said string, told greet.Told) []string {
	low := strings.ToLower(said)
	var why []string
	// Only where there was something to infer. "You went back in" is
	// a fact when a geofence recorded it and an inference only when
	// it was reached by putting a diary beside a stay, and a word
	// list cannot tell those apart on its own.
	if len(told.Were) > 0 {
		if w, ok := has(low, asserted); ok {
			why = append(why, "stated an inference as a fact ("+w+")")
		}
	}
	if w, ok := has(low, accused); ok {
		why = append(why, "put an absence to them as an accusation ("+w+")")
	}
	if w, ok := has(low, greeted); ok {
		why = append(why, "greeted them a second time ("+w+")")
	}
	if m := counted.FindString(low); m != "" && !named(m, told) {
		why = append(why, "read a count out loud ("+m+")")
	}
	if n := strings.Count(said, "?"); n > 1 {
		why = append(why, "asked more than one question")
	}
	for _, name := range invented(said, told) {
		why = append(why, "named somebody nothing reported ("+name+")")
	}
	return why
}

// invented : A capitalised name in the answer that appears nowhere in
// what the model was given about this window.
//
// The standing description is full of names -- a girlfriend, a
// mother, a best friend -- and reaching into it for somebody who was
// not there is the worst thing this can do. Checked against the
// window rather than against a list, so a name that really was
// reported passes.
func invented(said string, told greet.Told) []string {
	var seen strings.Builder
	for _, e := range told.Events {
		seen.WriteString(strings.ToLower(string(e.Payload)))
	}
	for _, a := range append(append([]greet.Appointment{}, told.Were...), told.Diary...) {
		seen.WriteString(strings.ToLower(a.What + " " + a.Where))
	}
	known := seen.String()

	// Mid-sentence only. A capital at the start of a sentence is
	// grammar, not a name, and treating it as one reported "Someone",
	// "Today" and "Looks" as invented people on the first run.
	var out []string
	for _, at := range regexp.MustCompile(`\b[A-Z][a-z]{2,}\b`).FindAllStringIndex(said, -1) {
		before := strings.TrimRight(said[:at[0]], " \"'")
		if before == "" || strings.HasSuffix(before, ".") || strings.HasSuffix(before, "?") ||
			strings.HasSuffix(before, "!") || strings.HasSuffix(before, "—") {
			continue
		}
		w := said[at[0]:at[1]]
		if !strings.Contains(known, strings.ToLower(w)) {
			out = append(out, w)
		}
	}
	return out
}

// named : Whether a number is part of something they were told
// about, rather than a count the model worked out.
func named(n string, told greet.Told) bool {
	for _, a := range append(append([]greet.Appointment{}, told.Were...), told.Diary...) {
		if strings.Contains(strings.ToLower(a.What), n) {
			return true
		}
	}
	for _, e := range told.Events {
		if strings.Contains(strings.ToLower(string(e.Payload)), n) {
			return true
		}
	}
	return false
}

func has(said string, words []string) (string, bool) {
	for _, w := range words {
		if strings.Contains(said, w) {
			return w, true
		}
	}
	return "", false
}

// compose : What the server would have built at that moment.
func compose(t *testing.T, events *eventmysql.Store, user, profile string,
	diary []greet.Appointment, at, since time.Time, where *time.Location) (greet.Told, int) {
	t.Helper()

	from := at.Add(-Ordinary)
	if since.Before(from) {
		from = since
	}
	all, err := events.Recent(t.Context(), user, event.Query{
		Since: from, Until: at, Limit: OrdinaryMost,
		Omit: append([]string{event.Stayed}, event.Churn...),
	})
	if err != nil {
		t.Fatalf("events: %v", err)
	}

	usual := map[string]int{}
	evs := make([]event.Event, 0, 32)
	for _, e := range all {
		usual[greet.Label(e)]++
		if !e.OccurredAt.Before(since) {
			evs = append(evs, e)
		}
	}
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	if len(evs) > greet.Most {
		evs = evs[len(evs)-greet.Most:]
	}

	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, where)
	told := greet.Told{
		Now: at, Since: since, Events: evs, Usual: usual, Profile: profile,
		Opening: greet.Opening(greet.Salutations[greet.Band(at)][0]),
	}
	for _, a := range diary {
		if a.When.Before(day) || !a.When.Before(day.AddDate(0, 0, 2)) {
			continue
		}
		if !a.AllDay && a.When.Before(at) {
			if !a.When.Before(since) {
				told.Were = append(told.Were, a)
			}
			continue
		}
		told.Diary = append(told.Diary, a)
	}
	return told, len(evs)
}

func loadDiary(t *testing.T, where *time.Location) []greet.Appointment {
	t.Helper()
	raw, err := os.ReadFile("testdata/diary.json")
	if err != nil {
		t.Fatalf("diary: %v", err)
	}
	var rows []struct {
		What   string `json:"what"`
		When   string `json:"when"`
		AllDay bool   `json:"allday"`
		Where  string `json:"where"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("diary: %v", err)
	}
	out := make([]greet.Appointment, 0, len(rows))
	for _, r := range rows {
		layout := time.RFC3339
		if r.AllDay {
			layout = "2006-01-02"
		}
		when, err := time.ParseInLocation(layout, r.When, where)
		if err != nil {
			continue
		}
		out = append(out, greet.Appointment{
			What: r.What, When: when.In(where), AllDay: r.AllDay, Where: r.Where,
		})
	}
	return out
}

func provider(t *testing.T, cfg config.Config, quiet *slog.Logger) *platformai.Environment {
	t.Helper()
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
		Thinking:     cfg.PlatformAI.Thinking, Timeout: cfg.PlatformAI.Timeout,
	}, quiet)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return env
}

// ask : The model's own words, before the opening is put in front of
// them. Checked raw on purpose: the server strips a salutation the
// model wrote, and a test that looked after the stripping would never
// see it happening.
func ask(t *testing.T, env *platformai.Environment, cfg config.Config, told greet.Told) (string, time.Duration) {
	t.Helper()
	model := cfg.PlatformAI.FastModel
	if model == "" {
		model = cfg.PlatformAI.Model
	}
	ctx, done := context.WithTimeout(t.Context(), greet.Within)
	defer done()

	start := time.Now()
	stream, err := env.Run(ctx, environment.Request{
		Prompt: greet.Prompt(told), Purpose: environment.PurposeGreeting,
		Vendor: cfg.PlatformAI.Vendor, Model: model,
		SystemPrompt: persona.Voice(cfg.Assistant.Persona, cfg.Assistant.Name),
	})
	if err != nil {
		t.Fatalf("asking: %v", err)
	}
	var last *environment.Message
	for msg := range stream {
		m := msg
		last = &m
	}
	took := time.Since(start).Round(time.Millisecond)
	if last == nil {
		t.Errorf("the provider said nothing after %v", took)
		return "", took
	}
	if last.Kind == environment.KindError {
		t.Errorf("the provider failed after %v: %s", took, last.Text)
		return "", took
	}
	return strings.TrimSpace(last.Text), took
}
