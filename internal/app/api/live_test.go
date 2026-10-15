package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"personal-assistant/internal/app/api/apitest"
	"personal-assistant/internal/app/live"
)

func liveURL(base string) string {
	return "ws" + strings.TrimPrefix(base, "http") + "/v1/live"
}

func TestLiveRefusesAConnectionWithoutAToken(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Live: live.NewHub()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, liveURL(e.Live(t)), nil)
	if err == nil {
		t.Fatal("connected without a token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestLiveSendsTheLatestStateThenEachChange(t *testing.T) {
	hub := live.NewHub()
	hub.Publish(live.Event{Kind: live.SatelliteState, State: "idle", At: time.Now()})
	e := apitest.NewWith(t, apitest.Options{Live: hub})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, liveURL(e.Live(t)), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + e.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	var got live.Event
	if err := wsjson.Read(ctx, c, &got); err != nil || got.State != "idle" || got.Kind != live.SatelliteState {
		t.Fatalf("first message = %+v, %v; want the satellite idle", got, err)
	}
	for hub.Subscribers() == 0 {
		time.Sleep(time.Millisecond)
	}
	hub.Publish(live.Event{Kind: live.SatelliteState, State: "listening", At: time.Now()})
	if err := wsjson.Read(ctx, c, &got); err != nil || got.State != "listening" {
		t.Fatalf("second message = %+v, %v; want listening", got, err)
	}
	c.Close(websocket.StatusNormalClosure, "done")
	deadline := time.Now().Add(2 * time.Second)
	for hub.Subscribers() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if hub.Subscribers() != 0 {
		t.Fatal("still subscribed after the client closed")
	}
}

// dialLive : An authenticated live connection to a fresh server.
func dialLive(t *testing.T) (*websocket.Conn, context.Context) {
	t.Helper()
	e := apitest.NewWith(t, apitest.Options{Live: live.NewHub()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, liveURL(e.Live(t)), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + e.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

type liveMessage struct {
	Type           string `json:"type"`
	Request        string `json:"request"`
	ChatID         string `json:"chat_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
}

// until : Reads messages until one of the given type arrives.
func until(t *testing.T, ctx context.Context, c *websocket.Conn, kind string) liveMessage {
	t.Helper()
	for {
		var m liveMessage
		if err := wsjson.Read(ctx, c, &m); err != nil {
			t.Fatalf("waiting for %q: %v", kind, err)
		}
		if m.Type == kind {
			return m
		}
	}
}

func TestLiveAnswersAPromptOnTheSameConnection(t *testing.T) {
	c, ctx := dialLive(t)
	if err := wsjson.Write(ctx, c, map[string]string{"type": "prompt", "request": "r1", "text": "Is the server healthy?"}); err != nil {
		t.Fatal(err)
	}
	accepted := until(t, ctx, c, "chat")
	if accepted.Request != "r1" || accepted.ChatID == "" || accepted.ConversationID == "" {
		t.Fatalf("accepted = %+v; want the request echoed with the chat and conversation", accepted)
	}
	answer := until(t, ctx, c, "answer")
	if answer.ChatID != accepted.ChatID || answer.Text == "" || answer.Reason != "stop" {
		t.Fatalf("answer = %+v; want the finished answer for chat %s", answer, accepted.ChatID)
	}
}

func TestLiveRefusesWhatCannotBeAsked(t *testing.T) {
	c, ctx := dialLive(t)
	for _, tc := range []struct {
		prompt map[string]string
		want   string
	}{
		{map[string]string{"type": "prompt", "request": "empty", "text": "   "}, "A question is required."},
		{map[string]string{"type": "prompt", "request": "elsewhere", "text": "hi", "conversation_id": "conv_nobodys"}, "No such conversation."},
	} {
		if err := wsjson.Write(ctx, c, tc.prompt); err != nil {
			t.Fatal(err)
		}
		got := until(t, ctx, c, "refused")
		if got.Request != tc.prompt["request"] || got.Message != tc.want {
			t.Errorf("refused = %+v; want %q for %s", got, tc.want, tc.prompt["request"])
		}
	}
}

func TestLiveIsAbsentWithoutAHub(t *testing.T) {
	e := apitest.New(t)
	if rec := e.Get(t, "/v1/live"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Spoken or typed is each prompt's: the same client asks both ways on
// the same connection.
func TestALivePromptSaysWhetherItWasSpoken(t *testing.T) {
	e := apitest.NewWith(t, apitest.Options{Live: live.NewHub()})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, liveURL(e.Live(t)), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + e.Token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	ask := func(request string, spoken bool) string {
		if err := wsjson.Write(ctx, c, map[string]any{"type": "prompt", "request": request, "text": "what time is it", "spoken": spoken}); err != nil {
			t.Fatal(err)
		}
		for {
			var m struct {
				Type    string `json:"type"`
				Request string `json:"request"`
				ChatID  string `json:"chat_id"`
			}
			if err := wsjson.Read(ctx, c, &m); err != nil {
				t.Fatal(err)
			}
			if m.Type == "chat" && m.Request == request {
				return m.ChatID
			}
		}
	}
	channelOf := func(id string) string {
		var got struct {
			Channel string `json:"channel"`
		}
		e.Decode(t, e.Do(t, http.MethodGet, "/v1/chats/"+id, ""), &got)
		return got.Channel
	}
	if got := channelOf(ask("r1", true)); got != "voice" {
		t.Errorf("a spoken prompt was answered as %q", got)
	}
	if got := channelOf(ask("r2", false)); got != "direct" {
		t.Errorf("a typed prompt was answered as %q", got)
	}
}
