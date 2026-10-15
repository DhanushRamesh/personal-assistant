package hass

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"personal-assistant/internal/base/platform/logging"
)

// Unknown : The state reported for every watched entity while Home
// Assistant cannot be heard, so a listener never goes on believing the last
// state it was told.
const Unknown = "unknown"

// subscribeID : The id of the one subscription a connection makes.
const subscribeID = 1

// WatcherConfig : What is needed to follow entities as they change.
type WatcherConfig struct {
	// URL : Where Home Assistant answers, such as http://localhost:8123.
	URL string
	// Token : A long-lived access token.
	Token logging.Secret
	// Entities : What to follow, such as
	// assist_satellite.laptop_lva_assist_satellite.
	Entities []string
	// OnState : Called with each new state, and with Unknown for every
	// entity when the connection is lost. Called from one goroutine.
	OnState func(entity, state string)
	// Logger : Receives connection trouble.
	Logger *slog.Logger
}

// Watcher : Follows entities' states over Home Assistant's WebSocket API,
// pushed as they change rather than polled.
//
// One subscribe_entities per connection: Home Assistant sends each entity's
// state at once and then only what changes, in its compressed form ("a" for
// added, "c" with "+" for changed). A dropped connection is retried with a
// backoff from one second to thirty, reset once a connection has held for a
// minute.
type Watcher struct {
	cfg  WatcherConfig
	ws   string
	wait func(context.Context, time.Duration) bool
}

// NewWatcher : Builds a Watcher, or reports ErrNotConfigured when there is
// nothing to watch or nothing to watch it with.
//
// An entity named by a setting that is now blank is not an entity. A
// caller passing one entity reads as something to watch, so the
// connection was made and retried for ever against a Home Assistant
// that had been turned off on purpose, once a second and then once
// every thirty, saying so each time.
func NewWatcher(cfg WatcherConfig) (*Watcher, error) {
	cfg.Entities = named(cfg.Entities)
	if cfg.URL == "" || cfg.Token.Reveal() == "" || len(cfg.Entities) == 0 || cfg.OnState == nil {
		return nil, ErrNotConfigured
	}
	ws, err := websocketURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Watcher{cfg: cfg, ws: ws, wait: sleep}, nil
}

// named : The entities that have a name, in order.
func named(entities []string) []string {
	kept := make([]string, 0, len(entities))
	for _, e := range entities {
		if e = strings.TrimSpace(e); e != "" {
			kept = append(kept, e)
		}
	}
	return kept
}

// websocketURL : Home Assistant's WebSocket address for its base URL.
func websocketURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("hass: reading %q: %w", base, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("hass: %q is not an http address", base)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/api/websocket"
	return u.String(), nil
}

// Run : Follows the entities until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	backoff := time.Second
	for {
		started := time.Now()
		err := w.once(ctx)
		if ctx.Err() != nil {
			return
		}
		for _, entity := range w.cfg.Entities {
			w.cfg.OnState(entity, Unknown)
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		w.cfg.Logger.WarnContext(ctx, "lost Home Assistant's live states, retrying",
			slog.Any("error", err), slog.Duration("in", backoff))
		if !w.wait(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// message : The parts of a Home Assistant WebSocket message this reads.
type message struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Success *bool  `json:"success"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Event *struct {
		Added map[string]struct {
			S *string `json:"s"`
		} `json:"a"`
		Changed map[string]struct {
			Plus struct {
				S *string `json:"s"`
			} `json:"+"`
		} `json:"c"`
	} `json:"event"`
}

// once : One connection, from dialling to its end.
func (w *Watcher) once(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(dialCtx, w.ws, nil)
	if err != nil {
		return fmt.Errorf("dialling: %w", err)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)

	var m message
	if err := wsjson.Read(dialCtx, c, &m); err != nil || m.Type != "auth_required" {
		return fmt.Errorf("expected auth_required, got %q: %v", m.Type, err)
	}
	auth := map[string]string{"type": "auth", "access_token": w.cfg.Token.Reveal()}
	if err := wsjson.Write(dialCtx, c, auth); err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	if err := wsjson.Read(dialCtx, c, &m); err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	if m.Type != "auth_ok" {
		return fmt.Errorf("the token was refused: %s", m.Type)
	}
	sub := map[string]any{"id": subscribeID, "type": "subscribe_entities", "entity_ids": w.cfg.Entities}
	if err := wsjson.Write(dialCtx, c, sub); err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}

	for {
		m = message{}
		if err := wsjson.Read(ctx, c, &m); err != nil {
			return fmt.Errorf("reading: %w", err)
		}
		if m.ID != subscribeID {
			continue
		}
		switch m.Type {
		case "result":
			if m.Success != nil && !*m.Success {
				why := "refused"
				if m.Error != nil {
					why = m.Error.Message
				}
				return errors.New("subscribing: " + why)
			}
		case "event":
			if m.Event == nil {
				continue
			}
			for entity, s := range m.Event.Added {
				if s.S != nil {
					w.cfg.OnState(entity, *s.S)
				}
			}
			for entity, d := range m.Event.Changed {
				if d.Plus.S != nil {
					w.cfg.OnState(entity, *d.Plus.S)
				}
			}
		}
	}
}

// sleep : Waits d, reporting false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
