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
	if err := through.Set(context.Background(), alarm.Alarm{
		At: six, Label: "take the tablets",
		Days: []time.Weekday{time.Monday, time.Saturday},
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	if path != "/api/services/notify/mobile_app_pixel_7" {
		t.Errorf("asked the wrong service: %q", path)
	}
	if got["message"] != "command_activity" {
		t.Errorf("the companion app would not start anything: %v", got["message"])
	}
	data, _ := got["data"].(map[string]any)
	if data["intent_action"] != alarm.Action {
		t.Errorf("the phone would not know what it was: %v", data["intent_action"])
	}
	extras, _ := data["intent_extras"].(string)
	for _, want := range []string{
		"android.intent.extra.alarm.HOUR:6:int",
		"android.intent.extra.alarm.MINUTES:5:int",
		"android.intent.extra.alarm.SKIP_UI:true:boolean",
		"android.intent.extra.alarm.MESSAGE:take+the+tablets:String.urlencoded",
		"android.intent.extra.alarm.DAYS:2;7:int[]",
	} {
		if !strings.Contains(extras, want) {
			t.Errorf("missing %q from\n%s", want, extras)
		}
	}
}

// TestNowhereToAskIsAnError : A server with no notify service cannot
// reach the phone, and saying the alarm is set would be a lie the
// person only discovers at six in the morning.
func TestNowhereToAskIsAnError(t *testing.T) {
	through := alarm.Through{URL: "http://127.0.0.1:1", Token: "x"}
	err := through.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"})
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
	if err := through.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"}); err == nil {
		t.Fatal("a refusal was taken for an alarm")
	}
}

// TestTheWeekIsCountedFromSunday : Go counts Sunday as 0 and the
// phone's alarm clock counts it as 1, and the whole week is
// silently a day out if that is got wrong.
func TestTheWeekIsCountedFromSunday(t *testing.T) {
	week := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday}
	if got := alarm.Numbered(week); got != "1;2;3;4;5;6;7" {
		t.Errorf("the week is numbered %q", got)
	}
}

// TestTheLabelCannotBreakTheExtras : Extras are separated by
// commas and colons, and a reminder says things like "call
// Gunalan: the invoice, 2pm". Unencoded, that is read as two more
// extras and the alarm arrives with no name or not at all.
func TestTheLabelCannotBreakTheExtras(t *testing.T) {
	got := alarm.Extras(alarm.Alarm{At: six, Label: "call Gunalan: the invoice, 2pm"})

	if strings.Count(got, ",") != 3 {
		t.Errorf("the label added separators of its own: %s", got)
	}
	if !strings.Contains(got, "call+Gunalan%3A+the+invoice%2C+2pm:String.urlencoded") {
		t.Errorf("the label is not encoded: %s", got)
	}
}

// TestTheDaysAreTypedAsIntegers : Measured against the phone: the
// days are taken as a repeat only as int[], and are dropped in
// silence as String[] or with no type, which leaves a daily alarm
// ringing once with nothing anywhere saying why.
func TestTheDaysAreTypedAsIntegers(t *testing.T) {
	got := alarm.Extras(alarm.Alarm{At: six, Days: []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday,
	}})

	if !strings.Contains(got, "android.intent.extra.alarm.DAYS:2;3;4;5;6:int[]") {
		t.Errorf("the week would not repeat: %s", got)
	}
}

// TestAOneOffNamesNoDays : An absent DAYS is how the phone is told
// it rings once, so an empty one must not be sent as a day.
func TestAOneOffNamesNoDays(t *testing.T) {
	if got := alarm.Extras(alarm.Alarm{At: six, Label: "wake up"}); strings.Contains(got, "DAYS") {
		t.Errorf("a one-off claimed days: %s", got)
	}
}
