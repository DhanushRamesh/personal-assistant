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
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/announce"
	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// Quiet : How soon after greeting somebody they may be greeted again.
//
// Nothing downstream promises to ask only once. A rule watching a signal
// that swings fifteen decibels with arm position will sooner or later
// decide somebody has arrived twice, and being welcomed into a room you
// have been sitting in is the failure that makes the whole thing feel
// broken.
const Quiet = 10 * time.Minute

// ArrivedResponse : What the caller is told was done.
type ArrivedResponse struct {
	// Said : The words spoken, or empty if nothing was.
	Said string `json:"said"`
	// Spoke : Whether anything was said out loud.
	Spoke bool `json:"spoke"`
	// Why : Why nothing was said, when nothing was.
	Why string `json:"why,omitempty"`
}

// Handler : Serves the presence endpoints.
type Handler struct {
	httpx.Responder
	announcer announce.Announcer
	reminders remind.Store
	location  *time.Location
	now       func() time.Time

	mu      sync.Mutex
	greeted map[string]time.Time
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
		greeted:   map[string]time.Time{},
	}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/presence", func(r chi.Router) {
		r.Post("/arrived", h.Arrived)
	})
}

// Arrived : Somebody has come into the room. Greets them, once.
func (h *Handler) Arrived(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID

	if !h.claim(user) {
		httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
			Why: "already greeted recently",
		})
		return
	}

	said := h.greeting(ctx, user)

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
		h.release(user)
		httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
			Said: said, Why: "could not be spoken: " + err.Error(),
		})
		return
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{Said: said, Spoke: true})
}

// claim : Whether this greeting may go ahead, marking it as taken.
func (h *Handler) claim(user string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	at := h.clock()
	if last, ok := h.greeted[user]; ok && at.Sub(last) < Quiet {
		return false
	}
	h.greeted[user] = at
	return true
}

// release : Gives the claim back, for a greeting that was never spoken.
func (h *Handler) release(user string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.greeted, user)
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
