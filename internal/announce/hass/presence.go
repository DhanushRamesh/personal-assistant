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
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
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

	// Evidence : What Entity is worked out from, such as
	// sensor.watch_signal_best. A flag only changes when something
	// decides to change it, so its own age says nothing: it reads the
	// same whether it is right or whether whatever maintains it died an
	// hour ago. The thing underneath moves constantly while it is
	// working, so its age is the only honest measure of whether the
	// flag still means anything.
	//
	// Empty means every answer is taken as current, which is what this
	// did before there was anything to check.
	Evidence string

	// Fresh : How old Evidence may be and still count. Zero selects
	// DefaultFresh.
	Fresh time.Duration
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

// DefaultFresh : How old the evidence may be and still count.
//
// The thing behind the flag is republished whenever it moves, which
// while it is working is constantly. Five minutes is far longer than any
// ordinary gap and far shorter than the twelve minutes it took to lose a
// reminder on 27 September 2026.
const DefaultFresh = 5 * time.Minute

// Look : What is known about whether the person is in the room.
//
// Away is false for anything that is not a plain, current "off": an
// error, an unavailable entity, a state nobody recognises. The two
// mistakes do not cost the same. Speaking to an empty room wastes a
// sentence; holding a reminder back from somebody sitting right there
// loses it until they think to ask.
//
// Sure says whether that answer rests on anything current. A flag holds
// its last value for ever when whatever maintains it stops, and reads
// exactly like a flag that is right -- so the flag is not asked about
// itself. What it is derived from is asked instead, and how long ago it
// last moved is the answer.
func (p *Presence) Look(ctx context.Context, _ string) remind.Where {
	if p == nil {
		return remind.Where{}
	}

	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout())
	defer cancel()

	flag, err := p.state(ctx, p.cfg.Entity)
	if err != nil {
		return remind.Where{}
	}

	// "off" and nothing else. "unavailable" and "unknown" both mean the
	// question could not be answered, which is not the same as no.
	return remind.Where{
		Away: flag.State == "off",
		Sure: p.current(ctx),
	}
}

// current : Whether the evidence behind the flag is recent enough for
// the flag to mean anything.
func (p *Presence) current(ctx context.Context) bool {
	if strings.TrimSpace(p.cfg.Evidence) == "" {
		return true
	}

	seen, err := p.state(ctx, p.cfg.Evidence)
	if err != nil {
		return false
	}
	// A sensor that says it does not know is not evidence, however
	// recently it said so.
	if seen.State == "unavailable" || seen.State == "unknown" || seen.State == "" {
		return false
	}
	if seen.LastUpdated.IsZero() {
		return false
	}
	return time.Since(seen.LastUpdated) <= p.cfg.fresh()
}

// entityState : The part of a Home Assistant state that matters here.
type entityState struct {
	State       string    `json:"state"`
	LastUpdated time.Time `json:"last_updated"`
}

// state : Asks Home Assistant what an entity is.
func (p *Presence) state(ctx context.Context, entity string) (entityState, error) {
	var out entityState

	url := p.cfg.URL + "/api/states/" + entity
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token.Reveal())

	resp, err := p.http.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("hass: asking about %s: %s", entity, resp.Status)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// Timeout : How long the question may take. Short: the firing loop waits
// on it, and not knowing is an answer this can give instantly.
func (c PresenceConfig) Timeout() time.Duration { return 3 * time.Second }

// fresh : How old the evidence may be.
func (c PresenceConfig) fresh() time.Duration {
	if c.Fresh <= 0 {
		return DefaultFresh
	}
	return c.Fresh
}

// Describe : What is being asked, for a log line.
func (p *Presence) Describe() string {
	if p == nil {
		return "nothing"
	}
	if strings.TrimSpace(p.cfg.Evidence) == "" {
		return fmt.Sprintf("%s at %s, with nothing to check it against",
			p.cfg.Entity, p.cfg.URL)
	}
	return fmt.Sprintf("%s at %s, trusted while %s is under %s old",
		p.cfg.Entity, p.cfg.URL, p.cfg.Evidence, p.cfg.fresh())
}
