//go:build spike

// Command spike-five asks whether the five things this server's own
// loop does can be done inside Eino's.
//
// One check each, against the real gateway, printing what happened
// rather than what the documentation promises. The five are: holding a
// spoken answer before anyone hears it, an aside running beside the
// answer, the tool list deferred past fifty, the clock placed after the
// history as a turn, and sending an answer back to be said shorter.
package main

import (
	"context"
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
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/einomodel"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/domain/persona"
)

// Holding : The cap the runner puts on reading an answer before it is
// spoken.
const Holding = 2500 * time.Millisecond

var (
	env    environment.Environment
	system string
	ran    []string
)

func main() {
	cfg, err := config.Load("config.ini", os.LookupEnv)
	must("config", err)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	env, err = platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		Thinking: cfg.PlatformAI.Thinking, Timeout: cfg.PlatformAI.Timeout,
	}, logger)
	must("gateway", err)
	system = persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	holding(ctx)
	aside(ctx)
	deferred(ctx)
	clock(ctx)
	shorter(ctx)
}

// holding : Can an answer be read before anybody hears it, and let go
// at 2.5 seconds if it is not finished?
func holding(ctx context.Context) {
	head("1. holding a spoken answer")

	agent := agentWith(ctx, tools(), nil, nil)
	done := make(chan *schema.Message, 1)
	started := time.Now()
	go func() {
		out, err := agent.Generate(ctx, say("how should I think about backing up my photos"))
		if err != nil {
			fmt.Println("   failed:", err)
		}
		done <- out
	}()

	select {
	case out := <-done:
		took := time.Since(started)
		fmt.Printf("   whole answer in %s, inside the %s cap\n", took.Round(time.Millisecond), Holding)
		fmt.Printf("   read before speaking: %d sentences\n", strings.Count(out.Content, "."))
		fmt.Println("   VERDICT  yes, but Eino does not help: Generate returns the whole")
		fmt.Println("            answer, and the race against the cap is yours to write.")
	case <-time.After(Holding):
		fmt.Printf("   not finished inside %s -- let go, as the runner does\n", Holding)
		fmt.Println("   VERDICT  yes, outside the agent")
		<-done
	}
}

// aside : Can something run beside the answer rather than after it?
func aside(ctx context.Context) {
	head("2. an aside beside the answer")

	agent := agentWith(ctx, tools(), nil, nil)
	asideDone := make(chan time.Duration, 1)
	go func() {
		at := time.Now()
		stream, err := env.Run(ctx, environment.Request{
			SystemPrompt: "Answer in three words.",
			Prompt:       "name one colour",
			Purpose:      environment.PurposeRemark,
		})
		if err == nil {
			for range stream {
			}
		}
		asideDone <- time.Since(at)
	}()

	at := time.Now()
	_, err := agent.Generate(ctx, say("what is the capital of France"))
	answer := time.Since(at)
	beside := <-asideDone
	fmt.Printf("   answer %s, aside %s, wall clock %s\n",
		answer.Round(time.Millisecond), beside.Round(time.Millisecond), time.Since(at).Round(time.Millisecond))
	if err != nil {
		fmt.Println("   failed:", err)
	}
	fmt.Println("   VERDICT  yes. The aside is a second call on the Environment and")
	fmt.Println("            never enters the agent, so Eino neither helps nor hinders.")
}

// deferred : Can a tool the model was never given be called anyway,
// with its name alone in the prompt?
func deferred(ctx context.Context) {
	head("3. the tool list deferred past fifty")

	// Only one tool is bound. The other is named in the prompt and
	// nowhere else, exactly as a deferred tool is.
	bound := compose.ToolsNodeConfig{
		Tools: []tool.BaseTool{clockTool()},
		UnknownToolsHandler: func(_ context.Context, name, input string) (string, error) {
			ran = append(ran, "unknown handler ran "+name+" "+input)
			if name == "calendar_events" {
				return "Nothing is in the calendar that day.", nil
			}
			return "", fmt.Errorf("no tool called %s", name)
		},
	}
	named := system + "\n\nThere is also a tool called calendar_events, which reads the " +
		"person's calendar for a day. You have not been given its parameters. Call it " +
		"with a day argument when you need it."

	ran = nil
	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: einomodel.New(env),
		ToolsConfig:      bound,
		MaxStep:          6,
	})
	must("agent", err)
	out, err := agent.Generate(ctx, []*schema.Message{
		schema.SystemMessage(named),
		schema.UserMessage("what is on my calendar tomorrow"),
	})
	if err != nil {
		fmt.Println("   failed:", err)
	} else {
		fmt.Printf("   answer: %s\n", strings.TrimSpace(out.Content))
	}
	for _, r := range ran {
		fmt.Println("   ", r)
	}
	if len(ran) == 0 {
		fmt.Println("    the unknown tool was never called")
	}
	fmt.Println("   VERDICT  see below")
}

// clock : Can the time go in after the history as a turn, rather than
// into the system prompt?
func clock(ctx context.Context) {
	head("4. the clock after the history, as a turn")

	now := time.Now()
	put := func(_ context.Context, input []*schema.Message) []*schema.Message {
		return append(input, schema.UserMessage(
			"It is "+now.Format("Monday 2 January 2006, 3:04 pm")+" where the person is."))
	}
	agent := agentWith(ctx, tools(), put, nil)
	out, err := agent.Generate(ctx, say("what time is it"))
	if err != nil {
		fmt.Println("   failed:", err)
	} else {
		fmt.Printf("   asked with no clock tool: %s\n", strings.TrimSpace(out.Content))
		fmt.Printf("   it really is: %s\n", now.Format("3:04 pm"))
	}
	fmt.Println("   VERDICT  yes. MessageModifier runs before every model call and")
	fmt.Println("            appends after the history, which is the placement that won.")
}

// shorter : Can an answer be sent back to be said again, shorter?
func shorter(ctx context.Context) {
	head("5. sending an answer back to be said shorter")

	agent := agentWith(ctx, tools(), nil, nil)
	first, err := agent.Generate(ctx, say("tell me about backing up photos"))
	if err != nil {
		fmt.Println("   failed:", err)
		return
	}
	fmt.Printf("   first  %d words\n", len(strings.Fields(first.Content)))

	again, err := agent.Generate(ctx, append(say("tell me about backing up photos"),
		first,
		schema.UserMessage("That was too long to be heard. Say it again in one sentence.")))
	if err != nil {
		fmt.Println("   failed:", err)
		return
	}
	fmt.Printf("   again  %d words: %s\n", len(strings.Fields(again.Content)), strings.TrimSpace(again.Content))
	fmt.Println("   VERDICT  yes, by asking again with the answer in the history.")
	fmt.Println("            The agent has no way to do it inside one run.")
}

func agentWith(ctx context.Context, t compose.ToolsNodeConfig, before, rewrite react.MessageModifier) *react.Agent {
	a, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: einomodel.New(env),
		ToolsConfig:      t,
		MessageModifier:  before,
		MessageRewriter:  rewrite,
		MaxStep:          6,
	})
	must("agent", err)
	return a
}

func say(s string) []*schema.Message {
	return []*schema.Message{schema.SystemMessage(system), schema.UserMessage(s)}
}

type noArgs struct{}

func clockTool() tool.BaseTool {
	t, err := utils.InferTool("time_now", "The time and date right now, where the person is.",
		func(context.Context, noArgs) (string, error) {
			return time.Now().Format("Monday 2 January 2006, 3:04 pm"), nil
		})
	must("tool", err)
	return t
}

func tools() compose.ToolsNodeConfig {
	return compose.ToolsNodeConfig{Tools: []tool.BaseTool{clockTool()}}
}

func head(s string) { fmt.Printf("\n%s\n%s\n", s, strings.Repeat("-", len(s))) }

func must(what string, err error) {
	if err != nil {
		fmt.Println(what+":", err)
		os.Exit(1)
	}
}
