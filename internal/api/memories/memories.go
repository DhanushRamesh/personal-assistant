// Package memories : Serves what the assistant has remembered about the
// person.
//
// Read-only. Memories are written by the assistant during a conversation,
// which is the only place it has the context to judge what is worth
// keeping; a screen that created them would be a notes app, and there are
// better notes apps.
//
// Seeing them matters for a different reason. A memory is believed
// indefinitely and corrected only by accident, so a wrong one quietly
// shapes every later answer. This is the only way to find that out.
package memories

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
)

// Store : Where memories are kept.
type Store interface {
	// All : Every memory of one tier.
	All(ctx context.Context, userID string, tier memory.Tier) ([]memory.Memory, error)
}

// Remembered : One memory, as a listing shows it.
type Remembered struct {
	ID      string `json:"id"`
	Tier    string `json:"tier"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// Searchable : Whether it can be found by meaning as well as by
	// wording.
	//
	// Worth showing: an unembedded memory is still stored and still found
	// by matching words, so nothing looks broken, and the only sign is
	// that a paraphrase stops finding it.
	Searchable bool       `json:"searchable"`
	Uses       int        `json:"uses"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ListResponse : Everything remembered, with the always-on ones first.
type ListResponse struct {
	Memories []Remembered `json:"memories"`
	Total    int          `json:"total"`
	// Always : How many are in every single prompt, which is the number
	// worth watching: they are paid for on every turn whether they matter
	// to it or not.
	Always int `json:"always"`
	// Unused : How many have never been given to the model at all.
	Unused int `json:"unused"`
}

// Handler : Serves the memory endpoints.
type Handler struct {
	httpx.Responder
	store Store
}

// New : Builds the handler.
func New(logger *slog.Logger, store Store) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/memories", h.List)
}

// List : What the assistant remembers.
//
// Always-on memories first, then the rest by how recently they were used.
// Not by when they were written: what matters about a memory is whether it
// is still earning its place, and the ones at the bottom of that order are
// the ones to question.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID

	out := ListResponse{Memories: []Remembered{}}
	for _, tier := range memory.Tiers() {
		got, err := h.store.All(ctx, user, tier)
		if err != nil {
			h.Fail(ctx, w, "reading memories", err)
			return
		}
		for _, m := range got {
			out.Memories = append(out.Memories, Remembered{
				ID: m.ID, Tier: string(m.Tier), Subject: m.Subject, Body: m.Body,
				Searchable: len(m.Embedding) > 0,
				Uses:       m.Uses, LastUsedAt: m.LastUsedAt,
				CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
			})
			out.Total++
			if m.Tier == memory.TierAlways {
				out.Always++
			}
			if m.Uses == 0 {
				out.Unused++
			}
		}
	}

	sort.SliceStable(out.Memories, func(i, j int) bool {
		a, b := out.Memories[i], out.Memories[j]
		if (a.Tier == string(memory.TierAlways)) != (b.Tier == string(memory.TierAlways)) {
			return a.Tier == string(memory.TierAlways)
		}
		if (a.LastUsedAt == nil) != (b.LastUsedAt == nil) {
			return a.LastUsedAt != nil
		}
		if a.LastUsedAt != nil && !a.LastUsedAt.Equal(*b.LastUsedAt) {
			return a.LastUsedAt.After(*b.LastUsedAt)
		}
		return a.CreatedAt.After(b.CreatedAt)
	})

	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}
