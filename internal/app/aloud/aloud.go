// Package aloud : Announcements said by the person's own desktop app, as
// Home Assistant's satellite says them.
//
// The laptop speaks for itself now -- its own wake word, its own speech to
// text, its own Piper voice (owner, 15 October 2026: *"the wake word, STT
// should be inside the client, and TTS too"*) -- and an announcement is
// spoken there in the same flow it was through Home Assistant (owner, 16
// October 2026: *"look at how announcements are handled there ... i want
// the exact flow"*): the server hands the words over and waits until they
// have been said, and the app waits for its own voice to fall quiet first,
// says them with no chime in front, hears "stop" as it does while answering,
// and plays nothing at the end and listens for nothing after.
//
// The words are pushed rather than collected, as Home Assistant's announce
// call is: a reminder falling due is said when it falls due. A desktop app
// that has said it speaks is joined here for as long as its live connection
// lasts; with none joined, or one that cannot say it, the announcement goes
// to Home Assistant as before.
package aloud

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/base/world/announce"
)

// Wait : The longest an announcement is waited on: the app's wait for its
// own voice to fall quiet, ninety seconds as Home Assistant's
// DefaultQuietWait, and the saying, two minutes as its DefaultSpeakTimeout.
var Wait = 90*time.Second + 2*time.Minute

// NoVoice : What the app answers when it has no voice to say anything in;
// Home Assistant says it instead.
const NoVoice = "no voice"

// Voices : The desktop apps that speak announcements themselves, and Home
// Assistant behind them. An announce.Announcer. Safe for concurrent use.
type Voices struct {
	logger   *slog.Logger
	fallback announce.Announcer
	mu       sync.Mutex
	// joined : Most recently joined last; the last is the one spoken through.
	joined  []*voice
	waiting map[string]*asked
}

type voice struct {
	userID, clientID string
	send             func(ctx context.Context, request, text string) error
}

type asked struct {
	by   *voice
	done chan string
}

// gone : Why an announcement handed to an app was not said: the app left first.
const gone = "the app went away"

// New : No apps joined yet; fallback says what none of them can.
func New(fallback announce.Announcer, logger *slog.Logger) *Voices {
	return &Voices{logger: logger, fallback: fallback, waiting: map[string]*asked{}}
}

// Join : An app that speaks, until leave; send hands it one announcement,
// the request it is to be answered under and the words. done is called with what became
// of each announcement handed to it: empty when it was said.
func (v *Voices) Join(userID, clientID string, send func(ctx context.Context, request, text string) error) (done func(request, failure string), leave func()) {
	me := &voice{userID: userID, clientID: clientID, send: send}
	v.mu.Lock()
	v.joined = append(v.joined, me)
	v.mu.Unlock()
	v.logger.Info("aloud: an app speaks announcements", slog.String("client_id", clientID))
	done = func(request, failure string) {
		v.mu.Lock()
		a := v.waiting[request]
		if a != nil && a.by == me {
			delete(v.waiting, request)
		}
		v.mu.Unlock()
		if a != nil && a.by == me {
			a.done <- failure
		}
	}
	leave = func() {
		v.mu.Lock()
		for i, j := range v.joined {
			if j == me {
				v.joined = append(v.joined[:i], v.joined[i+1:]...)
				break
			}
		}
		var left []*asked
		for id, a := range v.waiting {
			if a.by == me {
				delete(v.waiting, id)
				left = append(left, a)
			}
		}
		v.mu.Unlock()
		for _, a := range left {
			a.done <- gone
		}
	}
	return done, leave
}

// Available : Whether anything can speak.
func (v *Voices) Available() bool {
	return v.latest() != nil || (v.fallback != nil && v.fallback.Available())
}

// Say : Says it through the app joined last, and returns once it has been
// said; through Home Assistant when no app is joined, when the app has no
// voice, or when it left before saying it.
func (v *Voices) Say(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	if err := v.through(ctx, message); !errors.Is(err, errElsewhere) {
		return err
	}
	if v.fallback == nil {
		return errors.New("aloud: nothing is configured to speak")
	}
	return v.fallback.Say(ctx, message)
}

// Reach : Says it as Say does, and sends it wherever else Home Assistant
// reaches the person -- the phone -- as its Reach does.
func (v *Voices) Reach(ctx context.Context, message string) error {
	err := v.through(ctx, strings.TrimSpace(message))
	if errors.Is(err, errElsewhere) {
		if v.fallback == nil {
			return errors.New("aloud: nothing is configured to speak")
		}
		return v.fallback.Reach(ctx, message)
	}
	if err != nil {
		return err
	}
	if n, ok := v.fallback.(notifier); ok {
		n.Notify(ctx, message)
	}
	return nil
}

// notifier : Home Assistant's speaker, which also reaches the phone.
type notifier interface {
	Notify(ctx context.Context, message string)
}

// errElsewhere : No app said it, and Home Assistant is to.
var errElsewhere = errors.New("aloud: said elsewhere")

// through : Hands it to the app joined last and waits for it to be said.
func (v *Voices) through(ctx context.Context, message string) error {
	if message == "" {
		return nil
	}
	to := v.latest()
	if to == nil {
		return errElsewhere
	}
	id := requestID()
	a := &asked{by: to, done: make(chan string, 1)}
	v.mu.Lock()
	v.waiting[id] = a
	v.mu.Unlock()
	forget := func() {
		v.mu.Lock()
		delete(v.waiting, id)
		v.mu.Unlock()
	}
	if err := to.send(ctx, id, message); err != nil {
		forget()
		v.logger.WarnContext(ctx, "aloud: the app could not be handed an announcement", slog.Any("error", err))
		return errElsewhere
	}
	wait := time.NewTimer(Wait)
	defer wait.Stop()
	select {
	case failure := <-a.done:
		switch failure {
		case "":
			v.logger.InfoContext(ctx, "aloud: the app said an announcement", slog.String("client_id", to.clientID))
			return nil
		case NoVoice, gone:
			v.logger.InfoContext(ctx, "aloud: the app did not say it, Home Assistant will", slog.String("why", failure))
			return errElsewhere
		default:
			return fmt.Errorf("aloud: %s", failure)
		}
	case <-wait.C:
		forget()
		return fmt.Errorf("aloud: not said within %s", Wait)
	case <-ctx.Done():
		forget()
		return ctx.Err()
	}
}

func (v *Voices) latest() *voice {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.joined) == 0 {
		return nil
	}
	return v.joined[len(v.joined)-1]
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "say-" + hex.EncodeToString(b)
}
