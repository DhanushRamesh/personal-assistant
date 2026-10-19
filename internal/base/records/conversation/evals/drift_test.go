//go:build evals

package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// TestAFollowUpReachesTheTurnBeforeIt : Whether "say that again"
// reaches the answer it follows, with a real conversation behind it.
//
// The answer is put in the turn immediately before the question, which
// is the easiest case there is. What varies is only what sits in front
// of that pair.
//
// Measured 20 October 2026:
//
//	just the two              reached
//	20 real turns in front    reached
//	60 real turns in front    "I have no record of a surveyor's report, sir.
//	                           That was an invention on my part."
//	140 real turns in front   "I made that up, sir."
//	140 turns of filler       reached
//	60 real, bare system      "I am sorry, sir. I made that up."
//
// Three things follow. It is not length: a hundred and forty turns of
// arithmetic leave it intact. It is not the persona: a bare "you are an
// assistant" prompt disowns the answer just as readily, and still says
// "sir", which is the history being read and imitated. And the failure
// is not drifting off the subject but denying it -- the model is shown
// a turn of its own and says it invented it.
//
// What his conversation has that filler does not is a long run of
// fragmentary speech and answers that could not make it out. The model
// goes on doing what the conversation has been doing.
func TestAFollowUpReachesTheTurnBeforeIt(t *testing.T) {
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

	// Nothing a model could know without being told, so reaching it is
	// evidence of the turn before and of nothing else.
	const damp = "The surveyor found damp in the north wall, sir, " +
		"and put the repair at four thousand pounds."
	tail := []environment.Turn{
		{Role: environment.RoleUser, Text: "What did the surveyor say?"},
		{Role: environment.RoleAssistant, Text: damp},
	}
	full := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken) +
		" " + conversation.Heard() +
		" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "The house")
	bare := "You are an assistant. Answer in one short sentence."

	for _, c := range []struct {
		name    string
		history []environment.Turn
		system  string
	}{
		{"just the two", tail, full},
		{"20 real in front", append(real(t, cfg, 20), tail...), full},
		{"60 real in front", append(real(t, cfg, 60), tail...), full},
		{"140 real in front", append(real(t, cfg, 140), tail...), full},
		{"140 of filler in front", append(filler(140), tail...), full},
		{"60 real, bare system", append(real(t, cfg, 60), tail...), bare},
	} {
		answer := withoutTools(t, env, c.system, c.history, "say that again")
		reached := strings.Contains(strings.ToLower(answer), "damp")
		t.Logf("%-24s reached=%-5v %q", c.name, reached, first(answer, 110))
	}
}

// filler : Turns with nothing in them, to tell length from content.
func filler(n int) []environment.Turn {
	out := make([]environment.Turn, 0, n+1)
	for i := 0; len(out) < n; i++ {
		out = append(out,
			environment.Turn{Role: environment.RoleUser,
				Text: "What is the square root of " + strconv.Itoa(i*i+1) + "?"},
			environment.Turn{Role: environment.RoleAssistant,
				Text: "A little over " + strconv.Itoa(i) + ", sir."})
	}
	return out[:n]
}

// withoutTools : Asks with no tools, so the answer is prose.
//
// With tools offered the model reaches for one and the reply carries no
// words, which hides whether it had the answer already.
func withoutTools(t *testing.T, env *platformai.Environment, system string,
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
	return final.Text
}
