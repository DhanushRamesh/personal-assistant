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
package alarm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Intent : The broadcast the phone listens for.
//
// Caught on the phone by a profile watching for it, which then sets
// the alarm with the hour, the minute and the words. A broadcast
// rather than anything cleverer because it is the one thing both
// ends already understand: Home Assistant's companion sends it and
// Tasker receives it, and neither needs to know about the other.
const Intent = "jarvis.SET_ALARM"

// Setter : Somewhere an alarm can be asked for.
type Setter interface {
	// Set : Asks the phone for an alarm at a time, labelled. Returns
	// an error where the asking failed, which is not the same as the
	// phone having refused.
	Set(ctx context.Context, at time.Time, label string) error
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
func (t Through) Set(ctx context.Context, at time.Time, label string) error {
	if strings.TrimSpace(t.Service) == "" {
		return fmt.Errorf("alarm: no notify service is configured for the phone")
	}
	name := strings.TrimPrefix(strings.TrimSpace(t.Service), "notify.")

	body, err := json.Marshal(map[string]any{
		"message": "command_broadcast_intent",
		"title":   Intent,
		"data": map[string]any{
			"intent_package_name": "net.dinglisch.android.taskerm",
			"hour":                at.Hour(),
			"minute":              at.Minute(),
			"label":               strings.TrimSpace(label),
			"at":                  at.Format(time.RFC3339),
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
