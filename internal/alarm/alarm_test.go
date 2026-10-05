package alarm_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/alarm"
)

var six = time.Date(2026, 10, 6, 6, 5, 0, 0, time.UTC)

// TestItAsksThePhoneForTheHourAndTheWords : The phone needs the time
// in pieces and something to show on the alarm. A broadcast the
// phone already knows how to catch, rather than anything cleverer.
func TestItAsksThePhoneForTheHourAndTheWords(t *testing.T) {
	var got map[string]any
	var path string
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("went without the token: %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer ha.Close()

	through := alarm.Through{URL: ha.URL, Token: "secret", Service: "notify.mobile_app_pixel_7"}
	if err := through.Set(context.Background(), six, "take the tablets"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if path != "/api/services/notify/mobile_app_pixel_7" {
		t.Errorf("asked the wrong service: %q", path)
	}
	if got["title"] != alarm.Intent {
		t.Errorf("the phone would not know what it was: %v", got["title"])
	}
	data, _ := got["data"].(map[string]any)
	if data["hour"] != float64(6) || data["minute"] != float64(5) {
		t.Errorf("the hour did not survive: %v", data)
	}
	if data["label"] != "take the tablets" {
		t.Errorf("the alarm would show nothing useful: %v", data["label"])
	}
}

// TestNowhereToAskIsAnError : A server with no notify service cannot
// reach the phone, and saying the alarm is set would be a lie the
// person only discovers at six in the morning.
func TestNowhereToAskIsAnError(t *testing.T) {
	through := alarm.Through{URL: "http://127.0.0.1:1", Token: "x"}
	err := through.Set(context.Background(), six, "wake up")
	if err == nil {
		t.Fatal("claimed an alarm with nowhere to ask for one")
	}
	if !strings.Contains(err.Error(), "no notify service") {
		t.Errorf("the reason is not clear: %v", err)
	}
}

// TestHomeAssistantRefusingIsAnError : Anything but a yes is a no.
func TestHomeAssistantRefusingIsAnError(t *testing.T) {
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ha.Close()

	through := alarm.Through{URL: ha.URL, Token: "wrong", Service: "notify.mobile_app_pixel_7"}
	if err := through.Set(context.Background(), six, "wake up"); err == nil {
		t.Fatal("a refusal was taken for an alarm")
	}
}
