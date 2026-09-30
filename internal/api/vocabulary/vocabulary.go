// Package vocabulary serves the names heard in conversation, for the
// machine with the microphones to fetch.
//
// Read only. The list is written by the nightly job and by nothing
// else: a name typed in here would be replaced the same evening,
// which is worse than not offering it. Words somebody wants kept
// permanently belong in the hand-kept file on the voice machine,
// which this never touches and which survives every rebuild.
package vocabulary

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// Response : The list, as whatever fetched it reads it.
type Response struct {
	// Terms : The names, one per entry.
	Terms []string `json:"terms"`
	// Count : How many, so a caller that only wants the number does
	// not have to count them. Home Assistant shows this as the state
	// of a sensor, which may be no longer than 255 characters.
	Count int `json:"count"`
	// WrittenAt : When the oldest of the lists was written, zero when
	// none has been. Oldest rather than newest so that a list which
	// has partly gone stale reads as stale.
	WrittenAt *time.Time `json:"written_at,omitempty"`
}

// Handler : Serves the list.
type Handler struct {
	httpx.Responder
	store vocabulary.Store
}

// New : A handler over a store.
func New(store vocabulary.Store, logger *slog.Logger) *Handler {
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		store:     store,
	}
}

// Mount : Puts the endpoint on the router.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/vocabulary", h.Read)
}

// Read : Every name anybody has said.
//
// Everybody's rather than the caller's own. What fetches this is a
// microphone, and a microphone cannot tell who is about to speak: a
// list narrowed to one person would mishear the other one.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, Response{Terms: []string{}})
		return
	}

	terms, at, err := h.store.Everyone(ctx)
	if err != nil {
		h.Fail(ctx, w, "cannot read the names", err)
		return
	}

	// An empty list is [] and not null, so that whatever reads this
	// can iterate it without checking.
	if terms == nil {
		terms = []string{}
	}
	out := Response{Terms: terms, Count: len(terms)}
	if !at.IsZero() {
		out.WrittenAt = &at
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}
