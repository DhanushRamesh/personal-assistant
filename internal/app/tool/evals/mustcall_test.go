//go:build evals

package evals

import (
	"context"
	"fmt"
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// TestWhetherTheGatewayCanBeToldToUseATool : The pivotal question.
//
// Five times on 18 October 2026 the round after a send-back was told
// to use a tool, had the right one in front of it, and answered from
// memory again. Words have lost this argument repeatedly. tool_choice
// is the only thing left that would make it a rule, and whether this
// gateway passes it through is not something to guess at.
//
// Asked with a question that plainly needs a tool, so a refusal to
// call one is the model's and not the question's.
func TestWhetherTheGatewayCanBeToldToUseATool(t *testing.T) {
	env, specs, _ := harness(t)

	for _, choice := range []string{
		"", `"auto"`, `"required"`, `{"type":"any"}`, `{"type":"auto"}`,
		`{"type":"tool","name":"calendar_events"}`,
		`{"type":"function","function":{"name":"calendar_events"}}`,
	} {
		stream, err := env.Run(context.Background(), environment.Request{
			Prompt:      "is there anything in my diary tomorrow",
			Tools:       specs,
			MustUseTool: choice,
		})
		if err != nil {
			fmt.Printf("  tool_choice=%-9q REFUSED: %v\n", choice, err)
			continue
		}
		var last *environment.Message
		for msg := range stream {
			m := msg
			last = &m
		}
		switch {
		case last == nil:
			fmt.Printf("  tool_choice=%-9q nothing came back\n", choice)
		case last.Kind == environment.KindError:
			fmt.Printf("  tool_choice=%-9q ERROR: %s\n", choice, first(last.Text+last.Detail, 90))
		case last.Kind == environment.KindToolCalls:
			names := make([]string, 0, len(last.ToolCalls))
			for _, c := range last.ToolCalls {
				names = append(names, c.Name)
			}
			fmt.Printf("  tool_choice=%-9q called %v\n", choice, names)
		default:
			fmt.Printf("  tool_choice=%-9q answered in prose: %q\n", choice, first(last.Text, 60))
		}
	}
	fmt.Println()
}
