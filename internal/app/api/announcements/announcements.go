// Package announcements hands each client what it still owes
// somebody, and takes back what it did about it.
//
// Owner, 6 October 2026: *"an announcement has to be published to
// the client, like a pub sub. Each client is subscribed ... the
// typed clients receive it, the voice client also receives it but
// to say or not depends on the presence, and presence logic should
// reside on the client. So for a typed client presence is always
// true; for a voice client presence depends on the trigger, my
// watch. When an event reaches a client it has two options, it
// should be marked as announced or pending ... so when next time
// presence is true, the call is made to the server and gets the
// pending announcements."*
//
// The shift this makes is that "was it announced" stops being one
// fact about one person. A warning read on a laptop and missed by a
// speaker in an empty room is both at once, and the server had been
// holding a single verdict for both -- decided centrally, from a
// sensor that describes one room and knows nothing about where the
// laptop is.
//
// So the server publishes and remembers, and says nothing about
// rooms. A client knows whether anybody is in front of it, which
// the server cannot, and is the only thing that can honestly answer
// whether a sentence was heard.
package announcements

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/domain/announcement"
	"personal-assistant/internal/domain/outbox"
)

// Store : Where what is owed is kept.
type Store interface {
	Pending(ctx context.Context, clientID string, limit int) ([]outbox.Delivery, error)
	Mark(ctx context.Context, clientID, owedID string, state outbox.State,
		tries int, why string, at time.Time) error
}

// Heard : Where an announcement a client said is noted, in the
// conversation it is talking in.
type Heard interface {
	HeardBy(ctx context.Context, userID string, c *client.Client, kind conversation.Kind, text string)
}

// Handler : Serves one client its own announcements.
type Handler struct {
	httpx.Responder
	store Store
	now   func() time.Time
	heard Heard
}

// Notes : Notes each announcement a client says it has said, in its
// conversation, so "what was that?" has something to answer with.
func (h *Handler) Notes(heard Heard) *Handler {
	h.heard = heard
	return h
}

// New : Builds one.
func New(logger *slog.Logger, store Store, now func() time.Time) *Handler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store, now: now}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/announcements", func(r chi.Router) {
		r.Get("/", h.Pending)
		r.Post("/{id}", h.Mark)
	})
}

// Announcement : One thing this client still owes somebody.
type Announcement struct {
	ID string `json:"id"`
	// Kind : Which kind of announcement, so a client can decide how
	// to present it. A reminder is not a greeting.
	Kind string `json:"kind"`
	// Say : The sentence, settled before it was stored. A client
	// never has to compose anything, and nothing here asks a model.
	Say string `json:"say"`
	// Due : When it became worth saying.
	Due time.Time `json:"due"`
	// Expires : When it stops being worth saying. Given to the
	// client so that a voice client coming back after a week can
	// see for itself that a thing is past, rather than reading out
	// a queue of yesterday.
	Expires time.Time `json:"expires"`
	// Away : What to do with it when nobody is there to hear it --
	// "hold" to keep it for when they come back, "drop" to let it
	// go.
	//
	// Sent to the client because the client is what decides. It
	// knows whether anybody is in front of it, which the server
	// cannot; this is the only half the server knows, which is
	// whether the thing is still true later. Kept here rather than
	// compiled into each client, so that changing the rule is one
	// edit on the server and not a release of three clients.
	Away string `json:"away"`
	// Tries : How many times this client has already tried.
	Tries int `json:"tries"`
}

// PendingResponse : The body of a listing.
type PendingResponse struct {
	Announcements []Announcement `json:"announcements"`
}

// Pending : What this client still owes somebody, oldest first.
//
// This client, not this person. Two clients of the same person owe
// different things, which is the whole point.
func (h *Handler) Pending(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	client := authn.Of(ctx).Client
	if client == nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			"Announcements belong to a client, and this request did not come from one.")
		return
	}
	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, PendingResponse{Announcements: []Announcement{}})
		return
	}

	owing, err := h.store.Pending(ctx, client.ID, outbox.Limit)
	if err != nil {
		h.Fail(ctx, w, "reading what is still to be announced", err)
		return
	}

	out := make([]Announcement, 0, len(owing))
	for _, d := range owing {
		a := Announcement{
			ID:   d.Owing.ID,
			Kind: string(d.Owing.Kind),
			// As it is to be heard: with the word said in front of a
			// cold one, as the satellite was given it.
			Say:     announcement.Spoken(d.Owing.Kind, d.Owing.Text),
			Due:     d.Owing.Due,
			Expires: d.Owing.Expires,
			Tries:   d.Tries,
		}
		if rule, ok := d.Owing.Kind.Rule(); ok {
			a.Away = string(rule.Away)
		}
		out = append(out, a)
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, PendingResponse{Announcements: out})
}

// MarkRequest : What a client did about one announcement.
type MarkRequest struct {
	// State : said, or dropped where the client will not try again.
	//
	// Pending is not accepted and is not needed: it is what a row
	// already is. A voice client in an empty room says nothing and
	// writes nothing, and the announcement is still waiting when
	// the watch comes back into range.
	State string `json:"state"`
	// Why : What went wrong, in the client's own words. For
	// somebody reading back why they never heard it.
	Why string `json:"why,omitempty"`
}

// Mark : Records what this client did about one announcement.
func (h *Handler) Mark(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	client := authn.Of(ctx).Client
	if client == nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			"Announcements belong to a client, and this request did not come from one.")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "Which announcement?")
		return
	}

	var req MarkRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	state := outbox.State(req.State)
	if state != outbox.Said && state != outbox.Dropped {
		httpx.WriteError(ctx, w, http.StatusBadRequest,
			`An announcement is marked "said" or "dropped".`)
		return
	}
	if h.store == nil {
		httpx.WriteError(ctx, w, http.StatusServiceUnavailable,
			"There is nowhere to record that.")
		return
	}

	// What it was, read before it is marked, so a said one can be noted
	// where its answer will arrive.
	var owed *outbox.Delivery
	if state == outbox.Said && h.heard != nil {
		if pending, err := h.store.Pending(ctx, client.ID, outbox.Limit); err == nil {
			for i := range pending {
				if pending[i].Owing.ID == id {
					owed = &pending[i]
				}
			}
		}
	}

	// Counted here rather than sent by the client. A client that
	// reports its own attempt count can report anything, and the
	// count is what bounds a thing that fails every time.
	if err := h.store.Mark(ctx, client.ID, id, state, 1, req.Why, h.now()); err != nil {
		h.Fail(ctx, w, "recording what was announced", err)
		return
	}
	if owed != nil {
		h.heard.HeardBy(ctx, authn.Of(ctx).User.ID, client, owed.Owing.Kind, owed.Owing.Text)
	}
	w.WriteHeader(http.StatusNoContent)
}
