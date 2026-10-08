// Package remarks shows what the assistant considered saying unasked.
//
// The feature's ordinary output is silence, and silence has four
// causes: nothing arrived, nothing was unusual, the moment was
// refused, or the model decided it was not worth saying. From outside
// all four look the same, and so does a watcher that has stopped
// running. Owner: *"I should see in UI when it triggered, why it
// didn't speak, if spoken what it spoke."*
package remarks

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/base/notice"
)

// DefaultLimit, MaxLimit : How many are returned.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Store : Where what it considered saying is kept.
type Store interface {
	Recent(ctx context.Context, userID string, limit int) ([]notice.Noted, error)
}

// Handler : Serves them.
type Handler struct {
	httpx.Responder
	store Store
}

// New : Builds one.
func New(logger *slog.Logger, store Store) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/remarks", h.List)
}

// Remark : One thing it considered saying.
type Remark struct {
	At time.Time `json:"at"`
	// Label : The thing, as its own sender described it.
	Label string `json:"label"`
	// Reason : What made it worth looking at: often, novel.
	Reason string `json:"reason"`
	// Outcome : What came of it: said, held, declined, failed.
	Outcome string `json:"outcome"`
	// Said : What was spoken. Empty for every outcome but said,
	// which is most of them and is the point of keeping them.
	Said string `json:"said,omitempty"`
	// Because : Why it came to nothing, where the outcome alone does
	// not say enough.
	Because string `json:"because,omitempty"`
}

// ListResponse : The body of a listing.
type ListResponse struct {
	Remarks []Remark `json:"remarks"`
}

// List : What it has considered saying, newest first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That limit could not be read.")
			return
		}
		limit = min(n, MaxLimit)
	}

	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Remarks: []Remark{}})
		return
	}

	held, err := h.store.Recent(ctx, authn.Of(ctx).User.ID, limit)
	if err != nil {
		h.Fail(ctx, w, "reading what it considered saying", err)
		return
	}

	out := make([]Remark, 0, len(held))
	for _, n := range held {
		out = append(out, Remark{
			At: n.At, Label: n.Label, Reason: string(n.Reason),
			Outcome: string(n.Outcome), Said: n.Said, Because: n.Because,
		})
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Remarks: out})
}
