// Package away : The tools a person's own devices offer the assistant, over
// MCP.
//
// A desktop client on the live connection holds an MCP server: the tools
// for what it can see and do on that machine -- its spaces, its apps, what
// it may open. When it says so, the server becomes its MCP client over that
// same connection, asks it what tools it has, and offers them to the
// assistant for that person's chats, for as long as it stays connected.
// Nothing new is opened to the network: the messages ride the connection
// the client already holds (owner, 15 October 2026: "I think we should have
// like MCP in the app").
package away

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
)

// CallTimeout : How long a call to a device's tool may take before the
// assistant is told it did not answer. Short of a turn's thirty seconds,
// so the model still has time to say so.
var CallTimeout = 20 * time.Second

// HelloTimeout : How long a device has to answer the opening handshake and
// list its tools.
var HelloTimeout = 10 * time.Second

// RefreshEvery : How often a connected device's tools are listed again, so
// one added, taken out or described anew is offered as it now is, without
// the line closing first (owner, 21 October 2026: "whenever descriptions
// are being added, deleted, or updated, refresh the tool definitions to
// match"). A device that says its tools changed is listed at once.
var RefreshEvery = 5 * time.Minute

// Send : Writes one MCP message to the device, wrapped as the live
// connection carries it.
type Send func(ctx context.Context, message json.RawMessage) error

// Set : The devices connected now that offer tools, by person and client.
// Safe for concurrent use.
type Set struct {
	logger *slog.Logger
	mu     sync.Mutex
	lines  map[string]map[string]*line
}

// NewSet : An empty set.
func NewSet(logger *slog.Logger) *Set {
	return &Set{logger: logger, lines: map[string]map[string]*line{}}
}

// line : One device's MCP session, and the tools it offered.
type line struct {
	conn    *conn
	session *mcp.ClientSession
	tools   []tool.Tool
	// kept : Each tool as the device last described it, by name, to tell
	// whether a listing changed anything.
	kept map[string]string
	// changed : The device said its tools changed; done : the line closed.
	changed chan struct{}
	done    chan struct{}
	ending  sync.Once
}

// Join : Becomes the MCP client of a device that has said it holds a
// server. Its messages from the device are handed to deliver; leave ends
// the session, as the live connection closes. The handshake and the
// listing run on their own, since their answers arrive through deliver.
func (s *Set) Join(ctx context.Context, userID, clientID string, send func(context.Context, json.RawMessage) error) (deliver func(json.RawMessage), leave func()) {
	c := newConn(send)
	l := &line{conn: c, changed: make(chan struct{}, 1), done: make(chan struct{})}
	log := s.logger.With(slog.String("client_id", clientID))
	go s.open(ctx, userID, clientID, l, log)
	deliver = func(raw json.RawMessage) {
		msg, err := jsonrpc.DecodeMessage(raw)
		if err != nil {
			log.WarnContext(ctx, "away: a message that is not MCP", slog.Any("error", err))
			return
		}
		c.deliver(msg)
	}
	leave = func() {
		l.ending.Do(func() { close(l.done) })
		s.mu.Lock()
		if s.lines[userID][clientID] == l {
			delete(s.lines[userID], clientID)
		}
		s.mu.Unlock()
		if l.session != nil {
			_ = l.session.Close()
		}
		_ = c.Close()
	}
	return deliver, leave
}

// open : The handshake, then the listing, kept once both are done; then
// the tools are listed again every RefreshEvery, and at once when the
// device says they changed.
func (s *Set) open(ctx context.Context, userID, clientID string, l *line, log *slog.Logger) {
	hello, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "assistant", Version: "1"}, &mcp.ClientOptions{
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			select {
			case l.changed <- struct{}{}:
			default:
			}
		},
	})
	session, err := client.Connect(hello, transport{l.conn}, nil)
	if err != nil {
		log.WarnContext(ctx, "away: the device did not answer its handshake", slog.Any("error", err))
		return
	}
	l.session = session
	tools, kept, err := listed(hello, session, log)
	if err != nil {
		log.WarnContext(ctx, "away: the device did not list its tools", slog.Any("error", err))
		return
	}
	l.tools, l.kept = tools, kept
	s.mu.Lock()
	if s.lines[userID] == nil {
		s.lines[userID] = map[string]*line{}
	}
	s.lines[userID][clientID] = l
	s.mu.Unlock()
	log.InfoContext(ctx, "away: tools offered", slog.Int("tools", len(l.tools)))
	go s.watch(ctx, l, session, log)
}

// watch : Lists a device's tools again every RefreshEvery, and when it
// says they changed, until its line closes; what changed replaces them.
func (s *Set) watch(ctx context.Context, l *line, session *mcp.ClientSession, log *slog.Logger) {
	every := time.NewTicker(RefreshEvery)
	defer every.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.done:
			return
		case <-every.C:
		case <-l.changed:
		}
		s.refresh(ctx, l, session, log)
	}
}

// refresh : The device's tools listed again; kept only when one was added,
// taken out or described anew, and said so.
func (s *Set) refresh(ctx context.Context, l *line, session *mcp.ClientSession, log *slog.Logger) {
	list, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	tools, kept, err := listed(list, session, log)
	if err != nil {
		select {
		case <-l.done:
		default:
			log.WarnContext(ctx, "away: the device did not list its tools again", slog.Any("error", err))
		}
		return
	}
	s.mu.Lock()
	added, removed, changed := difference(l.kept, kept)
	if len(added)+len(removed)+len(changed) > 0 {
		l.tools, l.kept = tools, kept
	}
	s.mu.Unlock()
	if len(added)+len(removed)+len(changed) > 0 {
		log.InfoContext(ctx, "away: tools refreshed", slog.Int("tools", len(tools)), slog.Any("added", added), slog.Any("removed", removed), slog.Any("changed", changed))
	}
}

// listed : A device's tools as the assistant's own, and each as the device
// describes it, by name; one the assistant cannot take is left out.
func listed(ctx context.Context, session *mcp.ClientSession, log *slog.Logger) ([]tool.Tool, map[string]string, error) {
	got, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, nil, err
	}
	var tools []tool.Tool
	kept := map[string]string{}
	for _, t := range got.Tools {
		x, err := assistantTool(t, session)
		if err != nil {
			log.WarnContext(ctx, "away: a tool left out", slog.String("tool", t.Name), slog.Any("error", err))
			continue
		}
		tools = append(tools, x)
		described, _ := json.Marshal(t)
		kept[t.Name] = string(described)
	}
	return tools, kept, nil
}

// difference : The tools added, taken out and described anew, by name, from was to now.
func difference(was, now map[string]string) (added, removed, changed []string) {
	for name, d := range now {
		if w, ok := was[name]; !ok {
			added = append(added, name)
		} else if w != d {
			changed = append(changed, name)
		}
	}
	for name := range was {
		if _, ok := now[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return added, removed, changed
}

// For : The tools a person's connected devices offer now, the first
// device's where two offer one by the same name.
func (s *Set) For(userID string) []tool.Tool {
	if s == nil || userID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []tool.Tool
	seen := map[string]bool{}
	for _, l := range s.lines[userID] {
		for _, t := range l.tools {
			if !seen[t.Name] {
				seen[t.Name] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// assistantTool : An MCP tool as one of the assistant's own: described as
// the device describes it, its arguments as its schema gives them, and
// run by calling the device. One that only reads is a listing, and one
// that changes something a write; but in no domain, so a write is not
// held back to read first. The device finds what is asked for by name and
// says what there is when nothing is called that, which is all the read
// guards; held back, open_app read the spaces instead, never reached the
// laptop, and the model said Spotify was running (15 October 2026).
func assistantTool(t *mcp.Tool, session *mcp.ClientSession) (tool.Tool, error) {
	purpose := strings.TrimSpace(t.Description)
	if purpose == "" {
		return tool.Tool{}, errors.New("it says nothing of what it does")
	}
	params, err := paramsOf(t.InputSchema)
	if err != nil {
		return tool.Tool{}, err
	}
	reads := t.Annotations != nil && t.Annotations.ReadOnlyHint
	title := t.Title
	if title == "" {
		title = strings.ReplaceAll(t.Name, "_", " ")
	}
	name := t.Name
	return tool.Tool{
		Name:     name,
		Purpose:  purpose + " This acts on the person's own Linux laptop, through the desktop app running on it, whichever device they are speaking from -- the room's speaker, the phone or the laptop itself.",
		UseWhen:  "The person asks to " + strings.ToLower(title) + ", by voice or typing: it happens on their laptop.",
		Lists:    reads,
		Writes:   !reads,
		Params:   params,
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args map[string]any
			if len(in.Args) > 0 {
				if err := json.Unmarshal(in.Args, &args); err != nil {
					return tool.Failed("The arguments could not be read: " + err.Error())
				}
			}
			call, cancel := context.WithTimeout(ctx, CallTimeout)
			defer cancel()
			got, err := session.CallTool(call, &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil {
				return tool.Failed("The laptop did not answer: " + err.Error())
			}
			text := textOf(got.Content)
			if got.IsError {
				return tool.Failed(text)
			}
			return tool.OK(text)
		},
	}, nil
}

// paramsOf : A tool's JSON Schema as the assistant's own schema: its
// properties of the four kinds a model is asked to fill, and which are
// required. A property of any other kind leaves the tool out, rather than
// offering it with an argument nothing can give.
func paramsOf(schema any) (tool.Schema, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return tool.Schema{}, err
	}
	var s struct {
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
			Pattern     string   `json:"pattern"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return tool.Schema{}, err
	}
	out := tool.Schema{Properties: map[string]tool.Property{}, Required: s.Required}
	for name, p := range s.Properties {
		switch p.Type {
		case "string", "integer", "number", "boolean":
		default:
			return tool.Schema{}, fmt.Errorf("%s is of a kind not offered: %q", name, p.Type)
		}
		description := strings.TrimSpace(p.Description)
		if description == "" {
			description = strings.ReplaceAll(name, "_", " ")
		}
		out.Properties[name] = tool.Property{Type: p.Type, Description: description, Enum: p.Enum, Pattern: p.Pattern}
	}
	return out, nil
}

// textOf : The words of a tool's answer.
func textOf(content []mcp.Content) string {
	var parts []string
	for _, c := range content {
		if t, ok := c.(*mcp.TextContent); ok && strings.TrimSpace(t.Text) != "" {
			parts = append(parts, t.Text)
		}
	}
	if len(parts) == 0 {
		return "Done."
	}
	return strings.Join(parts, "\n")
}

// transport : An MCP transport whose one connection is the device's line.
type transport struct{ c *conn }

func (t transport) Connect(context.Context) (mcp.Connection, error) { return t.c, nil }

// conn : MCP messages to and from a device over the live connection:
// written through send, read as deliver hands them in.
type conn struct {
	send   Send
	in     chan jsonrpc.Message
	closed chan struct{}
	once   sync.Once
}

func newConn(send Send) *conn {
	return &conn{send: send, in: make(chan jsonrpc.Message, 16), closed: make(chan struct{})}
}

// deliver : A message from the device, waiting to be read; dropped once closed.
func (c *conn) deliver(m jsonrpc.Message) {
	select {
	case c.in <- m:
	case <-c.closed:
	}
}

func (c *conn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case m := <-c.in:
		return m, nil
	case <-c.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *conn) Write(ctx context.Context, m jsonrpc.Message) error {
	select {
	case <-c.closed:
		return io.ErrClosedPipe
	default:
	}
	data, err := jsonrpc.EncodeMessage(m)
	if err != nil {
		return err
	}
	return c.send(ctx, data)
}

func (c *conn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *conn) SessionID() string { return "" }
