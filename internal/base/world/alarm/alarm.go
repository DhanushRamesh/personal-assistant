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
	// Days : The days it repeats on, empty for one time only.
	//
	// The phone's own repeat, so it keeps ringing on a day this
	// server is down -- which is the whole reason for putting it on
	// the phone rather than only saying it.
	Days []time.Weekday
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

// Setter : Somewhere an alarm can be asked for, and taken back.
type Setter interface {
	// Set : Asks the phone for an alarm. Returns an error where the
	// asking failed, which is not the same as the phone having
	// refused.
	Set(ctx context.Context, a Alarm) error
	// Clear : Asks the phone to drop the alarm with these words.
	//
	// Only a one-off can be dropped. Android has no way to remove a
	// repeating alarm from outside -- dismissing one skips a single
	// occurrence and asking again makes a second alarm rather than
	// changing the first -- so Repeating reports that rather than
	// pretending. See Holds.
	Clear(ctx context.Context, a Alarm) error
}

// Within : How far ahead an alarm can be asked for and still ring on
// the right day.
//
// An alarm holds an hour and a minute and no date at all, so one
// asked for now goes off at the next time the clock reads that --
// today if it is still to come, otherwise tomorrow. For anything due
// inside a day that is the right moment. For anything further out it
// is the wrong day, and a phone that rings on the wrong day is worse
// than one that does not ring: somebody acts on it.
//
// Found on 8 October 2026 from a booking reminder set for the 10th at
// 2:14pm. The reminder was right, the phone was right, and they
// disagreed -- the alarm was going to ring at 2:14 the following
// afternoon, two days early, and the owner asked three times why it
// could not be moved. Nothing was wrong with it that could be moved.
const Within = 24 * time.Hour

// Reaches : Whether an alarm asked for now would ring at the right
// moment rather than the next time that clock face comes round.
//
// False for anything already past as well. An alarm for a moment
// gone by rings a day late, which is the same fault read backwards.
func Reaches(at, now time.Time) bool {
	ahead := at.Sub(now)
	return ahead > 0 && ahead <= Within
}

// Holds : Whether the phone keeps this alarm on its own, which is
// also exactly when it cannot be taken back.
//
// The two go together and it is worth being plain about why: a
// repeating alarm survives this server being switched off, which
// is the reason for using one, and the same independence means
// nothing here can reach in and remove it.
func Holds(a Alarm) bool { return len(a.Days) > 0 }

// Separator : What divides the hour, the minute and the words.
//
// A pipe because the phone splits on it and a reminder title will
// not contain one, where a comma or a colon certainly will. Any
// that do turn up are replaced rather than escaped: a title is for
// reading at six in the morning, not for parsing.
const Separator = "|"

// Never : What stands for "no days" on the wire.
//
// A value rather than an empty field, because the phone splits the
// body into a fixed number of pieces and a trailing empty one is
// not reliably one of them.
const Never = "0"

// Expected : What the phone answers when it has taken the alarm.
//
// Checked, because anything can answer an HTTP request. A captive
// portal returning its own cheerful 200 would otherwise read as an
// alarm that was set.
const Expected = "jarvis-alarm-ok"

// ExpectedOff : What the phone answers when it has dropped one.
const ExpectedOff = "jarvis-alarm-off"

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
	// OffURL : Where the phone listens for an alarm to be dropped,
	// including its own secret path. Empty means an alarm once set
	// cannot be taken back.
	OffURL string
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

// Settle : How long to leave the phone alone between taking an
// alarm back and putting the new one on.
//
// Measured, not guessed: the two arrive as separate starts of the
// clock's own activity, and back to back the second is dropped --
// the phone still answers that it worked, and no alarm appears.
// A move was losing its alarm entirely before this.
const Settle = 1500 * time.Millisecond

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

	// Signed, and otherwise left alone. Rebuilding the alarm here
	// rather than copying it once dropped the repeat and turned
	// every daily alarm into a one-off.
	signed := a
	signed.Label = Stamp(a.Label, p.By)

	return p.ask(ctx, p.URL, Body(signed), Expected)
}

// ask : One request to the phone, and the one place that decides
// whether it was really the phone that answered.
func (p Phone) ask(ctx context.Context, where, body, want string) error {
	wait := p.Timeout
	if wait <= 0 {
		wait = DefaultTimeout
	}
	asking, done := context.WithTimeout(ctx, wait)
	defer done()

	req, err := http.NewRequestWithContext(asking, http.MethodPost,
		strings.TrimSpace(where), strings.NewReader(body))
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
	if strings.TrimSpace(string(said)) != want {
		return fmt.Errorf("alarm: something other than the phone answered")
	}
	return nil
}

// Body : The alarm as the phone reads it -- hour, minute and words,
// divided by Separator.
func Body(a Alarm) string {
	days := Numbered(a.Days)
	if days == "" {
		days = Never
	}
	return strings.Join([]string{
		strconv.Itoa(a.At.Hour()),
		strconv.Itoa(a.At.Minute()),
		Labelled(a.Label),
		days,
	}, Separator)
}

// OnTheMinute : The time the phone will actually ring at.
//
// An alarm clock holds an hour and a minute and nothing smaller,
// so a reminder at 34 minutes past and 33 seconds becomes an alarm
// at 34 minutes past -- ringing 33 seconds *before* the thing it
// is for. Rounded up instead, so the alarm never goes off ahead of
// the reminder it belongs to. A reminder already on the minute is
// left exactly where it is.
func OnTheMinute(t time.Time) time.Time {
	if t.Second() == 0 && t.Nanosecond() == 0 {
		return t
	}
	return t.Truncate(time.Minute).Add(time.Minute).In(t.Location())
}

// Numbered : The days as the phone's alarm clock counts them --
// Sunday 1 through Saturday 7 -- semicolon separated, or empty for
// one time only.
//
// Android's numbering rather than Go's, which starts at Sunday 0,
// and semicolons because Separator already divides one field from
// the next.
func Numbered(days []time.Weekday) string {
	if len(days) == 0 {
		return ""
	}
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, strconv.Itoa(int(d)+1))
	}
	return strings.Join(out, ";")
}

// Labelled : The words, with anything that would divide them taken
// out.
//
// Newlines go too. The phone reads the body as one line, and a
// title carrying a second one would arrive truncated with no
// indication that anything was lost.
func Labelled(label string) string {
	clean := strings.Map(func(r rune) rune {
		switch r {
		// Divides one field from the next.
		case '|', '\n', '\r':
			return ' '
		// The phone builds the alarm in a line of Java, where the
		// label is a string literal, and reads the whole body for
		// %variables of its own first. A quote, a backslash or a
		// per cent would end the literal or name a variable, and
		// the alarm would simply not be set.
		case '"', '\\', '%':
			return ' '
		}
		return r
	}, label)
	return strings.Join(strings.Fields(clean), " ")
}

// Clear : Asks the phone to drop the alarm with these words.
//
// The words have to match what was sent, signature and all, which
// is why the signing happens here rather than at the call site.
func (p Phone) Clear(ctx context.Context, a Alarm) error {
	if Holds(a) {
		return fmt.Errorf("alarm: a repeating alarm cannot be taken back from here")
	}
	if strings.TrimSpace(p.OffURL) == "" {
		return fmt.Errorf("alarm: there is no address for taking an alarm back")
	}
	return p.ask(ctx, p.OffURL, Labelled(Stamp(a.Label, p.By)), ExpectedOff)
}
