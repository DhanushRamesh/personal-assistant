package runner_test

import (
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/base/records/chat"
)

// TestEveryRequestToTheModelIsKept : For the timeline the owner asked
// for -- "when the first api call went, when the network responded" --
// each request is kept with the chat: when it went out, when it was
// heard from, and how it ended. A round begun early and not needed is
// kept as unused rather than left looking unanswered.
func TestEveryRequestToTheModelIsKept(t *testing.T) {
	h := newHarness(t, &narrating{}, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelDirect, "switch to a new conversation")
	done := h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	var tools, answered int
	for i, c := range done.Calls {
		if c.DoneAt == nil || c.Ended == "" {
			t.Fatalf("call %d never ended: %+v", i, c)
		}
		if c.Ended != chat.CallUnused && (c.HeardAt == nil || c.HeardAt.Before(c.AskedAt) || c.DoneAt.Before(*c.HeardAt)) {
			t.Errorf("call %d is out of order: %+v", i, c)
		}
		switch c.Ended {
		case chat.CallTools:
			tools++
		case chat.CallAnswered:
			answered++
		}
	}
	if tools != 1 || answered < 1 {
		t.Fatalf("calls are %+v; want one asking for tools and one answering", done.Calls)
	}
	if done.Calls[0].Ended != chat.CallTools {
		t.Errorf("the first call ended %q, want tools", done.Calls[0].Ended)
	}
}
