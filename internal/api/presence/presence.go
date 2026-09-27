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
	"math/rand/v2"
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
	// Missed : How many never-said reminders were reported, and thereby
	// marked as told.
	Missed int `json:"missed,omitempty"`
}

// Handler : Serves the presence endpoints.
type Handler struct {
	httpx.Responder
	announcer announce.Announcer
	reminders remind.Store
	location  *time.Location
	now       func() time.Time

	mu sync.Mutex
	// lastGreeting : So the same words are not used twice running.
	lastGreeting string
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

	said := h.greeting()

	// Anything kept back while they were out is said now, after the
	// greeting, in the order it was for. Recorded as said only once it
	// has been: a delivery nobody heard must stay held.
	held := h.waiting(ctx, user)
	if len(held) > 0 {
		said += " " + remind.Delivered(held, h.clock(), h.where())
	}

	// And anything that was never said at all, which is the one thing
	// worth stopping somebody at the door for. Read once here and marked
	// as told below, so the same miss is not raised again in their next
	// sentence.
	unsaid := h.neverSaid(ctx, user)
	if len(unsaid) > 0 {
		said += " " + missedText(unsaid, h.where())
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

	// Saying it at the door is the telling. Without this the person
	// hears about the same miss here and again in their next sentence.
	if len(unsaid) > 0 {
		if err := h.reminders.Mentioned(ctx, remind.IDs(unsaid), h.clock()); err != nil {
			h.Logger.WarnContext(ctx, "told somebody about a miss but could not record it",
				slog.Any("error", err))
		}
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, ArrivedResponse{
		Said: said, Spoke: true, Delivered: len(held), Missed: len(unsaid),
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
// The hour and nothing else. What is still to come is left out on
// purpose: a reminder waiting for four o'clock is not news at half past
// one, and counting them at the door turns a greeting into a status
// report.
func (h *Handler) greeting() string { return h.Greeting() }

// Greeting : The greeting alone, without anything waiting. Exported so a
// test can ask for it at an hour it chooses.
func (h *Handler) Greeting() string {
	return h.hour()
}

// missedText : What to say about reminders that were never said.
//
// The words themselves, not a count. A count tells somebody they have
// lost something without telling them what, which is the worst of both.
func missedText(unsaid []remind.Reminder, loc *time.Location) string {
	var b strings.Builder
	if len(unsaid) == 1 {
		b.WriteString("One reminder was missed while you were out.")
	} else {
		fmt.Fprintf(&b, "%d reminders were missed while you were out.", len(unsaid))
	}
	for i := range unsaid {
		b.WriteString(" At ")
		b.WriteString(unsaid[i].DueAt.In(loc).Format("3:04"))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(unsaid[i].Body))
	}
	return b.String()
}

// neverSaid : Misses the person has not been told about yet.
func (h *Handler) neverSaid(ctx context.Context, user string) []remind.Reminder {
	if h.reminders == nil || user == "" {
		return nil
	}
	unsaid, err := h.reminders.Unmentioned(ctx, user)
	if err != nil {
		h.Logger.WarnContext(ctx, "could not read what was never said", slog.Any("error", err))
		return nil
	}
	return unsaid
}

// greetings : What to say, by the hour.
//
// Several of each, because one fixed line per part of the day is the
// same sentence every morning for ever, and a greeting somebody can
// recite along with is not a greeting.
//
// "Welcome back" is in every band rather than tied to the hour. It used
// to be the small hours and the late evening only, which made it mean
// "it is late" while sounding like "you have returned".
var greetings = map[string][]string{
	"night":     {"Hello, sir.", "Welcome back, sir.", "Still up, sir."},
	"morning":   {"Good morning, sir.", "Morning, sir.", "Welcome back, sir."},
	"afternoon": {"Good afternoon, sir.", "Afternoon, sir.", "Welcome back, sir."},
	"evening":   {"Good evening, sir.", "Evening, sir.", "Welcome back, sir."},
}

// band : Which set the hour falls in, in the person's own zone.
func (h *Handler) band() string {
	switch at := h.clock().In(h.where()); {
	case at.Hour() < 5:
		return "night"
	case at.Hour() < 12:
		return "morning"
	case at.Hour() < 17:
		return "afternoon"
	case at.Hour() < 22:
		return "evening"
	default:
		return "night"
	}
}

// hour : A greeting for the time of day, and not the last one used.
func (h *Handler) hour() string {
	choices := greetings[h.band()]
	if len(choices) == 0 {
		return "Hello, sir."
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Anything but the one before. Variety that can repeat itself twice
	// running is not much variety when there are only two of them.
	pick := choices[rand.IntN(len(choices))]
	for len(choices) > 1 && pick == h.lastGreeting {
		pick = choices[rand.IntN(len(choices))]
	}
	h.lastGreeting = pick
	return pick
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
