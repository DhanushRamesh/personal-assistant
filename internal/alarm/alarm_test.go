package alarm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/alarm"
)

var six = time.Date(2026, 10, 6, 6, 5, 0, 0, time.UTC)

// answering : A phone that records what it was asked and answers
// however the test says.
func answering(t *testing.T, status int, says string) (*alarm.Phone, *string) {
	t.Helper()
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("asked with %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(says))
	}))
	t.Cleanup(server.Close)
	return &alarm.Phone{URL: server.URL + "/a/secret"}, &got
}

// TestItAsksThePhoneForTheHourAndTheWords : The phone needs the
// time in pieces and something to show when it rings.
func TestItAsksThePhoneForTheHourAndTheWords(t *testing.T) {
	phone, got := answering(t, http.StatusOK, alarm.Expected)

	if err := phone.Set(context.Background(), alarm.Alarm{
		At: six, Label: "take the tablets",
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if *got != "6|5|take the tablets|0" {
		t.Errorf("the phone was asked %q", *got)
	}
}

// TestTheWordsCannotBreakTheLine : The phone splits the body on a
// pipe and reads it as one line, so a title carrying either would
// arrive as the wrong alarm or as none.
func TestTheWordsCannotBreakTheLine(t *testing.T) {
	phone, got := answering(t, http.StatusOK, alarm.Expected)

	if err := phone.Set(context.Background(), alarm.Alarm{
		At: six, Label: "call Gunalan | the invoice,\nand the 2pm",
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if strings.Count(*got, "|") != 3 {
		t.Errorf("the words added a divider of their own: %q", *got)
	}
	if *got != "6|5|call Gunalan the invoice, and the 2pm|0" {
		t.Errorf("the words did not survive readably: %q", *got)
	}
}

// TestACommaAndColonSurvive : What a reminder actually says. These
// broke the old route and must not break this one.
func TestACommaAndColonSurvive(t *testing.T) {
	if got := alarm.Labelled("call Gunalan: the invoice, 2pm"); got != "call Gunalan: the invoice, 2pm" {
		t.Errorf("ordinary punctuation was mangled: %q", got)
	}
}

// TestNowhereToAskIsAnError : A server with no address for the
// phone cannot set alarms, and saying one was set would be a lie
// the person only finds out about at six in the morning.
func TestNowhereToAskIsAnError(t *testing.T) {
	err := alarm.Phone{}.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"})
	if err == nil {
		t.Fatal("claimed an alarm with nowhere to ask for one")
	}
	if !strings.Contains(err.Error(), "no address") {
		t.Errorf("the reason is not clear: %v", err)
	}
}

// TestARefusalIsAnError : Anything but a yes is a no.
func TestARefusalIsAnError(t *testing.T) {
	phone, _ := answering(t, http.StatusNotFound, "")
	if err := phone.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"}); err == nil {
		t.Fatal("a refusal was taken for an alarm")
	}
}

// TestSomethingElseAnsweringIsAnError : Anything can answer an
// HTTP request. A captive portal returning its own cheerful 200
// would otherwise read as an alarm that was set, which is the
// failure this whole feature exists to prevent.
func TestSomethingElseAnsweringIsAnError(t *testing.T) {
	phone, _ := answering(t, http.StatusOK, "<html>Sign in to continue</html>")

	err := phone.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"})
	if err == nil {
		t.Fatal("a stranger's 200 was taken for an alarm")
	}
	if !strings.Contains(err.Error(), "other than the phone") {
		t.Errorf("the reason is not clear: %v", err)
	}
}

// TestAnUnreachablePhoneIsAnError : Off the tailnet, asleep, or
// Tasker not running.
func TestAnUnreachablePhoneIsAnError(t *testing.T) {
	phone := alarm.Phone{URL: "http://127.0.0.1:1/a/secret", Timeout: time.Second}
	if err := phone.Set(context.Background(), alarm.Alarm{At: six, Label: "wake up"}); err == nil {
		t.Fatal("claimed an alarm on a phone that never answered")
	}
}

// TestTheStampNamesWhoeverItIsCalledNow : The name is
// configuration -- it was FRIDAY before it was Jarvis -- so the
// signature follows it rather than being written down.
func TestTheStampNamesWhoeverItIsCalledNow(t *testing.T) {
	for _, c := range []struct{ by, want string }{
		{"Jarvis", "take the tablets — by Jarvis"},
		{"FRIDAY", "take the tablets — by FRIDAY"},
		{"", "take the tablets"},
	} {
		if got := alarm.Stamp("take the tablets", c.by); got != c.want {
			t.Errorf("as %q it signed %q, wanted %q", c.by, got, c.want)
		}
	}
}

// TestADailyAlarmCarriesEveryDay : The phone holds the repeat
// itself, so it keeps ringing on a day this server is down. Sunday
// is 1 on the phone and 0 in Go, and the whole week is silently a
// day out if that is got wrong.
func TestADailyAlarmCarriesEveryDay(t *testing.T) {
	week := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday}

	got := alarm.Body(alarm.Alarm{At: six, Label: "wake up", Days: week})
	if got != "6|5|wake up|1;2;3;4;5;6;7" {
		t.Errorf("a daily alarm goes over as %q", got)
	}
}

// TestAOneOffSaysSoRatherThanSayingNothing : The phone splits the
// body into a fixed number of pieces, and a trailing empty one is
// not reliably among them.
func TestAOneOffSaysSoRatherThanSayingNothing(t *testing.T) {
	got := alarm.Body(alarm.Alarm{At: six, Label: "wake up"})
	if got != "6|5|wake up|"+alarm.Never {
		t.Errorf("a one-off goes over as %q", got)
	}
}

// TestALabelCannotEscapeTheJava : The phone builds the alarm in a
// line of Java where the label is a string literal, and reads the
// body for %variables of its own first. A quote, a backslash or a
// per cent would end the literal or name a variable, and the alarm
// would simply not be set.
func TestALabelCannotEscapeTheJava(t *testing.T) {
	got := alarm.Labelled(`pay the 50% "deposit" \ today`)

	for _, bad := range []string{`"`, `\`, "%"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s survived into the label: %q", bad, got)
		}
	}
	if got != "pay the 50 deposit today" {
		t.Errorf("the label reads as %q", got)
	}
}

// TestTheRepeatSurvivesBeingSigned : Signing the label once
// rebuilt the alarm and dropped its days, so every daily alarm
// went over as a one-off. Body was tested directly and never saw
// it; this goes through Set, which is what the server calls.
func TestTheRepeatSurvivesBeingSigned(t *testing.T) {
	phone, got := answering(t, http.StatusOK, alarm.Expected)
	phone.By = "Jarvis"

	if err := phone.Set(context.Background(), alarm.Alarm{
		At: six, Label: "stretch", Key: "rem_1",
		Days: []time.Weekday{time.Monday, time.Saturday},
	}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if *got != "6|5|stretch \u2014 by Jarvis|2;7" {
		t.Errorf("the phone was asked %q", *got)
	}
}
