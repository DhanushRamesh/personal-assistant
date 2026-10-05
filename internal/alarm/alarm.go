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
// Straight to the phone over the tailnet, rather than out through
// Home Assistant's push service and back. The phone is a stable
// address on the tailnet from anywhere, and a reminder's title is
// the person's own business: "call Gunalan: the invoice" has no
// reason to pass through anybody else on its way across the room.
package alarm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Alarm : What the phone is being asked for.
//
// An hour, a minute and some words, which is all an alarm clock
// holds. No repeat: the clock's own repeat cannot be set from
// outside without building a Java object inside Tasker, so a
// repeating reminder instead has its next alarm set each time it
// fires. See remind.Loop.
type Alarm struct {
	// At : When it goes off. Only the hour and the minute make the
	// trip.
	At time.Time
	// Label : What shows on the phone when it rings. Somebody woken
	// at six wants the words, not a number.
	Label string
	// Key : What this alarm is for, usually a reminder's id. Never
	// sent to the phone; it is how a Keeper knows that two attempts
	// are the same alarm rather than two.
	Key string
}

// Stamp : The words with a signature after them, so an alarm going
// off at six says who set it.
//
// Whatever the assistant is currently called, read from
// configuration and never written down here. It was FRIDAY before
// it was Jarvis and may be something else again; an alarm label is
// no place for that to be out of date. Nameless leaves the words
// alone rather than signing them "by".
func Stamp(label, by string) string {
	label, by = strings.TrimSpace(label), strings.TrimSpace(by)
	if by == "" {
		return label
	}
	if label == "" {
		return "by " + by
	}
	return label + " — by " + by
}

// Setter : Somewhere an alarm can be asked for.
type Setter interface {
	// Set : Asks the phone for an alarm. Returns an error where the
	// asking failed, which is not the same as the phone having
	// refused.
	Set(ctx context.Context, a Alarm) error
}

// Separator : What divides the hour, the minute and the words.
//
// A pipe because the phone splits on it and a reminder title will
// not contain one, where a comma or a colon certainly will. Any
// that do turn up are replaced rather than escaped: a title is for
// reading at six in the morning, not for parsing.
const Separator = "|"

// Expected : What the phone answers when it has taken the alarm.
//
// Checked, because anything can answer an HTTP request. A captive
// portal returning its own cheerful 200 would otherwise read as an
// alarm that was set.
const Expected = "jarvis-alarm-ok"

// Phone : The person's phone, reached directly over the tailnet.
type Phone struct {
	// URL : Where the phone listens, including the secret path, such
	// as http://100.76.164.70:1821/a/<secret>. Empty means there is
	// nowhere to ask, and asking fails rather than pretending.
	//
	// The path is the only thing standing between this and anybody
	// else who can reach the port, so it is kept in config.ini with
	// the other secrets rather than written down here.
	URL string
	// By : What the assistant is called, signed after the words.
	// Empty signs nothing. Configuration, never a constant: it was
	// FRIDAY before it was Jarvis.
	By string
	// Timeout : How long the call may take. Zero selects a sensible
	// one.
	Timeout time.Duration
	// Client : Replaceable in tests.
	Client *http.Client
}

// DefaultTimeout : How long to wait on the phone.
//
// Short. This runs inside the turn that sets the reminder, and a
// phone that is asleep on another network should cost a sentence,
// not the whole answer.
const DefaultTimeout = 5 * time.Second

// Set : Asks the phone to set an alarm.
//
// What this can and cannot know is worth being plain about. The
// phone answers once Tasker has run, so a success here really is
// the alarm having been asked for on the device -- unlike a push,
// which is only a message accepted for delivery.
func (p Phone) Set(ctx context.Context, a Alarm) error {
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("alarm: there is no address for the phone")
	}

	wait := p.Timeout
	if wait <= 0 {
		wait = DefaultTimeout
	}
	ask, done := context.WithTimeout(ctx, wait)
	defer done()

	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		strings.TrimSpace(p.URL),
		strings.NewReader(Body(Alarm{At: a.At, Label: Stamp(a.Label, p.By)})))
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: wait}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("alarm: the phone answered %s", res.Status)
	}

	said, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return fmt.Errorf("alarm: %w", err)
	}
	if strings.TrimSpace(string(said)) != Expected {
		return fmt.Errorf("alarm: something other than the phone answered")
	}
	return nil
}

// Body : The alarm as the phone reads it -- hour, minute and words,
// divided by Separator.
func Body(a Alarm) string {
	return strings.Join([]string{
		strconv.Itoa(a.At.Hour()),
		strconv.Itoa(a.At.Minute()),
		Labelled(a.Label),
	}, Separator)
}

// Labelled : The words, with anything that would divide them taken
// out.
//
// Newlines go too. The phone reads the body as one line, and a
// title carrying a second one would arrive truncated with no
// indication that anything was lost.
func Labelled(label string) string {
	clean := strings.Map(func(r rune) rune {
		if r == '|' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, label)
	return strings.Join(strings.Fields(clean), " ")
}
