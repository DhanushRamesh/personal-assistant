// Package presence serves the endpoint that says somebody has walked in.
//
// The deciding is done outside: Home Assistant watches how strong a watch's
// signal is and works out when it has crossed into the room. What arrives
// here is the conclusion, and what this does is choose the words and say
// them.
//
// The words are put together here rather than by the model on purpose. A
// greeting that arrives after the person has sat down is not a greeting,
// and a model call costs two to five seconds against the two the detection
// takes. So this says only what it can look up, which also means it cannot
// claim anything that is not so.
package presence

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// ArrivedResponse : What the caller is told was done.
type ArrivedResponse struct {
	// Said : The words spoken, or empty if nothing was.
	Said string `json:"said"`
	// Spoke : Whether anything was said out loud.
	Spoke bool `json:"spoke"`
	// Why : Why nothing was said, when nothing was.
	Why string `json:"why,omitempty"`
	// Delivered : How many held-back reminders were said along with the
	// greeting.
	Delivered int `json:"delivered,omitempty"`
}

// Handler : Serves the presence endpoints.
type Handler struct {
	httpx.Responder
	announcer announce.Announcer
	reminders remind.Store
	location  *time.Location
	now       func() time.Time
}

// New : Builds the handler. A nil announcer says nothing, which is what a
// server with no satellite has.
func New(logger *slog.Logger, announcer announce.Announcer, reminders remind.Store,
	location *time.Location, now func() time.Time) *Handler {

	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		announcer: announcer,
		reminders: reminders,
		location:  location,
		now:       now,
	}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/presence", func(r chi.Router) {
		r.Post("/arrived", h.Arrived)
	})
}

// Arrived : Somebody has come into the room. Greets them.
//
// Every call greets. Deciding whether somebody has really been away is
// the caller's job, and the caller has far better evidence for it: the
// rule upstream will not ask again until the watch has been faint for
// thirty unbroken seconds.
//
// This did once refuse to greet the same person twice within ten
// minutes, on the reasoning that nothing upstream promises to ask once.
// It was guarding against the wrong thing. Of three real arrivals in a
// quarter of an hour it refused two, which is the same silence as the
// fault it was there to prevent, and harder to explain.
func (h *Handler) Arrived(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID

	said := h.greeting(ctx, user)

	// Anything kept back while they were out is said now, after the
	// greeting, in the order it was for. Recorded as said only once it
	// has been: a delivery nobody heard must stay held.
	held := h.waiting(ctx, user)
	for i := range held {
		said += " " + remind.Spoken(held[i], h.clock(), h.where())
	}

	if h.announcer == nil || !h.announcer.Available() {
		httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
			Said: said, Why: "nothing is configured to speak",
		})
		return
	}
	if err := h.announcer.Say(ctx, said); err != nil {
		// Not the caller's fault and not worth a failure: it asked for a
		// greeting and the speaker was busy or unreachable. Said so
		// plainly rather than reported as success.
		h.Logger.WarnContext(ctx, "could not speak a greeting", slog.Any("error", err))
		httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
			Said: said, Why: "could not be spoken: " + err.Error(),
		})
		return
	}

	// Only now. Said and not recorded is better than recorded and not
	// said: the first is heard twice, the second is lost.
	for i := range held {
		if err := h.reminders.Fired(ctx, held[i].ID, h.clock(), time.Time{}); err != nil {
			h.Logger.WarnContext(ctx, "said a held reminder but could not record it",
				slog.String("reminder_id", held[i].ID), slog.Any("error", err))
		}
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
		Said: said, Spoke: true, Delivered: len(held),
	})
}

// waiting : What was kept back while they were out.
func (h *Handler) waiting(ctx context.Context, user string) []remind.Reminder {
	if h.reminders == nil || user == "" {
		return nil
	}
	held, err := h.reminders.Waiting(ctx, user)
	if err != nil {
		h.Logger.WarnContext(ctx, "cannot read what was held back", slog.Any("error", err))
		return nil
	}
	return held
}

// greeting : What to say to somebody who has just walked in.
//
// The hour, and what is waiting. Nothing else: everything in here is
// looked up as it is said, so there is nothing for it to be wrong about.
func (h *Handler) greeting(ctx context.Context, user string) string {
	parts := []string{h.hour() + ", sir."}

	if h.reminders != nil && user != "" {
		waiting, err := h.reminders.List(ctx, user, remind.Pending)
		if err != nil {
			h.Logger.WarnContext(ctx, "could not read what is waiting", slog.Any("error", err))
		} else if n := len(waiting); n == 1 {
			parts = append(parts, "One reminder is waiting.")
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d reminders are waiting.", n))
		}
	}
	return strings.Join(parts, " ")
}

// hour : A greeting for the time of day, in the person's own zone.
func (h *Handler) hour() string {
	switch at := h.clock().In(h.where()); {
	case at.Hour() < 5:
		return "Welcome back"
	case at.Hour() < 12:
		return "Good morning"
	case at.Hour() < 17:
		return "Good afternoon"
	case at.Hour() < 22:
		return "Good evening"
	default:
		return "Welcome back"
	}
}

// clock : Now, in UTC.
func (h *Handler) clock() time.Time {
	if h.now == nil {
		return time.Now().UTC()
	}
	return h.now().UTC()
}

// where : The person's zone.
func (h *Handler) where() *time.Location {
	if h.location == nil {
		return time.UTC
	}
	return h.location
}
