//go:build evals

package evals

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
)

// lookFirstDraft : A copy of the runner's lookBefore, so the suite
// measures what production sends.
//
// A copy rather than the thing itself because runner does not export
// it and should not: it is prompt text, and a second package reaching
// into it to read a sentence is how two prompts drift apart without
// anybody noticing. The drift is caught here instead -- if this stops
// matching, the number this prints stops meaning anything, which is
// what the comment on lookBefore says to check.
//
// The send-back works -- both times it fired on real turns the model
// then called the right tool -- so the content is not in question.
// What is being measured is whether saying it to every turn makes the
// model reach for a tool when none is needed.
var lookFirstDraft = prompt.Text(
	"Before you answer: if the answer turns on anything about them -- what they have, what they did, what is in their day --",
	"then a tool must have run this turn to say so, and what you remember is not that tool.",
	"The ones listed as not yet described are usable: call tool_describe with the name and then call it.",
	"If none of them fits, answer as you were going to. Plenty of questions need no tool.",
)

// mustNotReach : The eval cases that are right to answer with no tool.
// They are what over-reach would break, so they are what a change
// aimed at under-reach has to be measured against.
var mustNotReach = []string{
	"what is the capital of Australia",
	"how are you",
	"thank you",
	"what is twelve times eight",
	"I had dosa for breakfast",
	"it is raining here today",
	"I am a bit tired this evening",
	"that took longer than I expected",
}

// TestTellingItToLookDoesNotMakeItReach : Whether the send-back's
// instruction can be said up front without costing the turns that
// need no tool at all.
func TestTellingItToLookDoesNotMakeItReach(t *testing.T) {
	env, specs, _ := harness(t)

	for _, with := range []bool{false, true} {
		label := "as it is now"
		if with {
			label = "told to look first"
		}
		var reached []string
		var took time.Duration
		for _, say := range mustNotReach {
			ask := say
			if with {
				ask = prompt.Block(say, lookFirstDraft)
			}
			started := time.Now()
			called := reachedFor(t, env, specs, ask)
			took += time.Since(started)
			if called != "" {
				reached = append(reached, say+" -> "+called)
			}
		}
		fmt.Printf("\n  %-20s %d of %d wrongly reached for a tool, %.1fs\n",
			label, len(reached), len(mustNotReach), took.Seconds())
		for _, r := range reached {
			fmt.Printf("      %s\n", r)
		}
	}
	fmt.Println()
}

// reachedFor : The tool one question provoked, or empty for none.
func reachedFor(t *testing.T, env *platformai.Environment,
	specs []environment.ToolSpec, say string) string {
	t.Helper()
	stream, err := env.Run(context.Background(), environment.Request{
		Prompt: say, Tools: specs,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var called string
	for msg := range stream {
		if msg.Kind == environment.KindToolCalls && len(msg.ToolCalls) > 0 {
			called = msg.ToolCalls[0].Name
		}
	}
	return called
}
