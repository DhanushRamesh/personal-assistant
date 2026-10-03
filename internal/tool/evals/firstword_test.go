//go:build evals

package evals

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	eventmemory "github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	remindmemory "github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/events"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
)

// TestWhatTheFirstWordCosts : Where the wait before the first word
// goes.
//
// Measured on four real turns, three quarters of a voice turn was
// spent before any word of the answer existed, and the tool schemas
// are 90% of what the model is handed. This asks the same question
// with different numbers of tools in front of it and prints what
// changes, so that cutting the prompt down is aimed rather than hoped.
//
// Not an assertion. A gateway's latency moves about on its own, so a
// threshold here would fail for reasons that have nothing to do with
// this repository.
// harness : A real environment and the full voice tool list, built
// the way the runner builds them.
func harness(t *testing.T) (*platformai.Environment, []environment.ToolSpec, config.Config) {
	t.Helper()

	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), reminders.Clock{Location: time.UTC}),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
		calendar.All(nil, calendar.Clock{Location: time.UTC}),
		tasks.All(nil, tasks.Clock{Location: time.UTC}),
		mail.All(nil, mail.Clock{Location: time.UTC}),
		places.Tools(nil, places.Clock{Location: time.UTC}),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
		Timeout:      90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	address := persona.AddressFor(cfg.Assistant.Persona)
	all := make([]environment.ToolSpec, 0)
	for _, x := range registry.For(chat.ChannelVoice) {
		schema, err := tool.Narrated(x.Params, address).MarshalJSON()
		if err != nil {
			t.Fatalf("%s: %v", x.Name, err)
		}
		all = append(all, environment.ToolSpec{
			Name:        x.Name,
			Description: tool.NarratedDescription(x.Description(), address),
			Parameters:  schema,
		})
	}
	// Heaviest first, so trimming the list models what deferring the
	// heavy ones would actually do.
	sort.Slice(all, func(i, j int) bool {
		return len(all[i].Parameters)+len(all[i].Description) >
			len(all[j].Parameters)+len(all[j].Description)
	})

	return env, all, cfg
}

func TestWhatTheFirstWordCosts(t *testing.T) {
	env, all, _ := harness(t)
	_ = env

	// A question with no tool answer, so every run does the same
	// amount of thinking and only the prompt in front of it differs.
	const say = "say good evening and nothing else"

	fmt.Printf("\n  %-8s %-9s %8s %8s %8s\n", "tools", "bytes", "first", "total", "pieces")
	for _, n := range []int{len(all), 20, 8, 0} {
		offered := all
		if n < len(offered) {
			// The heaviest removed, which is what deferring does.
			offered = all[len(all)-n:]
		}
		var bytes int
		for _, s := range offered {
			bytes += len(s.Parameters) + len(s.Description)
		}

		var first, total time.Duration
		var pieces int
		for run := 0; run < 2; run++ {
			f, tt, p := once(t, env, offered, say)
			if run == 0 || f < first {
				first, total, pieces = f, tt, p
			}
		}
		fmt.Printf("  %-8d %-9d %7.2fs %7.2fs %8d\n",
			len(offered), bytes, first.Seconds(), total.Seconds(), pieces)
	}
	fmt.Println()
}

// TestWhatADifferentModelCosts : The same question, the same forty-two
// tools, a different model behind the gateway.
//
// Owner, looking at the first-word breakdown: "what if we use a faster
// model." Worth knowing exactly, because the choice is not free: a
// smaller model picks the wrong tool more often, and the tool-choice
// evals in this package are what says whether it still picks right.
//
// Not an assertion, for the same reason as above.
func TestWhatADifferentModelCosts(t *testing.T) {
	env, specs, cfg := harness(t)

	const say = "what is on my calendar tomorrow"
	fmt.Printf("\n  %-34s %8s %8s %7s %s\n", "model", "first", "total", "pieces", "asked for")
	for _, m := range []struct{ vendor, model string }{
		{cfg.PlatformAI.Vendor, cfg.PlatformAI.Model},
		{"anthropic", "claude-haiku-4-5-20251001"},
		{"anthropic", "claude-3-5-haiku-20241022"},
		{"openai", "gpt-4o-mini"},
	} {
		first, total, pieces, called, err := oneModel(env, specs, say, m.vendor, m.model)
		if err != nil {
			fmt.Printf("  %-34s %s\n", m.model, short(err))
			continue
		}
		fmt.Printf("  %-34s %7.2fs %7.2fs %7d %s\n",
			m.model, first.Seconds(), total.Seconds(), pieces, called)
	}
	fmt.Println()
}

// short : An error as one line, since a gateway refusal is prose.
func short(err error) string {
	s := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(s) > 90 {
		s = s[:90] + "..."
	}
	return s
}

// oneModel : One question put to one model, reporting when the first
// fragment landed and which tool it reached for.
func oneModel(env *platformai.Environment, specs []environment.ToolSpec,
	say, vendor, model string) (time.Duration, time.Duration, int, string, error) {
	started := time.Now()
	stream, err := env.Run(context.Background(), environment.Request{
		Prompt: say, Tools: specs, Pieces: true, Vendor: vendor, Model: model,
	})
	if err != nil {
		return 0, 0, 0, "", err
	}
	var first time.Duration
	var pieces int
	called := "nothing"
	for msg := range stream {
		switch msg.Kind {
		case environment.KindPartial:
			if pieces == 0 {
				first = time.Since(started)
			}
			pieces++
		case environment.KindToolCalls:
			if first == 0 {
				first = time.Since(started)
			}
			if len(msg.ToolCalls) > 0 {
				called = msg.ToolCalls[0].Name
			}
		case environment.KindError:
			return 0, 0, 0, "", fmt.Errorf("%s: %s", msg.Code, msg.Text)
		}
	}
	return first, time.Since(started), pieces, called, nil
}

// once : Asks one question and reports when the first fragment landed.
func once(t *testing.T, env *platformai.Environment,
	specs []environment.ToolSpec, say string) (time.Duration, time.Duration, int) {
	t.Helper()

	started := time.Now()
	stream, err := env.Run(context.Background(), environment.Request{
		Prompt: say, Tools: specs, Pieces: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var first time.Duration
	var pieces int
	for msg := range stream {
		if msg.Kind == environment.KindPartial {
			if pieces == 0 {
				first = time.Since(started)
			}
			pieces++
		}
	}
	return first, time.Since(started), pieces
}
