// Package glance : POST /v1/glance, "what am I seeing", answered in one
// quick call from what the client says is on its screen.
package glance

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/app/glance"
	"personal-assistant/internal/base/records/client"
)

// Looker : What answers a glance.
type Looker interface {
	Look(ctx context.Context, userID string, c *client.Client, question, where string, spoken bool) (string, error)
}

// Request : What the client asks, with what is on its screen.
type Request struct {
	Question string `json:"question"`
	Where    string `json:"where"`
	Spoken   bool   `json:"spoken,omitempty"`
}

// Response : What to say.
type Response struct {
	Said string `json:"said"`
}

// Handler : Serves /v1/glance.
type Handler struct {
	httpx.Responder
	looker Looker
}

// New : Builds the handler. A nil looker answers that it cannot.
func New(logger *slog.Logger, looker Looker) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, looker: looker}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/glance", h.Glance)
}

// Glance : Says what the client's screen shows.
func (h *Handler) Glance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.looker == nil {
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "I cannot look at the screen from here.")
		return
	}
	var req Request
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "What was asked?")
		return
	}
	caller := authn.Of(ctx)
	said, err := h.looker.Look(ctx, caller.User.ID, caller.Client, req.Question, req.Where, req.Spoken)
	if errors.Is(err, glance.ErrNothingShown) {
		httpx.WriteError(ctx, w, http.StatusUnprocessableEntity, "The app said nothing of what is on its screen.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "glancing", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, Response{Said: said})
}
