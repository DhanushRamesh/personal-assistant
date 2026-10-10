// Package live : GET /v1/live, the open connection a client is told things
// over the moment they happen, and asks over without waiting for a reply.
//
// Server to client: every event the hub publishes (the satellite's state),
// the notices meant for the caller (a conversation of theirs changed,
// whichever client asked in it), and for each prompt the client sent, "chat" when it is accepted, "progress"
// as it works, and "answer" when it ends -- or "refused" when it was never
// accepted. Client to server: "prompt" and "cancel".
package live

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/asking"
	hub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/world/events"
)

// PingInterval : How often an idle connection is pinged, so that a client
// gone without closing is noticed and a proxy does not time the line out.
const PingInterval = 30 * time.Second

// writeTimeout : How long one message may take to send before the client
// is given up on.
const writeTimeout = 5 * time.Second

// readLimit : The largest message a client may send: a prompt, not a file.
const readLimit = 64 << 10

// Asking : What the connection asks through: the same service /api/chat uses.
type Asking interface {
	Ask(ctx context.Context, r asking.Request) (*asking.Started, error)
	Owns(ctx context.Context, userID, chatID string) bool
	Failure(ctx context.Context, chatID string) (code, detail string)
}

// Canceller : Stops a running chat.
type Canceller interface {
	Cancel(id string) bool
}

// inbound : A message from the client.
type inbound struct {
	Type           string `json:"type"`
	Request        string `json:"request,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Text           string `json:"text,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
}

// outbound : A message to the client about one of its chats.
type outbound struct {
	Type           string `json:"type"`
	Request        string `json:"request,omitempty"`
	ChatID         string `json:"chat_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Text           string `json:"text,omitempty"`
	Reason         string `json:"reason,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorDetail    string `json:"error_detail,omitempty"`
	Message        string `json:"message,omitempty"`
}

// Handler : Serves the live connection.
type Handler struct {
	logger *slog.Logger
	hub    *hub.Hub
	asking Asking
	cancel Canceller
}

// New : Builds the handler. A nil hub serves 404, as if the endpoint did not
// exist; a nil asking leaves the connection one-way.
func New(logger *slog.Logger, h *hub.Hub, a Asking, c Canceller) *Handler {
	return &Handler{logger: logger, hub: h, asking: a, cancel: c}
}

// Mount : Registers the endpoint on r, which must already require a token.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/live", h.Live)
}

// Live : Upgrades to a WebSocket, sends every hub event starting with the
// latest of each kind, and answers the client's prompts and cancels, until
// either side closes.
//
// A typed chat is not cancelled when the connection drops: unlike a voice
// turn, nobody has walked away from it, and its answer is in the
// conversation when they come back.
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
	c.SetReadLimit(readLimit)

	caller := authn.Of(r.Context())
	log := h.logger
	if caller != nil && caller.Client != nil {
		log = log.With(slog.String("client_id", caller.Client.ID))
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		defer cancel()
		for {
			var m inbound
			if err := wsjson.Read(ctx, c, &m); err != nil {
				return
			}
			h.handle(ctx, c, caller, m, log)
		}
	}()

	events, unsubscribe := h.hub.Subscribe()
	defer unsubscribe()
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
			if caller == nil || !e.For(caller.User.ID) {
				continue
			}
			if err := send(ctx, c, e); err != nil {
				return
			}
		case <-ping.C:
			pctx, stop := context.WithTimeout(ctx, writeTimeout)
			err := c.Ping(pctx)
			stop()
			if err != nil {
				return
			}
		}
	}
}

// handle : Acts on one message from the client.
func (h *Handler) handle(ctx context.Context, c *websocket.Conn, caller *authn.Caller, m inbound, log *slog.Logger) {
	if h.asking == nil || caller == nil || caller.Client == nil {
		return
	}
	switch m.Type {
	case "prompt":
		started, err := h.asking.Ask(ctx, asking.Request{
			UserID:               caller.User.ID,
			ClientID:             caller.Client.ID,
			Channel:              caller.Client.Channel,
			Model:                caller.Client.Model,
			ActiveConversationID: caller.Client.ActiveConversationID,
			ConversationID:       m.ConversationID,
			Prompt:               m.Text,
		})
		if err != nil {
			_ = send(ctx, c, outbound{Type: "refused", Request: m.Request, Message: refusal(err)})
			if !knownRefusal(err) {
				log.ErrorContext(ctx, "live: cannot ask", slog.Any("error", err))
			}
			return
		}
		_ = send(ctx, c, outbound{Type: "chat", Request: m.Request, ChatID: started.Chat.ID, ConversationID: started.ConversationID})
		log.InfoContext(ctx, "live: chat accepted", slog.String("chat_id", started.Chat.ID))
		go h.follow(ctx, c, started)
	case "cancel":
		if m.ChatID != "" && h.asking.Owns(ctx, caller.User.ID, m.ChatID) {
			h.cancel.Cancel(m.ChatID)
		}
	}
}

// follow : Sends a chat's progress and its answer, until it ends or the
// connection does. Pieces of an answer are for a voice turn only, as on
// /api/chat: a screen is sent the finished answer.
func (h *Handler) follow(ctx context.Context, c *websocket.Conn, started *asking.Started) {
	defer started.Stop()
	id, conversation := started.Chat.ID, started.ConversationID
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-started.Events:
			if !ok {
				_ = send(ctx, c, outbound{Type: "answer", ChatID: id, ConversationID: conversation, Reason: "stop"})
				return
			}
			if ev.Kind == events.KindPartial {
				continue
			}
			if ev.Kind.Terminal() {
				out := outbound{Type: "answer", ChatID: id, ConversationID: conversation, Text: strings.TrimSpace(ev.Text), Reason: reason(ev.Kind)}
				if ev.Kind == events.KindError {
					out.ErrorCode, out.ErrorDetail = h.asking.Failure(ctx, id)
				}
				_ = send(ctx, c, out)
				return
			}
			if text := strings.TrimSpace(ev.Text); text != "" {
				_ = send(ctx, c, outbound{Type: "progress", ChatID: id, Text: text})
			}
		}
	}
}

// reason : Why a chat stopped, as /api/chat names it.
func reason(kind events.Kind) string {
	if kind == events.KindFinal {
		return "stop"
	}
	return string(kind)
}

// refusal : What a client is told when its prompt was not accepted.
func refusal(err error) string {
	switch {
	case errors.Is(err, asking.ErrNoSuchConversation):
		return "No such conversation."
	case errors.Is(err, asking.ErrEmptyPrompt):
		return "A question is required."
	case errors.Is(err, asking.ErrPromptTooLong):
		return "That question is too long."
	case errors.Is(err, asking.ErrNotAccepting):
		return "Not accepting work at the moment."
	default:
		return "That could not be asked."
	}
}

func knownRefusal(err error) bool {
	return errors.Is(err, asking.ErrNoSuchConversation) || errors.Is(err, asking.ErrEmptyPrompt) ||
		errors.Is(err, asking.ErrPromptTooLong) || errors.Is(err, asking.ErrNotAccepting)
}

func send(ctx context.Context, c *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, v)
}
