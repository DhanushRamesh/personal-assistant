//go:build evals

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

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/platform/storage"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/persona"
)

// Does the clock beat the conversation's own stale times?
//
// Measured against his real chats on 11 October 2026: asked the time at
// 5:29 pm it answered 12:56 pm, at 5:08 pm it answered 12:39 pm, and at
// 7:43 pm it answered 6:41 pm. Each of those is a line it had written
// itself earlier in the same conversation. This is not the one-minute
// drift the clock was hardened against in October; it is whole hours,
// and the wording that fixed the drift did not touch it.
//
// The suspicion the eval exists to settle is about position rather than
// wording. The clock sat near the top, beside the manner, and every
// block that carries a time -- the transcript, the quoted exchanges,
// what was said aloud -- came after it. So both orders are asked the
// same question with the same stale history, and the difference between
// them is the whole result.

// real : His own longest conversation, as the runner would send it.
//
// Forty invented turns over one hour did not reproduce anything: both
// orders answered correctly, four times out of four, twice. The chat
// this actually happened in holds 140 messages spanning sixteen hours,
// from 4:09 am to 8:00 pm, and the times in it are spread across all of
// that. Invented history is too tidy to fail on, so this reads the real
// one. See the memory about sampling real arrivals rather than made-up
// ones; the same mistake, in a new place.
func real(t *testing.T, cfg config.Config, limit int) []environment.Turn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(ctx, cfg.Database, quiet, storage.Options{})
	if err != nil {
		t.Skipf("no database to read a real conversation from: %v", err)
	}
	defer func() { _ = db.Close() }()

	type row struct {
		Role    string
		Content string
	}
	var rows []row
	err = db.DB.WithContext(ctx).Raw(`
		SELECT role, content FROM messages
		WHERE conversation_id = (
			SELECT conversation_id FROM messages
			GROUP BY conversation_id ORDER BY COUNT(*) DESC LIMIT 1)
		ORDER BY created_at ASC LIMIT ?`, limit).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		t.Skipf("no real conversation to read: %v", err)
	}

	var history []environment.Turn
	for _, r := range rows {
		role := environment.RoleUser
		if r.Role == "assistant" {
			role = environment.RoleAssistant
		}
		history = append(history, environment.Turn{Role: role, Text: r.Content})
	}
	t.Logf("read %d real messages", len(history))
	return history
}

// spoken : The conversation itself, as the runner sends it -- real
// turns in History, not a quoted block.
//
// This is what the first version of this eval got wrong. It put the
// stale times in a recalled block only, asked both orders, and both
// answered correctly four times out of four, which proved nothing about
// a failure that happens in a chat of hundreds of messages. A time the
// assistant said as a turn is far heavier than the same words quoted
// back at it, and the failure needs the heavier one.
func spoken(now time.Time) []environment.Turn {
	var history []environment.Turn
	chat := []string{
		"what did I do yesterday", "remind me to drink water",
		"what is on my calendar", "cancel that", "thank you",
		"what is my battery", "is the laptop awake",
	}
	// Forty turns of ordinary talk with the time asked through it, the
	// last of them most recent -- which is where a model looks first.
	for i := 0; i < 20; i++ {
		if i%3 == 0 {
			at := now.Add(-time.Duration(60-i*3) * time.Minute)
			history = append(history,
				environment.Turn{Role: environment.RoleUser, Text: "what is the time"},
				environment.Turn{Role: environment.RoleAssistant,
					Text: "The time is " + at.Format("3:04 pm on Monday 2 January 2006") + ", sir."})
			continue
		}
		history = append(history,
			environment.Turn{Role: environment.RoleUser, Text: chat[i%len(chat)]},
			environment.Turn{Role: environment.RoleAssistant, Text: "Done, sir."})
	}
	return history
}

// stale : Times the assistant said earlier, as recall hands them back.
func stale(now time.Time) string {
	var heard []memory.Heard
	for _, ago := range []time.Duration{4 * time.Hour, 3 * time.Hour, 2 * time.Hour, time.Hour} {
		at := now.Add(-ago)
		heard = append(heard, memory.Heard{Exchange: memory.Exchange{
			Text: "They said: what is the time\n" +
				"You answered: The time is " +
				at.Format("3:04 pm on Monday 2 January 2006") + ", sir.",
			At: at,
		}})
	}
	return memory.Quoted(heard, now.Location())
}

func TestTheClockBeatsTheConversation(t *testing.T) {
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
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
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: 90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// The two orders, built from the same pieces so that position is
	// the only thing that differs between them.
	orders := []struct {
		name  string
		after bool // the clock goes after the history, as a turn
		build func(now time.Time) string
	}{
		{"clock first, as it was", false, func(now time.Time) string {
			return persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken) +
				" " + conversation.Now(now) +
				" " + conversation.Heard() +
				" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "The time") +
				"\n\n" + stale(now)
		}},
		{"clock last in the system prompt", false, func(now time.Time) string {
			return persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken) +
				" " + conversation.Heard() +
				" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "The time") +
				"\n\n" + stale(now) +
				"\n\n" + conversation.Now(now)
		}},
		{"clock after the history", true, func(now time.Time) string {
			return persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken) +
				" " + conversation.Heard() +
				" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "The time") +
				"\n\n" + stale(now)
		}},
	}

	// His own words, as speech-to-text delivered them.
	asking := []string{
		"what is the time",
		"is the time",
		"the time?",
		"what is the time right now",
	}

	history := real(t, cfg, 140)

	for _, order := range orders {
		var right, wrong int
		for _, ask := range asking {
			// Built per question, so the clock in the prompt and the
			// clock this checks against are the same minute.
			now := time.Now()
			behind := history
			if order.after {
				behind = append(append([]environment.Turn{}, history...),
					environment.Turn{Role: environment.RoleUser, Text: conversation.Now(now)},
					environment.Turn{Role: environment.RoleAssistant,
						Text: conversation.Acknowledged})
			}
			answer := answered(t, env, order.build(now), behind, ask)

			if saidTheTime(answer, now) {
				right++
				continue
			}
			wrong++
			t.Logf("  %-24q answered %q, and it was %s",
				ask, firstTime(answer), now.Format("3:04 pm"))
		}
		t.Logf("%-32s %d of %d right, %d stale", order.name, right, len(asking), wrong)
		if order.after && wrong > 0 {
			t.Errorf("the clock went last and %d of %d answers were still stale",
				wrong, len(asking))
		}
	}
}

// saidTheTime : Whether the answer carries the clock it was given.
//
// A minute either way, because the question and the answer are not in
// the same minute when one is asked at the turn of one.
func saidTheTime(answer string, now time.Time) bool {
	for _, d := range []time.Duration{0, time.Minute, -time.Minute} {
		if strings.Contains(strings.ToLower(answer),
			strings.ToLower(now.Add(d).Format("3:04 pm"))) {
			return true
		}
	}
	return false
}

// firstTime : The first clock time in an answer, for reporting.
func firstTime(answer string) string {
	for _, word := range strings.Fields(answer) {
		if strings.Count(word, ":") == 1 && strings.ContainsAny(word, "0123456789") {
			return strings.Trim(word, ".,")
		}
	}
	return fmt.Sprintf("%.60s", answer)
}

// answered : What the assistant answers, with the conversation behind it.
func answered(t *testing.T, env *platformai.Environment, system string,
	history []environment.Turn, ask string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, SystemPrompt: system, History: history, Tools: offered(t),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var final *environment.Message
	for msg := range stream {
		if msg.Kind.Terminal() {
			m := msg
			final = &m
		}
	}
	if final == nil || final.Kind == environment.KindError {
		t.Fatalf("%q got no usable answer", ask)
	}
	if len(final.ToolCalls) > 0 {
		return "reached for " + final.ToolCalls[0].Name
	}
	return strings.TrimSpace(final.Text)
}
