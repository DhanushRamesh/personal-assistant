// Package profile serves the standing description of the person, so they
// can read it and correct it.
//
// It is written by a model from a week of what they said, and prose
// written that way cannot be checked by anything but them. The first
// rebuild inferred a nationality and a city from a girlfriend's address
// and listed four symptoms mentioned in passing -- plausible, unasked
// for, and in every prompt from then on. The prompt was tightened, but
// tightening is a guess about tomorrow's mistake. Being able to read
// the thing and cross out what is wrong is the part that works whatever
// it gets wrong next.
package profile

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/profile"
)

// Response : The description, as the client reads it.
type Response struct {
	// Body : The description itself, empty when none has been written.
	Body string `json:"body"`
	// WrittenAt : When it was last written, zero when there is none.
	WrittenAt *time.Time `json:"written_at,omitempty"`
	// Mine : Whether the person last edited it themselves.
	//
	// Worth showing, because the nightly rebuild replaces whatever is
	// there: an edit made by hand survives until the next one.
	Mine bool `json:"mine"`
}

// Request : A correction.
type Request struct {
	Body string `json:"body"`
}

// Handler : Reads and writes the description.
type Handler struct {
	httpx.Responder

	memories memory.Store
	now      func() time.Time
}

// New : A handler over a memory store.
func New(memories memory.Store, now func() time.Time, logger *slog.Logger) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		memories:  memories,
		now:       now,
	}
}

// Mount : Puts the endpoints on the router.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/profile", func(r chi.Router) {
		r.Get("/", h.Read)
		r.Put("/", h.Write)
	})
}

// Read : The description, or an empty one.
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	held, err := h.find(r)
	if err != nil {
		h.Fail(ctx, w, "cannot read the description", err)
		return
	}
	if held == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, Response{})
		return
	}
	at := held.UpdatedAt
	httpx.WriteJSON(ctx, w, http.StatusOK, Response{
		Body:      held.Body,
		WrittenAt: &at,
		Mine:      held.EmbedModel == byHand,
	})
}

// Write : Replaces the description with the person's own words.
//
// An empty body deletes it rather than storing nothing: a description
// somebody has cleared should stop being read to the model, and an
// empty one in the always tier is a blank line in every prompt.
func (h *Handler) Write(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var ask Request
	if err := httpx.DecodeJSON(w, r, &ask); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	body := strings.TrimSpace(ask.Body)

	held, err := h.find(r)
	if err != nil {
		h.Fail(ctx, w, "cannot read the description", err)
		return
	}

	user := authn.Of(ctx).User.ID
	switch {
	case body == "" && held != nil:
		if err := h.memories.Forget(ctx, user, held.ID); err != nil {
			h.Fail(ctx, w, "cannot clear the description", err)
			return
		}
		httpx.WriteJSON(ctx, w, http.StatusOK, Response{})
		return

	case body == "":
		httpx.WriteJSON(ctx, w, http.StatusOK, Response{})
		return

	case held != nil:
		held.Body = body
		// Marked as theirs, and the embedding cleared because the words
		// it was made from have changed.
		held.EmbedModel = byHand
		held.Embedding = nil
		if err := h.memories.Update(ctx, held); err != nil {
			h.Fail(ctx, w, "cannot save the description", err)
			return
		}

	default:
		m, err := memory.New(user, memory.TierAlways, profile.Subject, body)
		if err != nil {
			httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
			return
		}
		m.EmbedModel = byHand
		if err := h.memories.Create(ctx, m); err != nil {
			h.Fail(ctx, w, "cannot save the description", err)
			return
		}
		held = m
	}

	at := held.UpdatedAt
	httpx.WriteJSON(ctx, w, http.StatusOK, Response{Body: held.Body, WrittenAt: &at, Mine: true})
}

// byHand : Recorded in EmbedModel to mark a description the person wrote.
//
// Not a new column. EmbedModel names what produced the vector, and a
// description typed by somebody has no vector and never had one, so the
// field is free and says something true.
const byHand = "by-hand"

// find : The person's description, or nil when they have none.
func (h *Handler) find(r *http.Request) (*memory.Memory, error) {
	ctx := r.Context()
	all, err := h.memories.All(ctx, authn.Of(ctx).User.ID, memory.TierAlways)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Subject == profile.Subject {
			return &all[i], nil
		}
	}
	return nil, nil
}
