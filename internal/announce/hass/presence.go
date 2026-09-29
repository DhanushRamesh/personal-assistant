package hass

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/logging"
)

// Presence : Whether somebody is in the room, as Home Assistant sees it.
//
// Home Assistant is asked rather than told. It is the only thing that knows
// -- it watches the signal from a watch and decides -- and asking it means
// there is no second copy of the answer here to go stale.
type Presence struct {
	cfg  PresenceConfig
	http *http.Client
}

// PresenceConfig : What is needed to ask.
type PresenceConfig struct {
	// URL : Where Home Assistant answers.
	URL string
	// Token : A long-lived access token.
	Token logging.Secret
	// Entity : What says whether somebody is in the room, such as
	// input_boolean.in_the_room. Empty disables the whole question.
	Entity string
	// HTTP : The client to use. Optional.
	HTTP *http.Client
}

// NewPresence : Builds one, or reports that there is nothing to ask with.
func NewPresence(cfg PresenceConfig) (*Presence, error) {
	if strings.TrimSpace(cfg.URL) == "" ||
		strings.TrimSpace(cfg.Token.Reveal()) == "" ||
		strings.TrimSpace(cfg.Entity) == "" {
		return nil, ErrNotConfigured
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")

	client := cfg.HTTP
	if client == nil {
		client = &http.Client{}
	}
	return &Presence{cfg: cfg, http: client}, nil
}

// Away : Whether the person is known to be out of the room.
//
// False for anything that is not a plain, current "off": an error, an
// unavailable entity, a state nobody recognises. The two mistakes do not
// cost the same. Speaking to an empty room wastes a sentence; holding a
// reminder back from somebody sitting right there loses it until they
// think to ask.
func (p *Presence) Away(ctx context.Context, _ string) bool {
	if p == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout())
	defer cancel()

	url := p.cfg.URL + "/api/states/" + p.cfg.Entity
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token.Reveal())

	resp, err := p.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return false
	}

	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	// Only a state that says, positively, that they are not here.
	//
	// "off" is the plain boolean the entity used to be; "away" is the
	// three-state sensor that replaced it. "unknown" is the reason that
	// sensor exists and must not count: it means the measurement could
	// not be trusted -- a dead publisher, a blocked radio -- and holding
	// a reminder back on the strength of a fault loses it for somebody
	// sitting right there. "unavailable" is the same kind of silence.
	return parsed.State == "off" || parsed.State == "away"
}

// Timeout : How long the question may take. Short: the firing loop waits
// on it, and not knowing is an answer this can give instantly.
func (c PresenceConfig) Timeout() time.Duration { return 3 * time.Second }

// Describe : What is being asked, for a log line.
func (p *Presence) Describe() string {
	if p == nil {
		return "nothing"
	}
	return fmt.Sprintf("%s at %s", p.cfg.Entity, p.cfg.URL)
}
