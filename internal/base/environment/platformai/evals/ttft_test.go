//go:build evals

// How long two models take to say their first word, on the same prompt.
//
// The first word is what a voice turn waits on: speech starts when the
// first piece arrives, not when the answer is finished. Measured at two
// prompt sizes because the question is whether a model is slower in
// itself or only slower to read, and a real turn sends about 35,000
// tokens.
//
// Behind a build tag because it costs real calls to a real service.
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

	"github.com/DhanushRamesh/personal-assistant/internal/base/config"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/base/environment/platformai"
)

// racing : The models compared, as the endpoint names them.
var racing = []platformai.ModelRef{
	{Vendor: "anthropic", ID: "claude-haiku-4-5-20251001"},
	{Vendor: "openai", ID: "gpt-5.3-codex"},
}

// rounds : How many times each model answers each prompt.
const rounds = 5

func TestHowLongTheFirstWordTakes(t *testing.T) {
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	for _, size := range []struct {
		name   string
		tokens int
	}{{"small", 0}, {"realistic_35k", 35000}} {
		prompt := question(size.tokens)
		for _, want := range racing {
			env, err := provider(cfg, want)
			if err != nil {
				t.Fatalf("%s: %v", want.ID, err)
			}
			var got []time.Duration
			for i := 0; i < rounds; i++ {
				d, err := firstWord(env, prompt)
				if err != nil {
					t.Logf("%-26s %-13s round %d: %v", want.ID, size.name, i+1, err)
				} else {
					got = append(got, d)
				}
				// Spaced because minting a token per call, twenty times
				// in a hundred seconds, is itself what gets refused.
				time.Sleep(4 * time.Second)
			}
			report(t, want.ID, size.name, got)
		}
	}
}

// question : A prompt of roughly the given token count, ending in the same
// request so the answer is the same length whatever the padding.
//
// Padded with varied prose rather than one repeated line: a run of
// identical tokens is not what a real prompt looks like and is not
// necessarily read at the same speed.
func question(tokens int) string {
	var b strings.Builder
	for i := 0; b.Len() < tokens*4; i++ {
		fmt.Fprintf(&b, "Note %d: on the %dth the kitchen light was left on until %d past, "+
			"the washing finished at %d, and nobody was home between %d and %d.\n",
			i, i%28+1, i%60, i%24, i%12, i%12+6)
	}
	b.WriteString("\nIgnore everything above. Say the word ready and nothing else.\n")
	return b.String()
}

// provider : One environment per model, reused for every round.
//
// Built once because platformai.New mints an access token, and a token
// minted per call is a request the service refuses long before the model
// has been asked anything.
func provider(cfg config.Config, want platformai.ModelRef) (*platformai.Environment, error) {
	return platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: want.Vendor, Model: want.ID,
		SystemPrompt: "Answer in one word.",
		Timeout:      120 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// firstWord : How long until the first piece of the answer arrives.
func firstWord(env *platformai.Environment, prompt string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	started := time.Now()
	stream, err := env.Run(ctx, environment.Request{
		Prompt: prompt, Purpose: environment.PurposeCondense,
	})
	if err != nil {
		return 0, err
	}

	var first time.Duration
	var failed string
	for msg := range stream {
		if first == 0 && (msg.Kind == environment.KindPartial || msg.Kind.Terminal()) {
			first = time.Since(started)
		}
		if msg.Kind == environment.KindError {
			failed = msg.Text
			if msg.Detail != "" {
				failed = msg.Detail
			}
		}
	}
	if failed != "" {
		return 0, fmt.Errorf("%s", failed)
	}
	if first == 0 {
		return 0, fmt.Errorf("no pieces at all")
	}
	return first, nil
}

// report : The middle and the worst of a model's rounds.
func report(t *testing.T, model, size string, got []time.Duration) {
	t.Helper()
	if len(got) == 0 {
		t.Logf("%-26s %-13s no usable rounds", model, size)
		return
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	t.Logf("FIRSTWORD %-26s %-13s n=%d  median=%5.0fms  min=%5.0fms  max=%5.0fms",
		model, size, len(got),
		float64(got[len(got)/2].Milliseconds()),
		float64(got[0].Milliseconds()),
		float64(got[len(got)-1].Milliseconds()))
}
