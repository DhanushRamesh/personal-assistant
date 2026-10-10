package hass

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"personal-assistant/internal/base/platform/logging"
)

const entity = "assist_satellite.laptop"

// fakeHA : Home Assistant's WebSocket handshake, a subscription, then the
// messages given, after which it hangs up.
func fakeHA(t *testing.T, token string, after ...any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/websocket" {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_ = wsjson.Write(ctx, c, map[string]string{"type": "auth_required"})
		var auth map[string]string
		if wsjson.Read(ctx, c, &auth) != nil {
			return
		}
		if auth["access_token"] != token {
			_ = wsjson.Write(ctx, c, map[string]string{"type": "auth_invalid"})
			return
		}
		_ = wsjson.Write(ctx, c, map[string]string{"type": "auth_ok"})
		var sub map[string]any
		if wsjson.Read(ctx, c, &sub) != nil || sub["type"] != "subscribe_entities" {
			return
		}
		_ = wsjson.Write(ctx, c, map[string]any{"id": 1, "type": "result", "success": true})
		for _, m := range after {
			_ = wsjson.Write(ctx, c, m)
		}
	}))
}

type recorder struct {
	mu     sync.Mutex
	states []string
}

func (r *recorder) on(_, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, state)
}

func (r *recorder) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.states...)
}

// watch : Runs a watcher against srv until it has reported want states.
func watch(t *testing.T, srv *httptest.Server, token string, want int) []string {
	t.Helper()
	rec := &recorder{}
	w, err := NewWatcher(WatcherConfig{URL: srv.URL, Token: logging.Secret(token), Entities: []string{entity}, OnState: rec.on})
	if err != nil {
		t.Fatal(err)
	}
	w.wait = func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(rec.got()) < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	return rec.got()[:min(want, len(rec.got()))]
}

func TestWatcherReportsTheStateThenEachChange(t *testing.T) {
	srv := fakeHA(t, "good",
		map[string]any{"id": 1, "type": "event", "event": map[string]any{"a": map[string]any{entity: map[string]any{"s": "idle", "a": map[string]any{}}}}},
		map[string]any{"id": 1, "type": "event", "event": map[string]any{"c": map[string]any{entity: map[string]any{"+": map[string]any{"s": "listening"}}}}},
		map[string]any{"id": 1, "type": "event", "event": map[string]any{"c": map[string]any{entity: map[string]any{"+": map[string]any{"lc": 1.5}}}}},
		map[string]any{"id": 1, "type": "event", "event": map[string]any{"c": map[string]any{entity: map[string]any{"+": map[string]any{"s": "responding"}}}}},
	)
	defer srv.Close()
	got := watch(t, srv, "good", 4)
	want := []string{"idle", "listening", "responding", Unknown}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestWatcherSaysUnknownWhenTheTokenIsRefused(t *testing.T) {
	srv := fakeHA(t, "good")
	defer srv.Close()
	if got := watch(t, srv, "bad", 1); len(got) != 1 || got[0] != Unknown {
		t.Fatalf("got %v, want [unknown]", got)
	}
}

func TestWatcherNeedsSomethingToWatch(t *testing.T) {
	_, err := NewWatcher(WatcherConfig{URL: "http://x", Token: "t", OnState: func(string, string) {}})
	if err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestWebsocketURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:8123":    "ws://localhost:8123/api/websocket",
		"https://ha.example/base/": "wss://ha.example/base/api/websocket",
	} {
		if got, err := websocketURL(in); err != nil || got != want {
			t.Errorf("websocketURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := websocketURL("ftp://x"); err == nil {
		t.Error("an ftp address was accepted")
	}
}
