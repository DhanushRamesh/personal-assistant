// Package alarm sets a real alarm on the person's own phone.
//
// A reminder is a thing the assistant says; an alarm is a thing the
// phone does. The difference matters at six in the morning, when the
// assistant is a speaker in a room that may be off, the person is
// asleep, and only the phone in their hand is going to work.
//
// Owner: *"whenever a reminder is set in Jarvis, that reminder
// should be kept as a phone alarm, and if it could not reach it
// should say I could not set the alarm."* So every reminder tries,
// and a failure is told rather than swallowed -- somebody who
// believes an alarm is set and finds it was not has been worse
// served than somebody told plainly that it was not.
//
// Nothing on the phone has to be set up for this. Home Assistant's
// companion app starts the clock's own activity, so there is no
// Tasker profile in the path to be edited, exported, or quietly
// broken by a reinstall.
package alarm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// Action : Android's own "set an alarm" intent, which every clock
	// app implements.
	Action = "android.intent.action.SET_ALARM"

	// Clock : The app the alarm is set in.
	//
	// Named rather than left to Android to resolve, because the
	// companion app requires a package and because the alarm has to
	// land in the same list the person already looks at. A different
	// phone with a different clock needs this changed.
	Clock = "com.google.android.deskclock"
)

// Alarm : What the phone is being asked for.
type Alarm struct {
	// At : When it goes off. Only the hour and the minute survive the
	// trip, because that is all an alarm clock holds.
	At time.Time
	// Label : What shows on the phone when it rings. Somebody woken
	// at six wants the words, not a number.
	Label string
	// Days : The days it repeats on, empty for one time only.
	//
	// The phone's own repeat rather than a fresh alarm each morning,
	// so it still rings on a day this server is down -- which is the
	// whole reason for putting it on the phone.
	Days []time.Weekday
}

// Setter : Somewhere an alarm can be asked for.
type Setter interface {
	// Set : Asks the phone for an alarm. Returns an error where the
	// asking failed, which is not the same as the phone having
	// refused.
	Set(ctx context.Context, a Alarm) error
}

// Through : An alarm asked for through Home Assistant's notify
// service for the phone.
type Through struct {
	// URL : Where Home Assistant is.
	URL string
	// Token : A long-lived access token.
	Token string
	// Service : The notify service for the phone, such as
	// notify.mobile_app_pixel_7. Empty means there is nowhere to ask,
	// and asking fails rather than pretending.
	Service string
	// Timeout : How long the call may take. Zero selects a sensible
	// one.
	Timeout time.Duration
	// Client : Replaceable in tests.
	Client *http.Client
}

// DefaultTimeout : How long to wait on Home Assistant.
const DefaultTimeout = 10 * time.Second

// Set : Asks the phone to set an alarm.
//
// What this can and cannot know is worth being plain about. A call
// that succeeds means Home Assistant accepted the message and
// handed it to Google, not that the phone is awake, has signal, or
// did anything with it. So the honest claim afterwards is that the
// alarm was asked for -- and a failure here is a failure that can be
// told.
func (t Through) Set(ctx context.Context, a Alarm) error {
	if strings.TrimSpace(t.Service) == "" {
		return fmt.Errorf("alarm: no notify service is configured for the phone")
	}
	name := strings.TrimPrefix(strings.TrimSpace(t.Service), "notify.")

	body, err := json.Marshal(map[string]any{
		"message": "command_activity",
		"data": map[string]any{
			"intent_action":       Action,
			"intent_package_name": Clock,
			"intent_extras":       Extras(a),
		},
	})
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}

	wait := t.Timeout
	if wait <= 0 {
		wait = DefaultTimeout
	}
	ask, done := context.WithTimeout(ctx, wait)
	defer done()

	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		strings.TrimRight(t.URL, "/")+"/api/services/notify/"+name, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")

	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: wait}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("alarm: home assistant answered %s", res.Status)
	}
	return nil
}

// Extras : The alarm written the way the companion app passes
// intent extras on -- "name:value" pairs separated by commas, with
// a type where guessing would get it wrong.
//
// The types are not decoration. Measured against the phone: the
// days arrive as a repeat only as int[], and are silently dropped
// as String[] or with no type at all, which leaves a daily alarm
// ringing once and nothing anywhere saying why. The label is
// url-encoded because a reminder says things like "call Gunalan:
// the invoice, 2pm" and the comma and colon are this format's own
// punctuation.
func Extras(a Alarm) string {
	out := []string{
		"android.intent.extra.alarm.HOUR:" + strconv.Itoa(a.At.Hour()) + ":int",
		"android.intent.extra.alarm.MINUTES:" + strconv.Itoa(a.At.Minute()) + ":int",
		// Otherwise the clock app opens on the person's screen and
		// waits to be tapped, which is no use at all from a room away.
		"android.intent.extra.alarm.SKIP_UI:true:boolean",
	}
	if label := strings.TrimSpace(a.Label); label != "" {
		out = append(out, "android.intent.extra.alarm.MESSAGE:"+
			url.QueryEscape(label)+":String.urlencoded")
	}
	if days := Numbered(a.Days); days != "" {
		out = append(out, "android.intent.extra.alarm.DAYS:"+days+":int[]")
	}
	return strings.Join(out, ",")
}

// Numbered : The days as the phone's alarm clock counts them --
// Sunday 1 through Saturday 7 -- semicolon separated, or empty for
// one time only.
//
// Android's numbering rather than Go's, which starts at Sunday 0,
// and semicolons because commas already separate one extra from the
// next.
func Numbered(days []time.Weekday) string {
	if len(days) == 0 {
		return ""
	}
	var out []string
	for _, d := range days {
		out = append(out, strconv.Itoa(int(d)+1))
	}
	return strings.Join(out, ";")
}
