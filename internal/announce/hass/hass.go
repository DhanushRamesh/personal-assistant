// Package hass speaks through a Home Assistant voice satellite.
//
// Home Assistant calls the server to have a question answered. This is the
// other way round: the server calling Home Assistant to have something said
// out loud, through the same speaker the person is already talking to.
package hass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// DefaultTimeout : How long a call that only asks something may take.
//
// Short, because nothing waits on an announcement and a satellite that is not
// answering should not hold anything up. Speaking is the exception and has
// its own, in DefaultSpeakTimeout.
const DefaultTimeout = 10 * time.Second

// DefaultSpeakTimeout : How long the call that speaks may take.
//
// Home Assistant holds the announce call open until the satellite has
// finished playing -- "Block until announcement is finished", in its own
// words -- so this has to cover synthesising the speech and reading it out,
// not merely handing it over.
//
// Ten seconds does not. It is about a hundred and fifty characters of speech,
// and remind.MaxBody allows five hundred. A longer one played perfectly well
// and then reported failure, which left it pending, and the firing loop said
// it again two seconds later, and again, until the grace hour ran out. Two
// minutes covers the longest body that can be stored, with room for a slow
// synthesiser.
const DefaultSpeakTimeout = 2 * time.Minute

// DefaultQuietWait : How long to wait for the satellite to stop talking
// before speaking over it.
//
// Announcing interrupts: the satellite drops whatever it is playing and says
// the new thing instead. An announcement that follows an answer therefore
// cuts the answer off part-way, which is worse than not announcing at all.
// Long enough for a paragraph read aloud, and after that the announcement is
// abandoned rather than delivered on top of speech.
const DefaultQuietWait = 90 * time.Second

// DefaultSettle : How long the satellite must stay quiet before the
// announcement is spoken.
//
// Falling idle is not the same as having finished: the state flips when the
// satellite stops feeding the speaker, so an announcement sent the instant it
// goes idle lands on the tail of the previous sentence and the two run
// together. A held second separates them, and the person hears two things
// said rather than one long one.
const DefaultSettle = time.Second

// idlePoll : How often the satellite is asked whether it has finished.
const idlePoll = 400 * time.Millisecond

// stateIdle : What the satellite calls doing nothing. Its other states are
// listening, processing and responding.
const stateIdle = "idle"

// maxResponseBytes : The most that will be read from a reply, so a
// misbehaving endpoint cannot exhaust memory.
const maxResponseBytes = 1 << 20

// Config : What is needed to reach Home Assistant.
type Config struct {
	// URL : Where Home Assistant answers, such as http://192.168.0.102:8123.
	// Empty disables announcing altogether.
	URL string
	// Token : A long-lived access token, created by the owner in Home
	// Assistant under their profile's security settings.
	Token logging.Secret
	// Satellite : The entity to speak through, such as
	// assist_satellite.laptop_lva_assist_satellite.
	Satellite string
	// Notify : Notify services that should also hear an announcement,
	// such as notify.mobile_app_pixel_7. Empty is the satellite alone.
	//
	// A satellite is a speaker in one room. Told to say something while
	// the person is out, it says it to nobody. These carry the same
	// words to a phone, which is wherever they are.
	Notify []string
	// Timeout : How long a call that only asks something may take. Zero
	// selects DefaultTimeout.
	Timeout time.Duration
	// SpeakTimeout : How long the call that speaks may take, which is as
	// long as the speaking takes. Zero selects DefaultSpeakTimeout.
	SpeakTimeout time.Duration
	// QuietWait : How long to wait for the satellite to finish speaking
	// before announcing. Zero selects DefaultQuietWait; negative announces
	// at once and interrupts whatever is playing.
	QuietWait time.Duration
	// Settle : How long the satellite must stay quiet before the
	// announcement is spoken. Zero selects DefaultSettle; negative speaks as
	// soon as it first reads as idle.
	Settle time.Duration
	// HTTP : The client to use. Optional.
	HTTP *http.Client
	// Logger : Where it is recorded that something did or did not reach
	// the phone. Nil is silent.
	Logger *slog.Logger
}

// Speaker : Says things aloud through one Home Assistant satellite.
type Speaker struct {
	cfg  Config
	http *http.Client
}

// ErrNotConfigured : Returned by New when there is not enough to reach Home
// Assistant. Not a fault: a server with nothing to announce through is a
// working server.
var ErrNotConfigured = errors.New("hass: no url, token or satellite")

// New : Builds a Speaker, or reports that there is nothing to build it from.
func New(cfg Config) (*Speaker, error) {
	if strings.TrimSpace(cfg.URL) == "" ||
		strings.TrimSpace(cfg.Token.Reveal()) == "" ||
		strings.TrimSpace(cfg.Satellite) == "" {
		return nil, ErrNotConfigured
	}

	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.SpeakTimeout <= 0 {
		cfg.SpeakTimeout = DefaultSpeakTimeout
	}
	if cfg.QuietWait == 0 {
		cfg.QuietWait = DefaultQuietWait
	}
	if cfg.Settle == 0 {
		cfg.Settle = DefaultSettle
	}

	// No timeout on the client: one number cannot be both short enough for
	// a state poll and long enough to read a paragraph aloud. Every request
	// below carries its own deadline instead.
	client := cfg.HTTP
	if client == nil {
		client = &http.Client{}
	}

	return &Speaker{cfg: cfg, http: client}, nil
}

// Available : Whether there is a satellite to speak through.
func (s *Speaker) Available() bool { return s != nil }

// announceRequest : The body of assist_satellite.announce.
type announceRequest struct {
	EntityID string `json:"entity_id"`
	Message  string `json:"message"`
	// Preannounce : Whether to play a chime first. Always false here: what
	// this says is a footnote to something already spoken, not a summons.
	Preannounce bool `json:"preannounce"`
}

// notifyRequest : What the notify service is sent.
//
// Message is the literal word TTS; the words to say go in Data. That is
// the Android companion app's own arrangement, not a choice made here.
type notifyRequest struct {
	Message string     `json:"message"`
	Data    notifyData `json:"data"`
}

// notifyData : How the companion app is told to speak.
type notifyData struct {
	// TTSText : The words to say aloud.
	TTSText string `json:"tts_text"`
	// MediaStream : Which volume control it obeys. The music stream
	// rather than the notification one, because the notification stream
	// is silenced by every do-not-disturb and most quiet-hours rules,
	// and an announcement nobody hears is the fault this exists to fix.
	MediaStream string `json:"media_stream"`
}

// Say : Speaks the message through the configured satellite, once it has
// finished saying anything else.
//
// Announcing interrupts, so this waits for the satellite to fall idle first.
// Without that an announcement following an answer cuts the answer off
// mid-sentence, which is how this was found: a recitation stopped dead so the
// assistant could say what it had named the conversation.
func (s *Speaker) Say(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	if err := s.waitUntilQuiet(ctx); err != nil {
		return err
	}

	body, err := json.Marshal(announceRequest{
		EntityID:    s.cfg.Satellite,
		Message:     message,
		Preannounce: false,
	})
	if err != nil {
		return fmt.Errorf("hass: building announce request: %w", err)
	}

	// Started only now, so the wait for the satellite to fall quiet -- up
	// to QuietWait, which is longer than this -- is not counted against
	// the speaking.
	ctx, cancel := context.WithTimeout(ctx, s.cfg.SpeakTimeout)
	defer cancel()

	url := s.cfg.URL + "/api/services/assist_satellite/announce"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hass: building announce request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("hass: announcing: %w", err)
	}
	defer resp.Body.Close()

	// Read and discard, so the connection can be reused.
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("hass: announcing: %s: %s",
			resp.Status, strings.TrimSpace(string(answer)))
	}

	return nil
}

// Reach : Says it in the room, and sends it wherever else the person can
// be reached.
//
// Separate from Say because most announcements should not follow
// anybody about. A greeting fires because they have just walked up to
// the laptop, so a copy on their phone arrives at the one moment it is
// certainly not wanted; naming a conversation is housekeeping. Only a
// reminder is worth the trip, and the trip is a real one -- these leave
// the network and reach the phone through Google's push service, which
// nothing else here does.
func (s *Speaker) Reach(ctx context.Context, message string) error {
	if err := s.Say(ctx, message); err != nil {
		return err
	}
	s.alsoNotify(ctx, message)
	return nil
}

// alsoNotify : Sends the same words to every notify service configured.
//
// After the satellite, and never instead of it: the speaker in the room
// is the one the person expects, and a phone that also buzzes is an
// addition to it.
//
// Failures are logged nowhere and returned to nobody. The announcement
// has already been made by the time this runs, and a phone that could
// not be reached is not a reason to report that the announcement
// failed -- doing so would have a caller retry, and the retry would say
// everything twice in the room where it was already heard.
func (s *Speaker) alsoNotify(ctx context.Context, message string) {
	for _, service := range s.cfg.Notify {
		err := s.notifyOne(ctx, service, message)
		if s.cfg.Logger == nil {
			continue
		}
		// Logged either way. Whether something reached the phone was
		// not answerable from the log before: failures were dropped and
		// successes said nothing, so the only way to tell was to read
		// the code.
		if err != nil {
			s.cfg.Logger.WarnContext(ctx, "could not reach the phone",
				slog.String("service", service), slog.Any("error", err))
			continue
		}
		s.cfg.Logger.InfoContext(ctx, "sent to the phone as well",
			slog.String("service", service))
	}
}

// notifyOne : Speaks an announcement through one notify service.
//
// "TTS" as the message with the words in tts_text is how the Android
// companion app is asked to say something aloud rather than show it.
// A phone that only shows it is a phone somebody has to look at, which
// is not what an announcement is for.
func (s *Speaker) notifyOne(ctx context.Context, service, message string) error {
	name := strings.TrimPrefix(strings.TrimSpace(service), "notify.")
	if name == "" {
		return nil
	}

	body, err := json.Marshal(notifyRequest{
		Message: "TTS",
		Data:    notifyData{TTSText: message, MediaStream: "music_stream"},
	})
	if err != nil {
		return fmt.Errorf("hass: building notify request: %w", err)
	}

	url := s.cfg.URL + "/api/services/notify/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hass: building notify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("hass: notifying %s: %w", name, err)
	}
	defer resp.Body.Close()

	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("hass: notifying %s: %s: %s",
			name, resp.Status, strings.TrimSpace(string(answer)))
	}
	return nil
}

// waitUntilQuiet : Blocks until the satellite has been doing nothing for
// Settle.
//
// The quiet has to hold, not merely occur. Idle is read again after the pause
// because the satellite reaches it before the speaker has finished and
// because a new turn can begin during the pause; either way the wait starts
// over rather than announcing into speech.
//
// A satellite that never falls quiet is reported rather than spoken over: the
// caller can then log it and drop the announcement, which is the right
// outcome for anything incidental.
func (s *Speaker) waitUntilQuiet(ctx context.Context) error {
	if s.cfg.QuietWait < 0 {
		return nil
	}

	deadline := time.Now().Add(s.cfg.QuietWait)
	for {
		state, err := s.state(ctx)
		if err != nil {
			// Not knowing is not a reason to interrupt. Treated as still
			// speaking, so the announcement is dropped rather than cutting
			// across an answer.
			return err
		}

		if state == stateIdle {
			if s.cfg.Settle <= 0 {
				return nil
			}
			if err := s.pause(ctx, s.cfg.Settle); err != nil {
				return err
			}
			// Still idle after the pause means the previous speech really
			// has ended, and a gap has been left after it.
			if state, err = s.state(ctx); err != nil {
				return err
			}
			if state == stateIdle {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("hass: %s was still %s after %s", s.cfg.Satellite, state, s.cfg.QuietWait)
		}

		if err := s.pause(ctx, idlePoll); err != nil {
			return err
		}
	}
}

// pause : Waits, unless the caller gives up first.
func (s *Speaker) pause(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// state : What the satellite is doing, as Home Assistant reports it.
func (s *Speaker) state(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	url := s.cfg.URL + "/api/states/" + s.cfg.Satellite
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("hass: building state request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token.Reveal())

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("hass: reading satellite state: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("hass: reading satellite state: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("hass: satellite state was not usable: %w", err)
	}
	return parsed.State, nil
}
