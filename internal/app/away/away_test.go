package away

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
)

// device : A desktop app's MCP server, joined to the set as the live
// connection would join it: what each side writes, the other is handed.
func device(t *testing.T, s *Set, userID string) (leave func()) {
	t.Helper()
	leave, _ = deviceServer(t, s, userID)
	return leave
}

// deviceServer : A device as device joins it, and its MCP server, whose tools a test changes.
func deviceServer(t *testing.T, s *Set, userID string) (leave func(), server *mcp.Server) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	server = mcp.NewServer(&mcp.Implementation{Name: "desktop-client", Version: "0"}, nil)
	type space struct {
		Space string `json:"space" jsonschema:"The space's name"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "list_spaces", Title: "List spaces", Description: "The spaces on this laptop.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "- Primeum"}}}, nil, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "open_space", Title: "Open a space", Description: "Switches to a space."},
		func(_ context.Context, _ *mcp.CallToolRequest, in space) (*mcp.CallToolResult, any, error) {
			if in.Space != "Primeum" {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "No such space."}}, IsError: true}, nil, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Switched to Primeum."}}}, nil, nil
		})

	// The device's end, written to by the set and read by the device's server.
	var deliver func(json.RawMessage)
	ready := make(chan struct{})
	app := newConn(func(_ context.Context, m json.RawMessage) error {
		<-ready
		deliver(m)
		return nil
	})
	deliver, leave = s.Join(ctx, userID, "cli_laptop", func(_ context.Context, m json.RawMessage) error {
		got, err := decode(m)
		if err != nil {
			return err
		}
		app.deliver(got)
		return nil
	})
	close(ready)
	if _, err := server.Connect(ctx, transport{app}, nil); err != nil {
		t.Fatalf("the device's server did not start: %v", err)
	}
	return leave, server
}

// offered : Waits for the device's tools to arrive, as the handshake and the listing take a moment.
func offered(t *testing.T, s *Set, userID string, n int) []tool.Tool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := s.For(userID); len(got) == n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the device's tools did not arrive: %v", s.For(userID))
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A device that says it holds an MCP server has its tools offered, as the
// assistant's own: described, its arguments its schema's, reading or
// changing as it says, reached by voice and by typing.
func TestADevicesToolsAreOfferedAsTheAssistantsOwn(t *testing.T) {
	s := NewSet(quiet())
	device(t, s, "u1")
	got := offered(t, s, "u1", 2)

	byName := map[string]tool.Tool{}
	for _, x := range got {
		byName[x.Name] = x
	}
	list, open := byName["list_spaces"], byName["open_space"]
	if !list.Lists || list.Writes || open.Lists || !open.Writes {
		t.Errorf("list reads %v/%v, open writes %v/%v; want the listing to read and the opening to write", list.Lists, list.Writes, open.Lists, open.Writes)
	}
	if open.Domain != "" || open.Params.Required[0] != "space" || open.Params.Properties["space"].Type != "string" {
		t.Errorf("open_space = %+v, want it in no domain, so it is not held back to read first, requiring a space by name", open)
	}
	if !open.Reaches(chat.ChannelVoice) || !open.Reaches(chat.ChannelDirect) {
		t.Error("a device's tool should be reachable by voice and by typing")
	}
	if len(s.For("u2")) != 0 {
		t.Error("another person was offered this person's device")
	}
	r, err := tool.NewRegistry(got...)
	if err != nil {
		t.Fatalf("the registry refused a device's tool: %v", err)
	}
	_ = r
}

// Calling one runs it on the device, and its answer, or its refusal,
// comes back as the tool's.
func TestCallingADevicesToolRunsItThere(t *testing.T) {
	s := NewSet(quiet())
	device(t, s, "u1")
	var open tool.Tool
	for _, x := range offered(t, s, "u1", 2) {
		if x.Name == "open_space" {
			open = x
		}
	}
	got := open.Run(context.Background(), tool.Invocation{Args: json.RawMessage(`{"space":"Primeum"}`)})
	if got.Outcome != conversation.OutcomeOK || got.Content != "Switched to Primeum." {
		t.Errorf("result = %+v, want it switched", got)
	}
	got = open.Run(context.Background(), tool.Invocation{Args: json.RawMessage(`{"space":"Ulaa"}`)})
	if got.Outcome != conversation.OutcomeFailed || got.Content != "No such space." {
		t.Errorf("result = %+v, want the device's refusal", got)
	}
}

// When the device's line closes, its tools go with it.
func TestADevicesToolsGoWhenItLeaves(t *testing.T) {
	s := NewSet(quiet())
	leave := device(t, s, "u1")
	offered(t, s, "u1", 2)
	leave()
	if got := s.For("u1"); len(got) != 0 {
		t.Errorf("tools still offered after leaving: %d", len(got))
	}
}

// A schema with an argument of a kind no model is asked to fill leaves the tool out.
func TestAnArgumentOfAnotherKindLeavesTheToolOut(t *testing.T) {
	if _, err := paramsOf(map[string]any{"type": "object", "properties": map[string]any{"where": map[string]any{"type": "object"}}}); err == nil {
		t.Error("an object argument was taken")
	}
	p, err := paramsOf(map[string]any{"type": "object", "properties": map[string]any{"folder": map[string]any{"type": "string", "pattern": "^/"}}, "required": []string{"folder"}})
	if err != nil || p.Properties["folder"].Pattern != "^/" || p.Properties["folder"].Description != "folder" {
		t.Errorf("params = %+v, %v", p, err)
	}
}

// decode : One MCP message from its JSON.
func decode(m json.RawMessage) (jsonrpc.Message, error) { return jsonrpc.DecodeMessage(m) }

// purposeOf : A tool offered now, by name; its Purpose, or "" when it is not offered.
func purposeOf(s *Set, userID, name string) string {
	for _, x := range s.For(userID) {
		if x.Name == name {
			return x.Purpose
		}
	}
	return ""
}

// waitFor : Waits for what is offered to come to be so.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s did not come to be", what)
}

// A tool the device adds, describes anew or takes out while connected is
// offered as it now is, without the line closing: the device says its
// tools changed, and they are listed again.
func TestADevicesToolsChangedWhileConnectedAreOfferedAsTheyAreNow(t *testing.T) {
	s := NewSet(quiet())
	_, server := deviceServer(t, s, "u1")
	offered(t, s, "u1", 2)

	mcp.AddTool(server, &mcp.Tool{Name: "zoom_page", Title: "Zoom a page", Description: "Zooms a page."},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Zoomed."}}}, nil, nil
		})
	offered(t, s, "u1", 3)

	mcp.AddTool(server, &mcp.Tool{Name: "list_spaces", Title: "List spaces", Description: "Every space, hibernating ones too.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "- Primeum"}}}, nil, nil
		})
	waitFor(t, "list_spaces described anew", func() bool {
		return strings.HasPrefix(purposeOf(s, "u1", "list_spaces"), "Every space, hibernating ones too.")
	})

	server.RemoveTools("open_space")
	offered(t, s, "u1", 2)
	if purposeOf(s, "u1", "open_space") != "" {
		t.Error("open_space still offered after the device took it out")
	}
}

// Listed again every RefreshEvery too, a device that never says its tools
// changed is offered them as they are; a listing that changes nothing keeps them.
func TestADevicesToolsAreListedAgainEveryRefresh(t *testing.T) {
	was := RefreshEvery
	RefreshEvery = 30 * time.Millisecond
	t.Cleanup(func() { RefreshEvery = was })
	s := NewSet(quiet())
	device(t, s, "u1")
	before := offered(t, s, "u1", 2)
	time.Sleep(100 * time.Millisecond)
	after := s.For("u1")
	names := func(ts []tool.Tool) string {
		var out []string
		for _, x := range ts {
			out = append(out, x.Name+"="+x.Purpose)
		}
		sort.Strings(out)
		return strings.Join(out, "|")
	}
	if names(after) != names(before) {
		t.Fatalf("listings that changed nothing changed what is offered: %s, was %s", names(after), names(before))
	}
	added, removed, changed := difference(map[string]string{"a": "1", "b": "2"}, map[string]string{"b": "3", "c": "4"})
	if strings.Join(added, ",") != "c" || strings.Join(removed, ",") != "a" || strings.Join(changed, ",") != "b" {
		t.Errorf("difference = %v %v %v, want c added, a removed, b changed", added, removed, changed)
	}
}
