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
)

// candidates : The models worth asking this gateway about.
//
// Named here rather than discovered, because the gateway publishes no
// list: the only way to know whether it will take a model is to send
// it one and read the refusal.
var candidates = []string{
	"claude-sonnet-4-6",
	"claude-sonnet-5-5",
	"claude-opus-5-5",
	"claude-haiku-5-5",
	"claude-haiku-4-5",
	"claude-opus-4-6",
}

// TestWhichModelsThisGatewayTakes : Not an assertion, a list.
//
// A model the gateway refuses fails every call made with it, and the
// refusal reaches the judge as an unreadable answer rather than as an
// error -- so a mistyped or withdrawn model name reads exactly like a
// pass that had nothing to say.
func TestWhichModelsThisGatewayTakes(t *testing.T) {
	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Timeout: time.Minute,
	}, quiet)
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	for _, model := range candidates {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		stream, err := env.Run(ctx, environment.Request{
			Prompt: "Reply with the single word: yes",
			Vendor: cfg.PlatformAI.Vendor, Model: model,
			SystemPrompt: "You answer in one word.",
		})
		if err != nil {
			t.Logf("%-20s refused outright: %v", model, err)
			cancel()
			continue
		}
		var last *environment.Message
		for msg := range stream {
			m := msg
			last = &m
		}
		cancel()

		switch {
		case last == nil:
			t.Logf("%-20s said nothing at all", model)
		case last.Kind == environment.KindError:
			t.Logf("%-20s NO   %s", model, strings.TrimSpace(last.Text))
		default:
			t.Logf("%-20s yes  %q", model, strings.TrimSpace(last.Text))
		}
		time.Sleep(time.Second)
	}
}
