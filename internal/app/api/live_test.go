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

func TestLiveIsAbsentWithoutAHub(t *testing.T) {
	e := apitest.New(t)
	if rec := e.Get(t, "/v1/live"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
