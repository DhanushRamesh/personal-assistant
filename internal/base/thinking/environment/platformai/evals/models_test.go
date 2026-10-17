//go:build evals

// Package evals asks the endpoint which models it will answer to.
//
// A vendor the service speaks to is not the set of models it routes, and
// the only way to tell the two apart is to ask. Models() carries what has
// been asked so far; a name absent from it has not been tried rather than
// been refused, so this is how a new one gets tried.
//
// Driven through the provider rather than curl: a handmade request is
// missing whatever the client sends and comes back 401 whatever the model.
//
// Asked twice before it is called refused. One model reported rejected on
// the first attempt and answered on the second, the failure having been in
// minting the token rather than in the model.
//
// Behind a build tag because it costs real calls to a real service.
package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
)

// asking : The models to try, beyond the ones already catalogued.
var asking = []platformai.ModelRef{
	// The exact identifier is what matters and nothing else: this
	// endpoint refuses claude-haiku-4-5 and answers to the same model
	// under its dated name, and routes gpt-5 while refusing gpt-5.3.
	// So the likely spellings go in together.
	{Vendor: "anthropic", ID: "claude-sonnet-5-5"},
	{Vendor: "anthropic", ID: "claude-sonnet-5.5"},
	{Vendor: "anthropic", ID: "claude-sonnet-5"},
	{Vendor: "anthropic", ID: "claude-opus-5-5"},
	{Vendor: "anthropic", ID: "claude-haiku-5-5"},
	// Asked for again 17 October 2026, because the owner wants the
	// quickest model there is: *"we can use haiku .. i want the 3
	// second response"*. The dated spellings go in with the plain
	// ones, since this endpoint has answered to a dated name where it
	// refused the short one before.
	{Vendor: "anthropic", ID: "claude-haiku-4-5"},
	{Vendor: "anthropic", ID: "claude-haiku-4-5-20251001"},
	{Vendor: "anthropic", ID: "claude-3-5-haiku-latest"},
	{Vendor: "anthropic", ID: "claude-3-5-haiku-20241022"},
	{Vendor: "anthropic", ID: "claude-haiku-4"},
}

func TestWhichModelsTheEndpointAnswersTo(t *testing.T) {
	cfg, err := config.Load("../../../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	for _, want := range asking {
		t.Run(want.ID, func(t *testing.T) {
			var last error
			for attempt := 1; attempt <= 2; attempt++ {
				if err := ask(cfg, want); err == nil {
					t.Logf("ANSWERED  %s/%s", want.Vendor, want.ID)
					return
				} else {
					last = err
				}
				time.Sleep(2 * time.Second)
			}
			t.Logf("REFUSED   %s/%s: %v", want.Vendor, want.ID, last)
		})
	}
}

// ask : One trivial question, for whether the model answers at all.
func ask(cfg config.Config, want platformai.ModelRef) error {
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: want.Vendor, Model: want.ID,
		SystemPrompt: "Answer in one word.",
		Timeout:      90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := env.Run(ctx, environment.Request{
		Prompt:  "Say the word ready and nothing else.",
		Purpose: environment.PurposeCondense,
	})
	if err != nil {
		return err
	}

	// Any error anywhere in the stream, not just the last terminal
	// message. A refusal arrives as its own message and a terminal one
	// can follow it, so reading only the last said a refused model had
	// answered -- which is how gpt-5.3-codex was first catalogued as
	// supported when the endpoint had called it unsupported nine times.
	var answered bool
	var refused string
	for msg := range stream {
		if msg.Kind == environment.KindError {
			refused = msg.Text
			if msg.Detail != "" {
				refused = msg.Detail
			}
		}
		if msg.Kind == environment.KindPartial || msg.Kind == environment.KindFinal {
			answered = true
		}
	}
	if refused != "" {
		return errRefused{refused}
	}
	if !answered {
		return errNoAnswer
	}
	return nil
}

type errRefused struct{ why string }

func (e errRefused) Error() string { return e.why }

var errNoAnswer = errRefused{"no terminal message"}
