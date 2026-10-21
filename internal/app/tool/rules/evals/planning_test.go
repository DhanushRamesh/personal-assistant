//go:build evals

// Package evals asks the thing that writes rules to write one, and
// reads what it writes.
//
// The suite that exists measures which tool the assistant reaches for.
// This measures the rule that comes out -- which is the half its own
// comment says it does not cover, and the half that matters now that
// the planner can see the laptop.
package evals

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/rules"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/job"
)

// laptop : The tools his desktop app brings, as the server makes them
// -- a read is Lists, a write is Writes, which is how the readOnlyHint
// arrives.
var laptop = []tool.Tool{
	{Name: "read_terminal", Lists: true, Purpose: "A terminal's latest output on this laptop, by name, with its Claude Code conversation."},
	{Name: "list_terminals", Lists: true, Purpose: "The terminals open on this laptop, by space."},
	{Name: "read_notes", Lists: true, Purpose: "What the owner wrote in a space's notes."},
	{Name: "ask_own_claude", Lists: true, Purpose: "Give a question to the space's own Claude Code, which only looks. Its answer is told as news when it is done."},
	{Name: "type_into_terminal", Writes: true, Purpose: "Type into Claude Code in a terminal on this laptop."},
}

func look(_, name string) (tool.Tool, bool) {
	for _, t := range laptop {
		if t.Name == name {
			return t, true
		}
	}
	// The reading tools a rule has always had.
	for _, n := range []string{"event_recent", "calendar_events", "mail_inbox", "memory_search"} {
		if n == name {
			return tool.Tool{Name: n, Lists: true}, true
		}
	}
	return tool.Tool{}, false
}

// asked : What he would say, in his own words.
var asked = []struct{ what, say string }{
	// A control. This shape has been written by the real server, so
	// if it fails here the harness is wrong and not the planner.
	{"a battery rule, which works today",
		"tell me whenever my phone battery drops below twenty per cent"},
	{"the one he described",
		"every morning check the Jarvis Server terminal, and if Claude Code is waiting on me, ask me before telling it to carry on"},
	{"waiting on a terminal",
		"when Claude Code in the Laptop client finishes, read the Secure access notes and tell me what is still outstanding"},
	{"reading only",
		"every evening at six, read the notes in Secure access and tell me what is left"},
}

// holding : Two of his rules, as the server shows them to the writer.
const holding = `His rules already, as examples of the shape:

{"when":{"wake":[{"on":{"source":"battery.level","op":"<","value":25}}]},` +
	`"perform":[{"do":"tool","use":"event_recent","with":{"kind":"battery.level","limit":1}},` +
	`{"do":"write","ask":"say what the battery is at"}]}

{"when":{"wake":[{"at":"07:30"}],"all":[{"source":"presence","op":"is","value":"present"}]},` +
	`"perform":[{"do":"tool","use":"calendar_events","with":{"days":1}},` +
	`{"do":"write","ask":"say what is on today, briefly"}]}`

// examples : His rules, unless EVAL_BARE is set.
//
// Set it to measure what the refusals alone can do: if a writer shown
// no example recovers from the sentences it is handed back, those
// sentences are carrying the shape, which is the thing worth knowing
// about them.
func examples() string {
	if os.Getenv("EVAL_BARE") != "" {
		return ""
	}
	return holding
}

func TestTheWriterPlansLaptopWork(t *testing.T) {
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
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
		Timeout: 120 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	writer := rules.Writer{
		Ask: func(ctx context.Context, system, ask string) (string, error) {
			stream, err := env.Run(ctx, environment.Request{Prompt: ask, SystemPrompt: system})
			if err != nil {
				return "", err
			}
			var final *environment.Message
			for msg := range stream {
				if msg.Kind.Terminal() {
					m := msg
					final = &m
				}
			}
			if final == nil {
				return "", context.DeadlineExceeded
			}
			if os.Getenv("SHOW") != "" {
				println("--- the writer answered ---")
				println(first(final.Text, 600))
			}
			return final.Text, nil
		},
		Kinds: func(context.Context, string) ([]event.Kind, error) {
			return []event.Kind{
				{Kind: "terminal.finished"}, {Kind: "terminal.asks"},
				{Kind: "battery.level"}, {Kind: "call.made"},
			}, nil
		},
		Reading: func(string) []string {
			var out []string
			for _, t := range laptop {
				if !t.Writes {
					out = append(out, t.Name)
				}
			}
			return append(out, "event_recent", "calendar_events", "mail_inbox", "memory_search")
		},
	}

	for _, c := range asked {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		// With examples of his own rules, which is what the server
		// passes and what a writer learns the shape from. Without
		// them it wrote {"when": {"on": ...}} and {"when": [...]},
		// both missing the wake array -- and a harness that withholds
		// what the real caller gives is measuring itself.
		when, acts, why := writer.Written(ctx,
			rules.Request{UserID: "usr_1", Asked: c.say, Holding: examples()}, look)
		cancel()

		if why != "" {
			t.Errorf("%s: refused -- %s", c.what, why)
			continue
		}
		steps := make([]string, 0, len(acts))
		for _, a := range acts {
			steps = append(steps, said(a))
		}
		// The wake and the conditions are different halves. Describe
		// is the conditions, and a rule woken by a clock or an event
		// has none -- so a log line that printed only that said
		// "nothing to check" about four correct rules.
		woken := make([]string, 0, len(when.Wake))
		for _, h := range when.Wake {
			switch {
			case h.Every != "":
				woken = append(woken, "every "+h.Every)
			case h.At != "":
				woken = append(woken, "at "+h.At)
			case h.Once != "":
				woken = append(woken, "once "+h.Once)
			case h.On != nil:
				woken = append(woken, strings.TrimSpace(h.On.Source+" "+h.On.Op+" "+stringed(h.On.Value)))
			}
			if h.After != "" {
				woken[len(woken)-1] += ", then after " + h.After
			}
		}
		t.Logf("%s\n      asked: %q\n      wakes: %s\n      if   : %s\n      steps: %s",
			c.what, c.say, strings.Join(woken, " or "), when.Describe(), strings.Join(steps, " -> "))
	}
}

// said : One step, short enough for a log line.
func said(a job.Act) string {
	switch a.Do {
	case job.DoTool:
		var with map[string]any
		_ = json.Unmarshal(a.With, &with)
		return a.Use + "(" + trim(with) + ")"
	case job.DoAsk:
		return "ask:" + first(a.Text, 44)
	case job.DoSay:
		return "say:" + first(a.Text, 36)
	case job.DoWrite:
		return "write:" + first(a.Ask, 36)
	}
	return string(a.Do)
}

func trim(with map[string]any) string {
	out := make([]string, 0, len(with))
	for k, v := range with {
		out = append(out, k+"="+first(stringed(v), 22))
	}
	return strings.Join(out, ",")
}

func stringed(v any) string {
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), `"`)
}

func first(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

var _ = chat.ChannelDirect
