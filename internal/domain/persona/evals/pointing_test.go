//go:build evals

package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// listed : What it had just written out, as it wrote it on 20 October
// 2026.
const listed = "Five tasks on Secure access, sir, all still to do:\n" +
	"- Show enrolment options in Ulaa About page and unenrol button there too\n" +
	"- Provisional access close button when gateway is not running\n" +
	"- Do a health call when provisional access is set\n" +
	"- Stop try again when in account mismatch\n" +
	"- Show provisional gateway info"

// TestAWordThatPointsPointsBackwards : the fault, and the same fault
// said in another language.
//
// Read those five tasks, he said *"add these tl secuyre access spoace
// notes"* and was told "I need to see what you want to add, sir. You
// said \"add these\" but there is nothing following it." It had looked
// for the list after the word in his message rather than before it in
// the conversation.
//
// A check could not catch this. Whether something points backwards is
// a question about meaning, and the words that do it differ with the
// language: Agrees("yes") is true and Agrees("ஆம்") is false, because
// the server matches English and strips every other alphabet before
// it compares. The prompt is the only lever that works in a language
// nobody listed, and this measures whether it holds in two.
//
// Counted by whether the answer names one of the five. That works
// whatever language the reply is in, since the tasks are named the
// same either way.
func TestAWordThatPointsPointsBackwards(t *testing.T) {
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

	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Written)
	history := []environment.Turn{
		{Role: environment.RoleUser, Text: "whara re the tasks for secure accesss"},
		{Role: environment.RoleAssistant, Text: listed},
	}

	// What the fault sounds like, which is the thing being counted.
	//
	// Not whether the five are named: the first checker looked for
	// their titles and every answer failed it, because the right
	// answer says "those five" and does not recite them -- which is
	// what the listing itself asks for. An answer is wrong here only
	// when it says the subject is missing.
	missing := []string{
		"nothing following", "nothing after", "there is nothing",
		"what you want to add", "what to add", "need to see what",
		"did not say what", "didn't say what", "which ones",
	}

	for _, c := range []struct{ what, ask string }{
		{"as he typed it", "add these tl secuyre access spoace notes"},
		{"plainly", "add these to the secure access notes"},
		{"only the pointing word", "do these"},
		{"in Tamil", "இவற்றைச் சேர்"},
		{"in Hindi", "इन्हें जोड़ो"},
	} {
		answer := pointed(t, env, system, history, c.ask)
		low := strings.ToLower(answer)
		var lost string
		for _, m := range missing {
			if strings.Contains(low, m) {
				lost = m
				break
			}
		}
		if lost != "" {
			t.Errorf("%-24s asked for a subject it had just written (%q): %q",
				c.what, lost, first(answer, 120))
			continue
		}
		t.Logf("%-24s held  %q", c.what, first(answer, 120))
	}
}

// pointed : One question, with what was said before it.
func pointed(t *testing.T, env *platformai.Environment, system string,
	history []environment.Turn, ask string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{
		Prompt: ask, SystemPrompt: system, History: history,
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
	if final == nil {
		return ""
	}
	return strings.TrimSpace(final.Text)
}

// first : The opening of an answer, for a log line.
func first(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
