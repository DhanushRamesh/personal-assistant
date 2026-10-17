//go:build spike

// Command spike-eino asks one question through Eino's ReAct agent, with
// this server's gateway behind it and two real tools in front.
//
// What it is for is the three questions in einomodel's doc comment. It
// writes down what it was asked, which tools were called with what, and
// what came back, so the answer is read rather than guessed at.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/environment/einomodel"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// called : What the agent asked each tool for, in order, so that a run
// can be read back afterwards.
var called []string

func main() {
	ask := "what is on my calendar tomorrow, and what time is it now?"
	if len(os.Args) > 1 {
		ask = strings.Join(os.Args[1:], " ")
	}

	cfg, err := config.Load("config.ini", os.LookupEnv)
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	env, err := platformai.New(platformai.Config{
		ClientID:     cfg.PlatformAI.ClientID,
		ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken,
		PortalID:     cfg.PlatformAI.PortalID,
		TokenURL:     cfg.PlatformAI.TokenURL,
		ChatURL:      cfg.PlatformAI.ChatURL,
		Scope:        cfg.PlatformAI.Scope,
		RedirectURI:  cfg.PlatformAI.RedirectURI,
		Vendor:       cfg.PlatformAI.Vendor,
		Model:        cfg.PlatformAI.Model,
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Written),
		Thinking:     cfg.PlatformAI.Thinking,
		Timeout:      cfg.PlatformAI.Timeout,
	}, logger)
	if err != nil {
		fmt.Println("gateway:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: einomodel.New(env),
		ToolsConfig:      tools(ctx),
		MaxStep:          8,
	})
	if err != nil {
		fmt.Println("agent:", err)
		os.Exit(1)
	}

	started := time.Now()
	out, err := agent.Generate(ctx, []*schema.Message{
		schema.SystemMessage(persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Written)),
		schema.UserMessage(ask),
	})
	took := time.Since(started)

	fmt.Printf("\nASKED    %s\n", ask)
	fmt.Printf("TOOK     %s\n", took.Round(time.Millisecond))
	if len(called) == 0 {
		fmt.Println("TOOLS    none were called")
	} else {
		fmt.Println("TOOLS")
		for _, c := range called {
			fmt.Println("   ", c)
		}
	}
	if err != nil {
		fmt.Println("FAILED  ", err)
		os.Exit(1)
	}
	fmt.Printf("ANSWER   %s\n", strings.TrimSpace(out.Content))
}

// clockArgs, diaryArgs : What the two tools take.
type clockArgs struct{}

type diaryArgs struct {
	Day string `json:"day" jsonschema:"description=The day to read, as a date or a word like tomorrow"`
}

// compose : Two tools shaped like this server's own, answering from
// nothing, so that what is measured is the round trip and not a
// calendar.
func tools(ctx context.Context) compose.ToolsNodeConfig {
	clock, err := utils.InferTool("time_now", "The time and date right now, where the person is.",
		func(ctx context.Context, _ clockArgs) (string, error) {
			called = append(called, "time_now {}")
			return time.Now().Format("Monday 2 January 2006, 3:04 pm"), nil
		})
	must(err)
	diary, err := utils.InferTool("calendar_events", "What is in the person's calendar on a given day.",
		func(ctx context.Context, a diaryArgs) (string, error) {
			raw, _ := json.Marshal(a)
			called = append(called, "calendar_events "+string(raw))
			return "Nothing is in the calendar that day.", nil
		})
	must(err)
	return compose.ToolsNodeConfig{Tools: []tool.BaseTool{clock, diary}}
}

func must(err error) {
	if err != nil {
		fmt.Println("tool:", err)
		os.Exit(1)
	}
}
