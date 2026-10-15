package tool_test

import (
	"context"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
)

// one : A tool by this name, answering with its name.
func one(name string) tool.Tool {
	return tool.Tool{
		Name: name, Purpose: "Does " + name + ".", UseWhen: "Asked to.",
		Channels: []chat.Channel{chat.ChannelDirect},
		Run:      func(context.Context, tool.Invocation) tool.Result { return tool.OK(name) },
	}
}

// A chat's registry holds a device's tools beside the server's, never in
// place of one, and the server's own registry is left as it was.
func TestARegistryWithADevicesToolsKeepsTheServersOwn(t *testing.T) {
	r, err := tool.NewRegistry(one("reminder_list"))
	if err != nil {
		t.Fatal(err)
	}
	impostor := one("reminder_list")
	impostor.Run = func(context.Context, tool.Invocation) tool.Result { return tool.OK("the device's") }
	c := r.With(one("open_space"), impostor)

	if got := c.Call(context.Background(), "open_space", tool.Invocation{Caller: tool.Caller{Channel: chat.ChannelDirect}}); got.Content != "open_space" {
		t.Errorf("open_space = %q", got.Content)
	}
	if got := c.Call(context.Background(), "reminder_list", tool.Invocation{Caller: tool.Caller{Channel: chat.ChannelDirect}}); got.Content != "reminder_list" {
		t.Errorf("reminder_list = %q, want the server's own", got.Content)
	}
	if _, ok := r.Get("open_space"); ok {
		t.Error("the server's registry was changed")
	}
	if r.With() != r {
		t.Error("nothing to add should give the registry itself")
	}
}

// Asked about a device's tool, a chat's tool_describe knows it: the
// server's own said there was no such tool, and the model said it could
// not open Spotify.
func TestAChatsDescribeKnowsADevicesTools(t *testing.T) {
	r, err := tool.NewRegistry(one("reminder_list"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add(tool.Describing(r)); err != nil {
		t.Fatal(err)
	}
	c := r.With(one("open_app"))
	got := c.Call(context.Background(), tool.DescribeName, tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect}, Args: []byte(`{"names":["open_app"]}`),
	})
	if got.Outcome == "failed" {
		t.Fatalf("describe failed: %s", got.Content)
	}
	if got := r.Call(context.Background(), tool.DescribeName, tool.Invocation{
		Caller: tool.Caller{Channel: chat.ChannelDirect}, Args: []byte(`{"names":["open_app"]}`),
	}); got.Outcome != "failed" {
		t.Errorf("the server's own describe knew a device's tool: %s", got.Content)
	}
}
