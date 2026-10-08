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

	"github.com/DhanushRamesh/personal-assistant/internal/base/config"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment/platformai"
)

// asking : The models to try, beyond the ones already catalogued.
var asking = []platformai.ModelRef{
	{Vendor: "openai", ID: "gpt-5.3-codex"},
	{Vendor: "openai", ID: "gpt-5.3"},
	{Vendor: "openai", ID: "gpt-5-codex"},
	{Vendor: "openai", ID: "gpt-5"},
}

func TestWhichModelsTheEndpointAnswersTo(t *testing.T) {
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
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
