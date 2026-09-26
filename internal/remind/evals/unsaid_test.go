//go:build evals

// Package evals asks a real model whether it passes on a reminder that
// was never said.
//
// The plumbing is proved by a unit test: the block reaches the prompt and
// is marked raised exactly once. What cannot be proved there is whether
// the model actually says it, and the first two attempts it did not --
// answering "Good day, sir" to a greeting and leaving the miss out, which
// spends the one telling and loses it for good.
//
// Behind a build tag because it costs real calls to a real service.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// opened : The things somebody says that give the assistant least excuse
// to add anything. If the miss survives these it survives anything.
var opened = []string{
	"hello",
	"what is two plus two",
	"thank you",
	"good morning",
}

func TestAMissedReminderIsPassedOn(t *testing.T) {
	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
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

	due := time.Now().Add(-3 * time.Hour)
	missed := []remind.Reminder{{
		Title: "Washing",
		Body:  "Time to take the washing out.",
		DueAt: due,
	}}

	// The blocks a real turn carries. Without them this passed while the
	// same wording failed in use: the miss was crowded out by memories
	// and old exchanges, which is the shape of every actual prompt.
	system := persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name) +
		" " + conversation.Now(time.Now()) +
		" " + conversation.Whereabouts("conv_01M3D477HXQ4YNQX7BNXJZZCV0", "Evening") +
		"\n\n" + memory.Standing([]memory.Memory{
		{Subject: "Home", Body: "Lives in Chennai."},
		{Subject: "How to answer", Body: "Wants answers kept short and plain."},
	}) +
		"\n\n" + memory.Offered([]memory.Match{
		{Memory: memory.Memory{Subject: "Guitar", Body: "Likes playing guitar."}, Score: 0.41},
		{Memory: memory.Memory{Subject: "Owl City", Body: "Likes the band Owl City."}, Score: 0.38},
	}) +
		"\n\n" + memory.Quoted([]memory.Heard{
		{Exchange: memory.Exchange{
			Text: "They said: my pet name is Toofy\nYou answered: Noted, sir, spelt T-O-O-F-Y.",
			At:   time.Now().Add(-time.Hour)}},
	}, time.Local) +
		"\n\n" + remind.Unsaid(missed, time.Local)

	var told, silent int
	for _, ask := range opened {
		answer := strings.ToLower(say(t, env, system, ask))

		if strings.Contains(answer, "washing") {
			told++
			continue
		}
		silent++
		t.Errorf("%-24q said nothing about the missed reminder:\n    %s", ask, answer)
	}

	t.Logf("a reminder nobody was told about: %d of %d passed on, %d lost",
		told, len(opened), silent)
}

// say : What the assistant answers.
func say(t *testing.T, env *platformai.Environment, system, ask string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{Prompt: ask, SystemPrompt: system})
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
	return strings.TrimSpace(final.Text)
}
