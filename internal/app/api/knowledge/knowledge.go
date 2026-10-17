// Package knowledge : POST /v1/knowledge, where a client puts what it knows
// of the person -- a space's notes, Claude Code's conversations, a file --
// to be kept and found by meaning (see internal/domain/knowledge).
package knowledge

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/domain/knowledge"
)

// Keeper : What keeps a source's text.
type Keeper interface {
	Keep(ctx context.Context, userID string, source knowledge.Source, ref, text string) (bool, error)
}

// Request : One source's whole text; empty forgets it.
type Request struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Text   string `json:"text"`
}

// Response : Whether it was embedded -- false when it had not changed.
type Response struct {
	Embedded bool `json:"embedded"`
}

// Handler : Serves /v1/knowledge.
type Handler struct {
	httpx.Responder
	keeper Keeper
}

// New : Builds the handler; a nil keeper answers that nothing is kept.
func New(logger *slog.Logger, keeper Keeper) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, keeper: keeper}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/knowledge", h.Keep)
}

// clientSources : What a client may put: not the server's own.
var clientSources = map[knowledge.Source]bool{knowledge.Notes: true, knowledge.Claude: true, knowledge.File: true}

// Keep : Keeps one source's text.
func (h *Handler) Keep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.keeper == nil {
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "Nothing is kept here.")
		return
	}
	var req Request
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	source := knowledge.Source(strings.TrimSpace(req.Source))
	ref := strings.TrimSpace(req.Ref)
	if !clientSources[source] || ref == "" || len(ref) > 255 {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "A source of notes, claude or file, and a ref, are needed.")
		return
	}
	did, err := h.keeper.Keep(ctx, authn.Of(ctx).User.ID, source, ref, req.Text)
	if errors.Is(err, knowledge.ErrUnavailable) {
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable, "Nothing to embed with.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "keeping what is known", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, Response{Embedded: did})
}
