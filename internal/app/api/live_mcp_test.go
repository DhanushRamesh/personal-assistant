package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/app/away"
	"personal-assistant/internal/app/live"
	"personal-assistant/internal/app/tool"
)

// onTheLine : An MCP connection for a device's own server over its live
// connection: mcp messages in, everything else on the line passed over.
type onTheLine struct{ c *websocket.Conn }

func (o onTheLine) Connect(context.Context) (mcp.Connection, error) { return o, nil }
func (o onTheLine) Read(ctx context.Context) (jsonrpc.Message, error) {
	for {
		var m struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
		}
		if err := wsjson.Read(ctx, o.c, &m); err != nil {
			return nil, err
		}
		if m.Type == "mcp" {
			return jsonrpc.DecodeMessage(m.Message)
		}
	}
}
func (o onTheLine) Write(ctx context.Context, msg jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return err
	}
	return wsjson.Write(ctx, o.c, map[string]any{"type": "mcp", "message": json.RawMessage(data)})
}
func (o onTheLine) Close() error      { return nil }
func (o onTheLine) SessionID() string { return "" }

// A desktop app that says on its live connection that it holds an MCP
// server has its tools offered for its person, over that same connection,
// and called there; they go when it disconnects.
func TestAnAppOnTheLiveConnectionOffersItsTools(t *testing.T) {
	devices := away.NewSet(slog.New(slog.NewTextHandler(io.Discard, nil)))
	e := apitest.NewWith(t, apitest.Options{Live: live.NewHub(), Away: devices})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, liveURL(e.Live(t)), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + e.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	server := mcp.NewServer(&mcp.Implementation{Name: "desktop-client", Version: "0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_spaces", Description: "The spaces on this laptop.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "- Primeum (active)"}}}, nil, nil
		})
	if err := wsjson.Write(ctx, c, map[string]string{"type": "mcp-hello"}); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Connect(ctx, onTheLine{c}, nil); err != nil {
		t.Fatal(err)
	}

	var got []tool.Tool
	for got = devices.For(e.User.ID); len(got) == 0 && ctx.Err() == nil; got = devices.For(e.User.ID) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || got[0].Name != "list_spaces" {
		t.Fatalf("offered %v, want the app's list_spaces", got)
	}
	if r := got[0].Run(ctx, tool.Invocation{}); r.Content != "- Primeum (active)" {
		t.Errorf("list_spaces = %+v, want the app's answer", r)
	}

	c.Close(websocket.StatusNormalClosure, "done")
	deadline := time.Now().Add(2 * time.Second)
	for len(devices.For(e.User.ID)) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(devices.For(e.User.ID)); n != 0 {
		t.Errorf("%d tools still offered after the app disconnected", n)
	}
}
