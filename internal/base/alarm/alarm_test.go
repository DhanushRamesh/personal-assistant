package alarm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/alarm"
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

// TestClearingDropsTheAlarmByItsWords : The phone finds the alarm
// by its label, so the words have to match what was sent --
// signature and all, which is why the signing happens inside.
func TestClearingDropsTheAlarmByItsWords(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		_, _ = w.Write([]byte(alarm.ExpectedOff))
	}))
	defer server.Close()

	phone := alarm.Phone{OffURL: server.URL + "/x/secret", By: "Jarvis"}
	if err := phone.Clear(context.Background(), alarm.Alarm{Label: "stretch", Key: "rem_1"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got != "stretch — by Jarvis" {
		t.Errorf("the phone was asked to drop %q", got)
	}
}

// TestARepeatingAlarmCannotBeTakenBack : Android has no way to
// remove one from outside -- dismissing skips a single occurrence
// and asking again makes a second alarm rather than changing the
// first. Saying so beats a call that reports success and leaves
// the alarm ringing every morning.
func TestARepeatingAlarmCannotBeTakenBack(t *testing.T) {
	phone := alarm.Phone{OffURL: "http://127.0.0.1:1/x", By: "Jarvis"}

	err := phone.Clear(context.Background(), alarm.Alarm{
		Label: "wake up", Days: []time.Weekday{time.Monday},
	})
	if err == nil {
		t.Fatal("claimed to have taken back a repeating alarm")
	}
	if !strings.Contains(err.Error(), "repeating") {
		t.Errorf("the reason is not clear: %v", err)
	}
}

// TestHoldsIsWhatThePhoneKeepsOnItsOwn : The same property makes a
// repeat survive this server and makes it unreachable from here.
func TestHoldsIsWhatThePhoneKeepsOnItsOwn(t *testing.T) {
	if alarm.Holds(alarm.Alarm{}) {
		t.Error("a one-off was taken for something the phone holds")
	}
	if !alarm.Holds(alarm.Alarm{Days: []time.Weekday{time.Monday}}) {
		t.Error("a repeat was not taken for something the phone holds")
	}
}

// TestTheAlarmNeverRingsBeforeTheReminder : An alarm clock holds
// no seconds, so a reminder at 34 past and 33 seconds would ring
// at 34 past -- half a minute before the thing it is for, which
// is how the alarm came to lead the voice.
func TestTheAlarmNeverRingsBeforeTheReminder(t *testing.T) {
	odd := time.Date(2026, 10, 7, 2, 34, 33, 0, time.UTC)
	if got := alarm.OnTheMinute(odd); got.Hour() != 2 || got.Minute() != 35 {
		t.Errorf("34:33 became %02d:%02d, which rings early", got.Hour(), got.Minute())
	}
}

// TestAWholeMinuteIsLeftWhereItIs : Rounding up a time already on
// the minute would push every ordinary reminder a minute late.
func TestAWholeMinuteIsLeftWhereItIs(t *testing.T) {
	exact := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	if got := alarm.OnTheMinute(exact); !got.Equal(exact) {
		t.Errorf("08:00 became %s", got)
	}
}

// TestReachesOnlyWithinADay : An alarm holds a time and not a date,
// so one asked for further ahead than a day rings on the wrong one.
//
// The boundary matters in both directions. Exactly a day ahead is the
// same clock face coming round once, which is right; a moment already
// gone rings a day late, which is the same fault backwards.
func TestReachesOnlyWithinADay(t *testing.T) {
	now := time.Date(2026, 10, 8, 14, 14, 0, 0, time.UTC)
	for _, c := range []struct {
		what string
		at   time.Time
		want bool
	}{
		{"in an hour", now.Add(time.Hour), true},
		{"later today", now.Add(6 * time.Hour), true},
		{"exactly a day", now.Add(24 * time.Hour), true},
		{"a minute past a day", now.Add(24*time.Hour + time.Minute), false},
		{"the booking two days out", time.Date(2026, 10, 10, 14, 14, 0, 0, time.UTC), false},
		{"now", now, false},
		{"a minute ago", now.Add(-time.Minute), false},
	} {
		if got := alarm.Reaches(c.at, now); got != c.want {
			t.Errorf("%s: Reaches = %v, want %v", c.what, got, c.want)
		}
	}
}
