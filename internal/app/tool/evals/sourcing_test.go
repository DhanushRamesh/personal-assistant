//go:build evals

// Package evals measures, over every answer this server has given,
// whether an answer that was looked up says that it was.
//
// No model and no gateway: the chats and the tool calls they made are
// already in the database, so the past is the evidence.
package evals

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
)

// Since : How far back to read. Long enough to carry the figure this
// is meant to move: 48 of 353, measured on 19 October 2026.
const Since = "30 DAY"

// reads : The tools that look something up, and the domain each one
// belongs to.
//
// By name rather than from the registry, so the measurement needs none
// of the server standing up -- and by read rather than by prefix,
// which is the correction this needed. The first run counted writes:
// "Your twenty minute timer is set, sir" came back as a reminder
// answer that failed to say where it came from, when a write carries
// no source by design and should not. Reminders read 0 of 166 that
// way, and 166 was mostly reminder_set.
var reads = map[string]string{
	"calendar_calendars": "calendar", "calendar_events": "calendar", "calendar_free": "calendar",
	"coming_due": "coming", "coming_settled": "coming",
	"leaving_due": "leaving",
	"task_find":   "task", "task_list": "task", "task_lists": "task",
	"reminder_list": "reminder", "reminder_recent": "reminder",
	"mail_inbox": "mail", "mail_read": "mail", "mail_search": "mail",
	"mail_thread": "mail", "mail_unread": "mail",
	"memory_list": "memory", "memory_search": "memory",
	"event_kinds": "event", "event_ledger": "event",
	"event_recent": "event", "event_untouched": "event",
	"call_people":    "calling",
	"place_distance": "map", "place_find": "map", "place_near": "map", "place_visits": "map",
	"web_read": "web", "web_search": "web",
	"situation_find": "situation",
}

type said struct {
	ID       string
	Response string
}

type called struct {
	ChatID    string
	ToolCalls string
}

func open(t *testing.T) *storage.DB {
	t.Helper()
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := storage.Open(context.Background(), cfg.Database,
		slog.New(slog.NewTextHandler(io.Discard, nil)), storage.Options{})
	if err != nil {
		t.Skipf("no database to read: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestAnAnswerThatWasLookedUpSaysSo : The figure the provenance line
// exists to move.
//
// An answer that called a tool should name where it came from, in the
// words tool.From gives for that tool's domain.
//
// The baseline, over the thirty days up to 19 October 2026 and taken
// minutes after tool.Carries went in, so all of it predates the line:
//
//	answers that looked something up   440
//	of those, saying where from         19   (4%)
//	  calendar   3 of 123      mail     8 of 34
//	  task       5 of  66      web      2 of 52
//	  event      0 of  74      reminder 0 of 62
//	  map        0 of  23      memory   0 of 11
//
// Reminders and events are the two worth watching: 136 answers between
// them, not one of which said where it had looked.
func TestAnAnswerThatWasLookedUpSaysSo(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	var answers []said
	if err := db.WithContext(ctx).Raw(
		`SELECT id, response FROM chats
		 WHERE status = 'completed' AND response IS NOT NULL AND TRIM(response) <> ''
		   AND created_at > UTC_TIMESTAMP() - INTERVAL ` + Since).Scan(&answers).Error; err != nil {
		t.Fatalf("reading the answers: %v", err)
	}

	var calls []called
	if err := db.WithContext(ctx).Raw(
		`SELECT chat_id, tool_calls FROM messages
		 WHERE chat_id IS NOT NULL AND tool_calls IS NOT NULL AND tool_calls <> ''
		   AND created_at > UTC_TIMESTAMP() - INTERVAL ` + Since).Scan(&calls).Error; err != nil {
		t.Fatalf("reading the tool calls: %v", err)
	}
	if len(answers) == 0 {
		t.Skip("this server has answered nothing yet")
	}

	// Which domains each answer actually read from.
	read := map[string]map[string]bool{}
	for _, c := range calls {
		var made []struct{ Name string }
		if json.Unmarshal([]byte(c.ToolCalls), &made) != nil {
			continue
		}
		for _, one := range made {
			domain, ok := reads[one.Name]
			if !ok || tool.From(domain) == "" {
				continue
			}
			if read[c.ChatID] == nil {
				read[c.ChatID] = map[string]bool{}
			}
			read[c.ChatID][domain] = true
		}
	}

	type score struct{ looked, said int }
	by := map[string]*score{}
	var looked, told int
	var silent []string

	for _, a := range answers {
		domains := read[a.ID]
		if len(domains) == 0 {
			continue
		}
		looked++
		lower := strings.ToLower(a.Response)
		named := false
		for domain := range domains {
			if by[domain] == nil {
				by[domain] = &score{}
			}
			by[domain].looked++
			if strings.Contains(lower, strings.ToLower(tool.From(domain))) {
				by[domain].said++
				named = true
			}
		}
		if named {
			told++
		} else if len(silent) < 8 {
			silent = append(silent, strings.Join(strings.Fields(a.Response), " "))
		}
	}

	if looked == 0 {
		t.Skip("no answer in the window read anything")
	}
	t.Logf("answers that looked something up   %d", looked)
	t.Logf("of those, saying where from        %d  (%d%%)", told, 100*told/looked)

	names := make([]string, 0, len(by))
	for domain := range by {
		names = append(names, domain)
	}
	sort.Strings(names)
	for _, domain := range names {
		s := by[domain]
		t.Logf("  %-10s %3d of %3d   (%q)", domain, s.said, s.looked, tool.From(domain))
	}
	for _, one := range silent {
		if len(one) > 110 {
			one = one[:109] + "…"
		}
		t.Logf("  silent: %s", one)
	}

	// No bar yet. The line went in on 19 October 2026 with 48 of 353
	// behind it, and a bar set before any traffic has run through it
	// would be a number chosen rather than measured. Fail it once
	// there is a week of answers to set it from.
	if told == 0 {
		t.Errorf("not one of %d looked-up answers said where it came from", looked)
	}
}
