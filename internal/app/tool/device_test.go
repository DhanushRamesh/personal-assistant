package tool_test

import (
	"context"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
)

// TestADevicesToolCanBeDescribed : Whether tool_describe reaches a tool
// the laptop offered, which decides whether the laptop's twenty-nine
// can be deferred at all.
func TestADevicesToolCanBeDescribed(t *testing.T) {
	base, err := tool.NewRegistry(tool.Tool{
		Name: "calendar_events", Domain: "calendar", Purpose: "Read the diary.",
		UseWhen: "They ask what is on.", Channels: []chat.Channel{chat.ChannelDirect},
		Params: tool.Schema{Properties: map[string]tool.Property{}},
		Run:    func(context.Context, tool.Invocation) tool.Result { return tool.OK("none") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := base.Add(tool.Describing(base)); err != nil {
		t.Fatal(err)
	}

	withDevice := base.With(tool.Tool{
		Name: "open_app", Domain: "machine", Purpose: "Open an app on the laptop.",
		UseWhen: "They ask for an app.", Channels: []chat.Channel{chat.ChannelDirect},
		Params: tool.Schema{Properties: map[string]tool.Property{}},
		Run:    func(context.Context, tool.Invocation) tool.Result { return tool.OK("opened") },
	})

	got := withDevice.Call(context.Background(), tool.DescribeName, tool.Invocation{
		Caller: tool.Caller{UserID: "u", Channel: chat.ChannelDirect},
		Args:   []byte(`{"names":["open_app"]}`),
	})
	if !strings.Contains(strings.ToLower(got.Content), "open_app") {
		t.Errorf("the laptop's tool could not be described: %q", got.Content)
	}
	if got.Outcome != "ok" {
		t.Errorf("outcome = %q, content %q", got.Outcome, got.Content)
	}
}
