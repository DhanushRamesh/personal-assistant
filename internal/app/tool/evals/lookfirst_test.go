//go:build evals

package evals

import (
	"context"
	"fmt"
	"testing"
	"time"

	"personal-assistant/internal/base/environment"
	"personal-assistant/internal/base/environment/platformai"
	"personal-assistant/internal/base/prompt"
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
	"One check before you answer.",
	"If any part of what you are about to say is that something does not exist, that they have none of something, or that you cannot do a thing, then it is a claim about what they have and only a tool that ran this turn can support it. What you remember cannot.",
	"The tools named but not yet described are usable: call tool_describe with the name and then call it. A tool you have not been handed the arguments for is not a tool you do not have.",
	"And if they have said something is happening, or told you something to keep, that is a thing to put somewhere rather than a remark to pass over -- whether or not they asked you to.",
	"Otherwise answer as you were going to.")

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
