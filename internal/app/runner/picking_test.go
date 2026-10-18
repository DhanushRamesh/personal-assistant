package runner_test

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/nearest"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/environment"
)

// sharedWords : An embedder scoring by the words two texts have in
// common, over a vocabulary small enough to reason about.
//
// Not a model. What is under test is the wiring -- that a turn's tools
// are narrowed and that the right one survives -- not whether a vector
// is any good at meaning, which is measured against the real embedder
// in the tool suite.
type sharedWords struct{}

var vocabulary = []string{"diary", "roof", "battery", "terminal"}

func (sharedWords) Model() string   { return "shared words" }
func (sharedWords) Available() bool { return true }

func (sharedWords) vector(text string) embed.Vector {
	v := make(embed.Vector, len(vocabulary))
	low := strings.ToLower(text)
	for i, w := range vocabulary {
		if strings.Contains(low, w) {
			v[i] = 1
		}
	}
	return embed.Normalize(v)
}

func (s sharedWords) Documents(_ context.Context, texts []string) ([]embed.Vector, error) {
	out := make([]embed.Vector, 0, len(texts))
	for _, t := range texts {
		out = append(out, s.vector(t))
	}
	return out, nil
}

func (s sharedWords) Query(_ context.Context, text string) (embed.Vector, error) {
	return s.vector(text), nil
}

// manyTools : Four tools and the one that describes the others, so the
// registry actually defers and there is something to choose between.
func manyTools(t *testing.T) *tool.Registry {
	t.Helper()
	say := func(name, purpose, ask string) tool.Tool {
		return tool.Tool{
			Name: name, Domain: strings.SplitN(name, "_", 2)[0], Purpose: purpose,
			UseWhen:  "When they ask about it.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Examples: []tool.Example{{Ask: ask, Args: `{}`}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.OK("done")
			},
		}
	}
	r, err := tool.NewRegistry(
		say("calendar_events", "Read the diary.", "what is in my diary tomorrow"),
		say("memory_search", "Find what they told you.", "what did I say about the roof"),
		say("event_recent", "What their devices reported.", "how much battery is left"),
		say("read_terminal", "Read a terminal.", "what is the terminal doing"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add(tool.Describing(r)); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestATurnIsOfferedTheToolsItsQuestionIsAbout : The wiring, end to
// end. Until 18 October 2026 every turn was offered the same
// hand-written list whatever was asked; this offers the nearest, and
// the measurement behind it is in the tool suite -- 99 of 106 right
// against 85.
func TestATurnIsOfferedTheToolsItsQuestionIsAbout(t *testing.T) {
	registry := manyTools(t)
	pick := &nearest.Picker{Embed: sharedWords{}}
	if _, err := pick.Warm(context.Background(), registry.All()); err != nil {
		t.Fatal(err)
	}

	p := &noting{scripted: &scripted{rounds: []environment.Message{
		environment.Final("Nothing, sir."),
	}}}
	h := newHarness(t, p, runner.Options{
		Tools: registry, Picker: pick, ToolsNearest: 1,
	})

	tk := h.submitOn(t, chat.ChannelDirect, "what did I say about the roof")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	p.mu.Lock()
	offered := append([][]string(nil), p.offered...)
	p.mu.Unlock()
	if len(offered) == 0 {
		t.Fatal("it was never asked")
	}
	got := offered[0]
	if !has(got, "memory_search") {
		t.Errorf("the tool the question is about was not offered: %v", got)
	}
	if has(got, "calendar_events") || has(got, "event_recent") {
		t.Errorf("a tool nothing was asked about was still offered: %v", got)
	}
	// tool_describe survives whatever the ranking says, or a model
	// given names alone has no way to ask about them.
	if !has(got, tool.DescribeName) {
		t.Errorf("nothing was left to ask about the others with: %v", got)
	}
}

// TestWithoutAPickerNothingChanges : A server with no embedder, or the
// setting off, offers what it always offered.
func TestWithoutAPickerNothingChanges(t *testing.T) {
	registry := manyTools(t)
	for _, tc := range []struct {
		name string
		opts runner.Options
	}{
		{"no picker", runner.Options{Tools: registry, ToolsNearest: 1}},
		{"setting off", runner.Options{Tools: registry, Picker: &nearest.Picker{Embed: sharedWords{}}}},
	} {
		p := &noting{scripted: &scripted{rounds: []environment.Message{
			environment.Final("Nothing, sir."),
		}}}
		opts := tc.opts
		h := newHarness(t, p, opts)
		tk := h.submitOn(t, chat.ChannelDirect, "what did I say about the roof")
		h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

		p.mu.Lock()
		offered := append([][]string(nil), p.offered...)
		p.mu.Unlock()
		if len(offered) == 0 || len(offered[0]) == 0 {
			t.Errorf("%s: a turn was left with no tools at all: %v", tc.name, offered)
		}
	}
}

// TestAColdPickerOffersTheUsualList : The minute after a restart, while
// the vectors are being learnt. Nothing may be worse than it was.
func TestAColdPickerOffersTheUsualList(t *testing.T) {
	registry := manyTools(t)
	p := &noting{scripted: &scripted{rounds: []environment.Message{
		environment.Final("Nothing, sir."),
	}}}
	h := newHarness(t, p, runner.Options{
		Tools: registry, Picker: &nearest.Picker{Embed: sharedWords{}}, ToolsNearest: 1,
	})
	tk := h.submitOn(t, chat.ChannelDirect, "what did I say about the roof")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	p.mu.Lock()
	offered := append([][]string(nil), p.offered...)
	p.mu.Unlock()
	if len(offered) == 0 || len(offered[0]) == 0 {
		t.Errorf("a cold picker left a turn with no tools: %v", offered)
	}
}

func has(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
