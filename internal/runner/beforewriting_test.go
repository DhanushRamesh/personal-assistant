package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// diary : A domain with something to read and something to write, and
// a count of how often each was called.
func diary(t *testing.T, reads, writes *int) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry(
		tool.Tool{
			Name:     "calendar_events",
			Domain:   "calendar",
			Lists:    true,
			Purpose:  "Read the diary.",
			UseWhen:  "They ask what is on.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				*reads++
				return tool.OK("Sunday 4 October, 18:10: Meesaya Murukku. id ev-1.")
			},
		},
		tool.Tool{
			Name:     "calendar_update",
			Domain:   "calendar",
			Writes:   true,
			Purpose:  "Change an event.",
			UseWhen:  "They ask for one to be moved.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				*writes++
				return tool.OK("Moved it.")
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestARefusedWriteCanBeMadeOnTheNextRound : The round the guard used
// to cost.
//
// It was guess, be refused, read, write -- two rounds of a turn that
// has thirty seconds, four times in one conversation about a cinema
// booking. The server now does the reading while it refuses, so the
// round after the refusal is the write.
func TestARefusedWriteCanBeMadeOnTheNextRound(t *testing.T) {
	var reads, writes int

	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}, runner.Options{Tools: diary(t, &reads, &writes)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if reads != 1 {
		t.Errorf("the diary was read %d times, want 1: the server reads it while refusing", reads)
	}
	if writes != 1 {
		t.Errorf("the write ran %d times, want 1: the round after the refusal should be the write", writes)
	}
	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a write that went through on the second round was denied: %q", answer)
	}
}
