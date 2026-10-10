// Package live : GET /v1/live, the open connection a client is told things
// over the moment they happen.
package live

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	hub "personal-assistant/internal/app/live"
)

// PingInterval : How often an idle connection is pinged, so that a client
// gone without closing is noticed and a proxy does not time the line out.
const PingInterval = 30 * time.Second

// writeTimeout : How long one message may take to send before the client
// is given up on.
const writeTimeout = 5 * time.Second

// Handler : Serves the live connection.
type Handler struct {
	logger *slog.Logger
	hub    *hub.Hub
}

// New : Builds the handler. A nil hub serves 404, as if the endpoint did not
// exist, so a server built without one says so plainly.
func New(logger *slog.Logger, h *hub.Hub) *Handler {
	return &Handler{logger: logger, hub: h}
}

// Mount : Registers the endpoint on r, which must already require a token.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/live", h.Live)
}

// Live : Upgrades to a WebSocket and sends every event the hub publishes,
// starting with the latest of each kind, until either side closes.
//
// The client sends nothing yet; what it sends is read and discarded, which
// is also how a close from its side is noticed.
func (h *Handler) Live(w http.ResponseWriter, r *http.Request) {
	if h.hub == nil {
		http.NotFound(w, r)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.WarnContext(r.Context(), "live: upgrade refused", slog.Any("error", err))
		return
	}
	defer c.CloseNow()

	ctx := c.CloseRead(r.Context())
	events, unsubscribe := h.hub.Subscribe()
	defer unsubscribe()

	log := h.logger
	if caller := authn.Of(r.Context()); caller != nil && caller.Client != nil {
		log = log.With(slog.String("client_id", caller.Client.ID))
	}
	log.InfoContext(ctx, "live: connected", slog.Int("connected", h.hub.Subscribers()))
	defer log.InfoContext(context.WithoutCancel(ctx), "live: disconnected")

	ping := time.NewTicker(PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				c.Close(websocket.StatusTryAgainLater, "fell behind")
				return
			}
			if err := send(ctx, c, e); err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func send(ctx context.Context, c *websocket.Conn, e hub.Event) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, e)
}
