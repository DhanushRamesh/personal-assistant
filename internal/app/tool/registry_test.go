package tool_test

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
)

// looker : An Elsewhere that records what it was asked.
type looker struct {
	named, except string
	say           string
}

func (l *looker) Instead(_ context.Context, _, named, except string) string {
	l.named, l.except = named, except
	return l.say
}

// A tool that looked by name and found nothing has the rest of the
// sentence added to it, from the stores it cannot reach itself.
func TestWhatOneToolCannotFindIsLookedForElsewhere(t *testing.T) {
	missing := tool.Tool{
		Name: "reminder_cancel", Purpose: "Calls off a reminder.",
		UseWhen: "They want one stopped.", Writes: true,
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Run: func(context.Context, tool.Invocation) tool.Result {
			return tool.WhichOne("reminder", "water alarm", []string{"Call the dentist"})
		},
	}
	r, err := tool.NewRegistry(missing)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	found := &looker{say: "There is, though, a rule called \"drink water\"."}
	r.Elsewhere = found

	out := r.Call(context.Background(), "reminder_cancel", tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1", Channel: chat.ChannelDirect},
		Args:   []byte(`{}`)})

	if found.named != "water alarm" || found.except != "reminder" {
		t.Errorf("searched for %q except %q, want the name and the domain that looked",
			found.named, found.except)
	}
	if !strings.Contains(out.Content, "drink water") {
		t.Errorf("what was found elsewhere never reached the model:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "Call the dentist") {
		t.Errorf("the tool's own answer was replaced rather than added to:\n%s", out.Content)
	}
}

// A tool that worked is never searched for, which is what makes this
// cost nothing on a turn that did not need it.
func TestAToolThatFoundItIsLeftAlone(t *testing.T) {
	worked := tool.Tool{
		Name: "reminder_cancel", Purpose: "Calls off a reminder.",
		UseWhen: "They want one stopped.", Writes: true,
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Run: func(context.Context, tool.Invocation) tool.Result {
			return tool.OK("Called off.")
		},
	}
	r, err := tool.NewRegistry(worked)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	found := &looker{say: "should never be added"}
	r.Elsewhere = found

	out := r.Call(context.Background(), "reminder_cancel", tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1", Channel: chat.ChannelDirect},
		Args:   []byte(`{}`)})

	if found.named != "" {
		t.Errorf("a successful call was searched for anyway, as %q", found.named)
	}
	if out.Content != "Called off." {
		t.Errorf("content = %q, want it untouched", out.Content)
	}
}
