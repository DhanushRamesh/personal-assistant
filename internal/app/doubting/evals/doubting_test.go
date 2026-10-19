//go:build evals

// Package evals reads every send-back this server has ever made and
// scores a judge against what the real second look actually found.
//
// No model and no gateway. The whole set is already in the database,
// in the record each chat keeps of its own requests, so the past is
// evidence without anything having been collected for it.
package evals

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/doubting"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
)

// read : Every chat that recorded its calls, oldest first.
func read(t *testing.T) []doubting.Case {
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

	var rows []doubting.Rows
	if err := db.WithContext(context.Background()).Raw(
		`SELECT id, prompt, channel, created_at, calls FROM chats
		 WHERE calls IS NOT NULL AND calls <> '' AND calls <> '[]'
		 ORDER BY created_at`).Scan(&rows).Error; err != nil {
		t.Fatalf("reading the chats: %v", err)
	}
	cases := doubting.Cases(rows)
	if len(cases) == 0 {
		t.Skip("this server has never sent an answer back")
	}
	t.Logf("%d chats with a call record, %d of them sent back", len(rows), len(cases))
	return cases
}

// TestWhatTheSecondLookCosts : The baselines, and the shape of the set
// any judge has to beat.
func TestWhatTheSecondLookCosts(t *testing.T) {
	cases := read(t)
	ctx := context.Background()

	var looked, spoken, withDraft int
	first, last := cases[0].At, cases[len(cases)-1].At
	for _, c := range cases {
		if c.Looked {
			looked++
		}
		if c.Channel == "voice" {
			spoken++
		}
		if strings.TrimSpace(c.Draft) != "" {
			withDraft++
		}
	}
	days := last.Sub(first).Hours() / 24
	fmt.Printf("\n  %d send-backs over %.0f days, %s to %s\n",
		len(cases), days, first.Format("2 January"), last.Format("2 January"))
	fmt.Printf("  %d were owed a look, %d were not\n", looked, len(cases)-looked)
	fmt.Printf("  %d spoken, %d typed\n", spoken, len(cases)-spoken)
	fmt.Printf("  %d carry the answer that was thrown away; the rest have only the question\n\n", withDraft)

	for _, j := range []doubting.Judge{doubting.Always{}, doubting.Never{}} {
		s, err := doubting.Score(ctx, j, cases)
		if err != nil {
			t.Fatalf("%s: %v", j.Name(), err)
		}
		fmt.Printf("  %s\n", s)
	}
	fmt.Printf("\n  a judge would have to keep nearly all of the %d, and the %d needless\n", looked, len(cases)-looked)
	fmt.Printf("  ones are worth %s between them -- because the look-first round is\n",
		(time.Duration(len(cases)-looked) * doubting.Round).Round(time.Second))
	fmt.Printf("  started speculatively, so it costs only what is left of it.\n")
	fmt.Printf("  the cost of a turn is elsewhere: a tool is %s, because the model\n", "about 5s")
	fmt.Printf("  cannot speak until it has seen what the tool returned.\n\n")
}

// TestWhatTheNeedlessOnesLookLike : The questions whose second look
// found nothing, commonest first, and the ones whose look was owed.
//
// Printed rather than asserted. What a judge has to separate cannot be
// guessed at from the counts, and the first look at this is what says
// whether the question alone carries the answer or whether the thrown-
// away reply is needed too.
func TestWhatTheNeedlessOnesLookLike(t *testing.T) {
	cases := read(t)
	show := func(title string, want bool, n int) {
		seen := map[string]int{}
		for _, c := range cases {
			if c.Looked != want {
				continue
			}
			p := strings.ToLower(strings.Join(strings.Fields(c.Prompt), " "))
			if p == "" {
				p = "(nothing said)"
			}
			seen[p]++
		}
		type row struct {
			say string
			n   int
		}
		rows := make([]row, 0, len(seen))
		for s, k := range seen {
			rows = append(rows, row{s, k})
		}
		sort.Slice(rows, func(i, k int) bool {
			if rows[i].n != rows[k].n {
				return rows[i].n > rows[k].n
			}
			return rows[i].say < rows[k].say
		})
		fmt.Printf("\n  %s (%d distinct)\n", title, len(rows))
		for i, r := range rows {
			if i >= n {
				break
			}
			say := r.say
			if len(say) > 68 {
				say = say[:67] + "…"
			}
			fmt.Printf("    %3d  %s\n", r.n, say)
		}
	}
	show("the look found nothing", false, 25)
	show("the look was owed", true, 25)
	fmt.Println()
}

// TestWhatKeepingTheFirstAnswerWouldChange : The 154 send-backs where
// the second look called no tool, and what each one gave up.
//
// The rule being measured is "if the send-back called no tool, keep the
// first answer". The send-back exists to make it look; where it looked
// and found nothing to call, the second answer is the same model
// answering the same question again, with an instruction pushing it to
// go and look. That push is what produced "I said that because you have
// a note asking me to advise you to sleep" in place of the true answer
// it had already written, "You asked what O Yen Yen X was, sir, and I
// looked it up."
//
// Only the cases that carry the draft can say whether the first answer
// was better. Drafts have been kept since 18 October 2026.
func TestWhatKeepingTheFirstAnswerWouldChange(t *testing.T) {
	cases := read(t)

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

	var idle, idleWithDraft, owed int
	for _, c := range cases {
		if c.Looked {
			owed++
			continue
		}
		idle++
		if c.Draft == "" {
			continue
		}
		idleWithDraft++

		var final string
		if err := db.WithContext(context.Background()).Raw(
			`SELECT COALESCE(response, "") FROM chats WHERE id = ?`, c.ChatID).Scan(&final).Error; err != nil {
			t.Fatalf("reading the answer: %v", err)
		}

		t.Logf("\n  asked   %s\n  first   %s\n  sent    %s",
			short(c.Prompt), short(c.Draft), short(strings.TrimSpace(final)))
	}

	t.Logf("\n%d send-backs: %d were owed a look, %d were not", len(cases), owed, idle)
	t.Logf("%d of the %d carry the answer that was thrown away", idleWithDraft, idle)
	t.Logf("so the rule would have kept the first answer in %d of %d turns (%d%%)",
		idle, len(cases), 100*idle/len(cases))
}

// short : One line of a thing said, for reading a table.
func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 150 {
		return s[:149] + "…"
	}
	return s
}
