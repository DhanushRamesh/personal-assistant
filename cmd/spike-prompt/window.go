//go:build spike

package main

import (
	"fmt"
	"strings"

	"personal-assistant/internal/base/records/conversation"
)

// weighWindow : What a real conversation's history costs once it has
// been planned, which is what actually goes on the wire.
//
// Separate from weighing the system prompt because the two are
// measured differently and were nearly confused: tool results are 51%
// of what a conversation *stores*, and none of it is *sent* --
// conversation.ForModel replaces every one of them with a line saying
// it ran and to call it again.
func weighWindow(said []conversation.Message, budget int) {
	before := 0
	results, speech := 0, 0
	for _, m := range said {
		before += len(m.Content)
		for _, r := range m.ToolResults {
			before += len(r.Content)
			results += len(r.Content)
		}
		speech += len(m.Content)
	}

	planned := conversation.Plan(said, conversation.Summary{}, conversation.Limits{Bytes: budget})
	after, withdrawn := 0, 0
	for _, m := range planned.Messages {
		after += len(m.Content)
		for _, r := range m.ToolResults {
			after += len(r.Content)
			if strings.Contains(r.Content, "ran here.") {
				withdrawn += len(r.Content)
			}
		}
	}

	fmt.Printf("  %d messages stored, %d in the window\n", len(said), len(planned.Messages))
	fmt.Printf("  stored      %7d chars   speech %d, tool results %d\n", before, speech, results)
	fmt.Printf("  sent        %7d chars  ~%d tokens\n", after, after/perToken)
	fmt.Printf("    of which  %7d chars   are the lines standing in for withdrawn results\n", withdrawn)
}
