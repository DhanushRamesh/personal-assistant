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

// TestAHelloIsAskedWithoutTools : What is said in passing cannot need a
// tool, and the block is the most expensive thing in the request.
//
// persona.InPassing already decides this before the first call -- it is
// what stops the answer being sent back to look -- and until 18 October
// 2026 the tools went out anyway: 117,923 bytes of definitions that
// could not be used, costing 1.30s to the first word and about 31,000
// tokens on every "thank you".
func TestAHelloIsAskedWithoutTools(t *testing.T) {
	for _, said := range []string{"thank you", "hello", "ok", "good morning"} {
		p := &noting{scripted: &scripted{rounds: []environment.Message{
			environment.Final("Of course, sir."),
		}}}
		h := newHarness(t, p, runner.Options{Tools: writer(true)})

		tk := h.submitOn(t, chat.ChannelDirect, said)
		h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

		p.mu.Lock()
		offered := append([][]string(nil), p.offered...)
		prompts := append([]string(nil), p.prompts...)
		p.mu.Unlock()

		if len(offered) == 0 {
			t.Fatalf("%q was never asked", said)
		}
		for i, names := range offered {
			if len(names) > 0 {
				t.Errorf("%q was offered %d tools on round %d: %v", said, len(names), i+1, names)
			}
		}
		// And the catalogue with them: naming tools it cannot call is
		// the same cost by another route.
		for _, prompt := range prompts {
			if strings.Contains(prompt, "you have not been given their arguments") {
				t.Errorf("%q was still told about the tools it was not offered", said)
			}
		}
	}
}

// TestAQuestionIsStillOfferedTools : The other half. Only what is said
// in passing loses them, and a question that might need one keeps them.
func TestAQuestionIsStillOfferedTools(t *testing.T) {
	p := &noting{scripted: &scripted{rounds: []environment.Message{
		environment.Final("Nothing tomorrow, sir."),
	}}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelDirect, "thanks, and what is in my diary tomorrow")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	p.mu.Lock()
	offered := append([][]string(nil), p.offered...)
	p.mu.Unlock()

	if len(offered) == 0 || len(offered[0]) == 0 {
		t.Errorf("a question beginning with thanks was asked with no tools: %v", offered)
	}
}
