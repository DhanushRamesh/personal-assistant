//go:build evals

package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
)

// mustNarrate : Questions that can only be answered by calling
// something, so every one of them should arrive with a sentence to
// say while it runs.
var mustNarrate = []string{
	"what is in my diary tomorrow",
	"when did I last speak to Amma",
	"how much battery has my phone got",
	"what have I got to do this week",
	"where was I on Tuesday",
	"remind me to call the bank at four",
	"what was I listening to yesterday",
	"is there anything in my inbox from the office",
}

// TestWhatTheNarrationCosts : whether the saying argument survives
// being described once instead of eighteen times.
//
// It is 808 bytes stapled to every tool -- 3,636 tokens of the same
// paragraph per turn, 18% of the fixed prompt. Moving it to the
// system prompt is the single cheapest thing that can be done about
// a median turn of 32,574 input tokens.
//
// It cannot simply be moved, and the comment on saidWhile says why:
// the model ignored this as a rule stated beside the tools and only
// obeyed it once the examples lived in the schema property, "where
// the model is already reading". So the saving has to be measured
// against the behaviour it was built to buy, and this is that
// measurement: does a tool call still arrive with a sentence, and is
// the sentence still any good.
func TestWhatTheNarrationCosts(t *testing.T) {
	env, specs, _ := harness(t)

	var missing, poor []string
	for _, say := range mustNarrate {
		call := calledWith(t, env, specs, say)
		if call == nil {
			continue // reaching for the right tool is measured elsewhere
		}
		said := strings.TrimSpace(firstOf(tool.TakeSaying(json.RawMessage(call.Arguments))))
		switch {
		case said == "":
			missing = append(missing, say)
		case len(strings.Fields(said)) > 4:
			poor = append(poor, fmt.Sprintf("%-40s too long (%d words): %q", say, len(strings.Fields(said)), said))
		case strings.HasSuffix(said, "?"):
			poor = append(poor, fmt.Sprintf("%-40s a question: %q", say, said))
		default:
			fmt.Printf("      %-38s %q\n", say, said)
		}
	}

	fmt.Printf("\n  narration: %d of %d arrived with a sentence, %d of those were poor\n",
		len(mustNarrate)-len(missing), len(mustNarrate), len(poor))
	fmt.Printf("  a saying is read aloud with the answer waiting behind it, so three or four words is the whole budget\n")
	for _, p := range append(missing, poor...) {
		fmt.Printf("      %s\n", p)
	}
	fmt.Println()
}

// calledWith : The first tool call one question provoked, arguments
// and all.
func calledWith(t *testing.T, env *platformai.Environment,
	specs []environment.ToolSpec, say string) *environment.ToolCall {
	t.Helper()
	stream, err := env.Run(context.Background(), environment.Request{
		Prompt: say, Tools: specs,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var out *environment.ToolCall
	for msg := range stream {
		if msg.Kind == environment.KindToolCalls && len(msg.ToolCalls) > 0 && out == nil {
			call := msg.ToolCalls[0]
			out = &call
		}
	}
	return out
}

// firstOf : The saying, dropping the rest of the arguments.
func firstOf(said string, _ json.RawMessage) string { return said }
