package hass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// DefaultAsideTimeout : How long the call that speaks an aside may take.
//
// The call returns once the audio has been handed to the player rather than
// once it has been heard, so this covers a request, not a sentence.
const DefaultAsideTimeout = 15 * time.Second

// DefaultSettleWait : The longest the answer is held back for an aside that
// is still being spoken.
//
// A ceiling, not a delay: a player that has finished is noticed within
// asidePoll. It exists because a player stuck reporting itself as playing
// must not hold an answer for ever, and an answer late is worse than an
// aside clipped.
const DefaultSettleWait = 12 * time.Second

// asidePoll : How often the player is asked whether it has finished.
const asidePoll = 150 * time.Millisecond

// statePlaying : What the media player calls having something to play.
const statePlaying = "playing"

// AsideConfig : What is needed to speak while a turn is still running.
type AsideConfig struct {
	// URL : Where Home Assistant answers.
	URL string
	// Token : A long-lived access token.
	Token logging.Secret
	// MediaPlayer : The satellite's media player entity, such as
	// media_player.laptop_lva_media_player.
	MediaPlayer string
	// Engine : The text-to-speech entity, such as tts.piper.
	Engine string
	// Voice : Which voice to use, such as jarvis-medium. Empty leaves the
	// engine's default, which is a different voice from the one answers are
	// spoken in.
	Voice string
	// Language : The language the voice speaks, such as en_GB.
	Language string
	// Timeout : How long the call may take. Zero selects
	// DefaultAsideTimeout.
	Timeout time.Duration
	// SettleWait : The longest to hold an answer back while an aside is
	// still being spoken. Zero selects DefaultSettleWait; negative never
	// waits and cuts the aside off instead.
	SettleWait time.Duration
	// HTTP : The client to use. Optional.
	HTTP *http.Client
}

// Aside : Says something through the satellite's media player while a turn
// is still being answered.
//
// Speaking to the satellite itself is the wrong door for this. Both an
// announcement and an answer run through the satellite's tts_player with the
// same completion callback, and that callback marks the turn finished: an
// announcement made mid-turn ends the turn it was describing. The media
// player is a separate player that the voice pipeline never reads, so
// playing on it leaves the turn untouched. Measured: the satellite stayed
// idle throughout, with and without other audio playing.
//
// What it cannot do is quieten anything else. A browser playing video is a
// separate audio client and is mixed rather than ducked, which is how
// answers already behave.
type Aside struct {
	cfg  AsideConfig
	http *http.Client
}

// ErrNoAside : Returned by NewAside when there is nothing to speak through.
// Not a fault: saying nothing during a turn is what this did before.
var ErrNoAside = errors.New("hass: no url, token or media player")

// NewAside : Builds an Aside, or reports that there is nothing to build it
// from.
func NewAside(cfg AsideConfig) (*Aside, error) {
	if strings.TrimSpace(cfg.URL) == "" ||
		strings.TrimSpace(cfg.Token.Reveal()) == "" ||
		strings.TrimSpace(cfg.MediaPlayer) == "" ||
		strings.TrimSpace(cfg.Engine) == "" {
		return nil, ErrNoAside
	}

	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultAsideTimeout
	}
	if cfg.SettleWait == 0 {
		cfg.SettleWait = DefaultSettleWait
	}

	client := cfg.HTTP
	if client == nil {
		client = &http.Client{}
	}
	return &Aside{cfg: cfg, http: client}, nil
}

// Available : Whether there is anywhere to speak an aside.
func (a *Aside) Available() bool { return a != nil }

// Say : Speaks the message through the media player, as music.
//
// Two calls, and the second one matters. Asking tts.speak to say it
// through a media player sends the audio as an announcement, and an
// announcement on this satellite plays through the same player the answer
// uses -- so the aside stopped the answer's audio and the turn never
// closed, leaving the satellite stuck in responding and deaf to the wake
// word. The sound is fetched as a URL instead and played as ordinary
// media, which is the one player the voice pipeline does not touch.
func (a *Aside) Say(ctx context.Context, message string) error {
	if a == nil {
		return nil
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	url, err := a.voiced(ctx, message)
	if err != nil {
		return err
	}

	// Turned up before every aside, because what it is played through is
	// the satellite's music player, and that player is ducked to half
	// volume whenever the satellite speaks. Nothing here can tell whether
	// a duck is still in force from a previous turn, so rather than read
	// the volume it is simply set. The failure is ignored: an aside heard
	// quietly is better than one not heard at all.
	if err := a.post(ctx, "media_player/volume_set", map[string]any{
		"entity_id":    a.cfg.MediaPlayer,
		"volume_level": 1.0,
	}); err != nil {
		// Not fatal, and not worth a line in the log every turn.
		_ = err
	}

	return a.post(ctx, "media_player/play_media", map[string]any{
		"entity_id":          a.cfg.MediaPlayer,
		"media_content_id":   url,
		"media_content_type": "music",
		// Never true. True is the announcement path, which is the
		// satellite's own text-to-speech player.
		"announce": false,
	})
}

// voiced : The sound of a sentence, as a URL the media player can fetch.
func (a *Aside) voiced(ctx context.Context, message string) (string, error) {
	body := map[string]any{
		"engine_id": a.cfg.Engine,
		"message":   message,
		"cache":     false,
	}
	if a.cfg.Language != "" {
		body["language"] = a.cfg.Language
	}
	if a.cfg.Voice != "" {
		body["options"] = map[string]any{"voice": a.cfg.Voice}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("hass: building tts request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.URL+"/api/tts_get_url",
		bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("hass: building tts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("hass: asking for speech: %w", err)
	}
	defer resp.Body.Close()

	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("hass: asking for speech: %s: %s",
			resp.Status, strings.TrimSpace(string(answer)))
	}

	var said struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(answer, &said); err != nil || said.URL == "" {
		return "", fmt.Errorf("hass: the speech had no url")
	}
	return said.URL, nil
}

// Settled : Waits for what was said to finish, so the answer follows it
// rather than talking over it.
//
// The two are spoken through different players -- the aside through the
// media player, the answer through the satellite -- so nothing sequences
// them by itself. Beginning the answer while an aside is still playing
// lowers the aside to half volume and leaves it murmuring underneath,
// which is worse than either alone.
//
// Waiting cannot fail a turn. A player that will not answer, or will not
// stop, has the aside cut off and the answer goes out.
func (a *Aside) Settled(ctx context.Context) error {
	if a == nil || a.cfg.SettleWait < 0 {
		return a.Stop(ctx)
	}

	deadline := time.Now().Add(a.cfg.SettleWait)
	for {
		state, err := a.state(ctx)
		if err != nil || state != statePlaying {
			return err
		}
		if time.Now().After(deadline) {
			return a.Stop(ctx)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(asidePoll):
		}
	}
}

// state : What the media player is doing.
func (a *Aside) state(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.cfg.URL+"/api/states/"+a.cfg.MediaPlayer, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token.Reveal())

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("hass: reading %s: %s", a.cfg.MediaPlayer, resp.Status)
	}

	var said struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &said); err != nil {
		return "", err
	}
	return said.State, nil
}

// Stop : Silences whatever the media player is saying.
//
// The way out when an aside will not finish. Nothing else plays on this
// player, so this cannot cut anything that was not put there here.
func (a *Aside) Stop(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return a.post(ctx, "media_player/media_stop",
		map[string]any{"entity_id": a.cfg.MediaPlayer})
}

// Describe : What an aside is spoken through, for a log line.
func (a *Aside) Describe() string {
	if a == nil {
		return "nothing"
	}
	return fmt.Sprintf("%s through %s", a.cfg.MediaPlayer, a.cfg.Engine)
}

// post : Calls one Home Assistant service.
func (a *Aside) post(ctx context.Context, service string, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("hass: building %s request: %w", service, err)
	}

	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.URL+"/api/services/"+service, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("hass: building %s request: %w", service, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token.Reveal())
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("hass: calling %s: %w", service, err)
	}
	defer resp.Body.Close()

	answer, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("hass: calling %s: %s: %s",
			service, resp.Status, strings.TrimSpace(string(answer)))
	}
	return nil
}
