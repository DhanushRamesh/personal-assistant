// Package reminders serves the endpoints for seeing what is waiting to be
// said, putting one off, and calling one off.
//
// The assistant can already do all of it by being asked. This is for
// looking at the list, which is not a thing to do out loud.
package reminders

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/api/views"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// ListResponse : What a listing answers with.
type ListResponse struct {
	Reminders []views.Reminder `json:"reminders"`
}

// SnoozeRequest : How long to put a reminder off for.
type SnoozeRequest struct {
	// Minutes : From now. Zero or absent selects DefaultSnooze.
	Minutes int `json:"minutes"`
}

// SnoozeResponse : What putting one off answers with.
type SnoozeResponse struct {
	// Reminder : The one that will now go off at the later time.
	Reminder views.Reminder `json:"reminder"`
	// Added : Whether this is a new one-off beside an untouched series,
	// rather than the reminder itself moved. A client that says "put off"
	// either way would be telling somebody their daily alarm had shifted.
	Added bool `json:"added"`
}

// DefaultSnooze : How long a reminder is put off for when no length is
// given. The same ten minutes the spoken tool uses.
const DefaultSnooze = 10

// MaxSnooze : The longest, in minutes. A year, which is past anything
// somebody means by "later".
const MaxSnooze = 525600

// Handler : Serves the reminder endpoints.
type Handler struct {
	httpx.Responder
	store remind.Store
}

// New : Builds the handler from the store holding the reminders. A nil
// store serves empty listings, which is what a server with nowhere to keep
// them has.
func New(logger *slog.Logger, store remind.Store) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store}
}

// Mount : Registers the reminder endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/reminders", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/{id}/snooze", h.Snooze)
		r.Delete("/{id}", h.Cancel)
	})
}

// List : What is waiting to be said, and what was never said at all.
//
// Missed ones are included because leaving them out was how a reminder
// disappeared: never spoken, not on this screen, and nothing to show it
// had ever existed. Held ones for the same reason -- they are waiting,
// they just cannot be said until somebody is there to hear them.
//
// What already happened is left out unless asked for. past=true adds
// the ones that were said, which is what a past reminder is: one that
// reached somebody. all=true adds cancelled ones as well, for anything
// that wants the whole record.
//
// Either way it is asked for rather than shown, because a screen that
// opens on a log buries the two things actually coming.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Reminders: []views.Reminder{}})
		return
	}

	// Held belongs here too: it is waiting to be said, and leaving it
	// out made a reminder kept back while somebody was out of the room
	// vanish from the one screen that lists them.
	states := []remind.Status{remind.Pending, remind.Held, remind.Missed}
	switch q := r.URL.Query(); {
	case q.Get("all") == "true":
		// Everything there has ever been, cancelled ones included.
		states = nil
	case q.Get("past") == "true":
		// The ones that were actually said, which is what somebody
		// means by a past reminder: one they got. A cancelled one was
		// never received, it stopped existing, and listing it among
		// things that happened says it happened.
		states = append(states, remind.Done)
	}

	found, err := h.store.List(ctx, authn.Of(ctx).User.ID, states...)
	if err != nil {
		h.Fail(ctx, w, "listing reminders", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Reminders: views.OfReminders(found)})
}

// Snooze : Puts one off until later.
//
// A repeating one is not moved. The series stays where it is and a single
// one-off is made beside it, which the answer says so the screen can too.
func (h *Handler) Snooze(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if h.store == nil || !remind.ValidID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}

	// An empty body is ten minutes, which is what the button sends.
	var req SnoozeRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That is not a request this understands.")
			return
		}
	}
	if req.Minutes == 0 {
		req.Minutes = DefaultSnooze
	}
	if req.Minutes < 0 || req.Minutes > MaxSnooze {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "Put off by how long?")
		return
	}

	user := authn.Of(ctx).User.ID
	existing, err := h.store.Get(ctx, user, id)
	if errors.Is(err, remind.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading reminder", err)
		return
	}

	until := time.Now().UTC().Add(time.Duration(req.Minutes) * time.Minute)
	added, err := remind.Later(ctx, h.store, existing, until)
	switch {
	case errors.Is(err, remind.ErrNotSnoozable):
		httpx.WriteError(ctx, w, http.StatusConflict,
			"That one is "+string(existing.Status)+", so there is nothing to put off.")
		return
	case err != nil:
		h.Fail(ctx, w, "putting a reminder off", err)
		return
	}

	if added != nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, SnoozeResponse{
			Reminder: views.OfReminder(*added), Added: true,
		})
		return
	}

	existing.DueAt, existing.Status = until, remind.Pending
	httpx.WriteJSON(ctx, w, http.StatusOK, SnoozeResponse{Reminder: views.OfReminder(*existing)})
}

// Cancel : Calls one off.
//
// Answered as missing when it belongs to somebody else, so the existence
// of another person's reminder is not revealed either.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if h.store == nil || !remind.ValidID(id) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}

	user := authn.Of(ctx).User.ID
	existing, err := h.store.Get(ctx, user, id)
	if errors.Is(err, remind.ErrNotFound) {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such reminder.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "reading reminder", err)
		return
	}

	if err := h.store.Cancel(ctx, user, id); err != nil {
		h.Fail(ctx, w, "cancelling reminder", err)
		return
	}

	existing.Status = remind.Cancelled
	httpx.WriteJSON(ctx, w, http.StatusOK, views.OfReminder(*existing))
}
