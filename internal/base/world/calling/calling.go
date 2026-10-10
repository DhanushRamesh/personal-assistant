// Package calling rings somebody from the person's own phone.
//
// Owner, 10 October 2026: *"we can set an alarm right -- likewise
// there should be an option to call a person. For that it should have
// the contacts list on my phone."*
//
// It does not have the contacts list, and that is the design rather
// than a shortcut. What goes to the phone is a **name**; the phone
// looks that name up in its own contacts and dials. A contact added
// this morning works without an import, nobody else's telephone
// numbers live on this server, and the one real safeguard holds: the
// only people reachable are the people already in the phone.
//
// The server is not blind for it. Every name it knows arrived because
// the phone reported a call and named who it was with, so it knows
// the ten people actually rung -- which is the list worth matching a
// mis-heard name against. An imported address book of five hundred
// would bury them.
package calling

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Call : One call to place.
type Call struct {
	// Who : The name, as the phone's own contacts have it.
	//
	// Never a number. A server that can be made to dial arbitrary
	// digits by one mis-heard sentence is a different and worse
	// thing than one that can only ring a contact.
	Who string
}

// Expected : What the phone answers when it has placed the call.
//
// Checked, because anything can answer an HTTP request and a captive
// portal's cheerful 200 would otherwise read as a call being made.
//
// Not the assistant's name, unlike the alarm's token: the name is
// configuration and was FRIDAY before it was Jarvis, and a word on
// the wire that has to be changed on the phone when the assistant is
// renamed is a trap. Said plainly here because the alarm's tokens
// predate the rule and are not the example to copy.
const Expected = "phone-call-ok"

// DefaultTimeout : How long to wait on the phone.
//
// Longer than the alarm's five seconds. Tasker answers an alarm as
// soon as the clock has it, where placing a call starts the dialler,
// and this is not inside anybody's turn: the errand sender is asking,
// behind the answer.
const DefaultTimeout = 10 * time.Second

// Phone : The person's phone, reached directly over the tailnet.
type Phone struct {
	// URL : Where the phone listens for a call, including the secret
	// path. Empty means there is nowhere to ask, and asking fails
	// rather than pretending.
	//
	// The path is the only thing between this and anybody else who
	// can reach the port -- more so here than for an alarm, since
	// what is on the other end of it dials -- so it is kept with the
	// other secrets.
	URL string
	// Timeout : How long the call may take. Zero selects
	// [DefaultTimeout].
	Timeout time.Duration
	// Client : Replaceable in tests.
	Client *http.Client
}

// Ring : Asks the phone to place one call.
func (p Phone) Ring(ctx context.Context, c Call) error {
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("calling: there is no address for the phone")
	}
	body, err := Body(c)
	if err != nil {
		return err
	}
	return p.post(ctx, p.URL, body, Expected)
}

// post : One request to the phone, and the one place that decides
// whether it was really the phone that answered.
func (p Phone) post(ctx context.Context, where, body, want string) error {
	wait := p.Timeout
	if wait <= 0 {
		wait = DefaultTimeout
	}
	asking, done := context.WithTimeout(ctx, wait)
	defer done()

	req, err := http.NewRequestWithContext(asking, http.MethodPost,
		strings.TrimSpace(where), strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("calling: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: wait}
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("calling: the phone answered %s", res.Status)
	}
	said, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return fmt.Errorf("calling: %w", err)
	}
	if strings.TrimSpace(string(said)) != want {
		return fmt.Errorf("calling: something other than the phone answered")
	}
	return nil
}

// Body : The call as the phone reads it -- the name, and nothing
// else.
//
// Checked here rather than at the tool, because this is the last
// place before the wire and it is the place a second caller would
// miss.
func Body(c Call) (string, error) {
	who := Tidy(c.Who)
	if who == "" {
		return "", fmt.Errorf("calling: there is nobody to ring")
	}
	if Digits(who) {
		return "", fmt.Errorf("calling: %q is a number, and only a name may be rung", who)
	}
	return who, nil
}

// Tidy : A name as it goes on the wire.
//
// Newlines out, because the phone reads one line and a name carrying
// a second one would arrive as a name and a stray instruction.
func Tidy(who string) string {
	who = strings.ReplaceAll(who, "\r", " ")
	who = strings.ReplaceAll(who, "\n", " ")
	return strings.Join(strings.Fields(who), " ")
}

// Digits : Whether what was given is a telephone number rather than
// somebody's name.
//
// Refused outright rather than passed on. The phone would dial it
// perfectly well, which is exactly the problem.
func Digits(who string) bool {
	digits := 0
	for _, r := range who {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == ' ' || r == '+' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return false
		}
	}
	return digits >= 3
}

// Worth : How long a call is still worth placing.
//
// Two minutes. Somebody who asked to ring their mother and whose
// phone was unreachable for ten wants to be told it did not happen,
// not to have it dial while they are in a meeting.
const Worth = 2 * time.Minute

// What : Something about a call in progress that can be changed.
type What string

const (
	// Speaker : The phone's loudspeaker.
	Speaker What = "speaker"
	// Mic : The person's own microphone, which muting turns off.
	Mic What = "mic"
)

// During : One change to a call already in progress.
type During struct {
	// What : Which thing. Required.
	What What
	// On : Whether it should be on. Muting is Mic off, which is
	// the one place the wording and the flag disagree -- said in
	// the tool, not here.
	On bool
}

// DuringBody : The change as the phone reads it -- two words, so a
// profile can be read at a glance a year from now.
func DuringBody(d During) (string, error) {
	switch d.What {
	case Speaker, Mic:
	default:
		return "", fmt.Errorf("calling: %q is not something that can be changed mid-call", d.What)
	}
	if d.On {
		return string(d.What) + " on", nil
	}
	return string(d.What) + " off", nil
}

// ExpectedDuring : What the phone answers when it has made the change.
const ExpectedDuring = "phone-during-ok"

// Change : Asks the phone to change something about the call it is on.
func (p Phone) Change(ctx context.Context, where string, d During) error {
	if strings.TrimSpace(where) == "" {
		return fmt.Errorf("calling: there is no address for the phone")
	}
	body, err := DuringBody(d)
	if err != nil {
		return err
	}
	return p.post(ctx, where, body, ExpectedDuring)
}

// Connecting : How long to leave the phone before changing anything
// about a call it was asked to place in the same breath.
//
// A guess, and said so. The dialler has to be up before the speaker
// means anything, and nothing here is told when that happens -- the
// phone answers the request, not the call. Four seconds is long
// enough for the dialler and short enough that somebody who said
// "and put it on speaker" is not still waiting.
const Connecting = 4 * time.Second

// Soon : How long a mid-call change is worth making.
//
// Twenty seconds. Unlike a call, which is still worth placing a
// minute late, putting a call on speaker after the call has ended is
// nothing but confusing.
const Soon = 20 * time.Second
