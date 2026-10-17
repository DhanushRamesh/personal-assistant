package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// laptop : The tools a person's desktop app offers while it is connected.
type laptop struct{ tools []tool.Tool }

func (l laptop) For(string) []tool.Tool { return l.tools }

// noting : A scripted environment that notes the tools each chat request offered.
type noting struct {
	*scripted
	mu      sync.Mutex
	offered [][]string
	prompts []string
}

func (o *noting) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	if req.Purpose == environment.PurposeChat {
		names := make([]string, 0, len(req.Tools))
		for _, t := range req.Tools {
			names = append(names, t.Name)
		}
		o.mu.Lock()
		o.offered = append(o.offered, names)
		o.prompts = append(o.prompts, req.SystemPrompt)
		o.mu.Unlock()
	}
	return o.scripted.Run(ctx, req)
}

// A tool the person's laptop offers is described to the model beside the
// server's own, though the server's are deferred, and calling it runs it:
// a change on the laptop is not held back to read first.
func TestALaptopsToolIsOfferedAndRun(t *testing.T) {
	var opened int
	open := tool.Tool{
		Name: "open_app", Writes: true,
		Purpose: "Opens an app on the laptop.", UseWhen: "They ask to open an app.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{"app": {Type: "string", Description: "The app"}}, Required: []string{"app"}},
		Run:      func(context.Context, tool.Invocation) tool.Result { opened++; return tool.OK("Opened Spotify.") },
	}
	env := &noting{scripted: &scripted{rounds: []environment.Message{
		environment.ToolCalls([]environment.ToolCall{{ID: "call-open", Name: "open_app", Arguments: `{"app":"Spotify"}`}}),
		environment.Final("Spotify is open."),
	}}}
	h := newHarness(t, env, runner.Options{Tools: deferring(t, func() tool.Result { return tool.OK("") }), Away: laptop{[]tool.Tool{open}}})

	tk := h.submitOn(t, chat.ChannelVoice, "open Spotify")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	env.mu.Lock()
	defer env.mu.Unlock()
	if len(env.offered) == 0 {
		t.Fatal("nothing was asked")
	}
	found := false
	for _, n := range env.offered[0] {
		found = found || n == "open_app"
	}
	if !found {
		t.Errorf("first request offered %v, want open_app among them", env.offered[0])
	}
	if !strings.Contains(env.prompts[0], "laptop is connected now") || !strings.Contains(env.prompts[0], "open_app") {
		t.Error("the model was not told the laptop is connected")
	}
	if opened != 1 {
		t.Errorf("open_app ran %d times, want once", opened)
	}
}

// Only the device tools used most are described in full; the rest are
// named in the catalogue, to be asked for.
func TestALaptopsRarerToolsAreNamedNotDescribed(t *testing.T) {
	mk := func(name string) tool.Tool {
		return tool.Tool{
			Name: name, Purpose: "Does " + name + " on the laptop.", UseWhen: "Asked to.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run:      func(context.Context, tool.Invocation) tool.Result { return tool.OK("") },
		}
	}
	env := &noting{scripted: &scripted{rounds: []environment.Message{environment.Final("Done.")}}}
	h := newHarness(t, env, runner.Options{Tools: deferring(t, func() tool.Result { return tool.OK("") }),
		Away: laptop{[]tool.Tool{mk("read_terminal"), mk("change_space")}}})

	tk := h.submitOn(t, chat.ChannelVoice, "what is in the terminal")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	env.mu.Lock()
	defer env.mu.Unlock()
	got := strings.Join(env.offered[0], " ")
	if !strings.Contains(got, "read_terminal") || strings.Contains(got, "change_space") {
		t.Errorf("described %v, want read_terminal and not change_space", env.offered[0])
	}
	if !strings.Contains(env.prompts[0], "- change_space: Does change_space on the laptop.") {
		t.Error("change_space is not named in the catalogue")
	}
}
