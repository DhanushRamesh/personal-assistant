// Package speech : Serves the endpoint that reports a cut-off recording.
//
// Called by the speech-to-text bridge, not by Home Assistant. The bridge is
// the only thing in the chain that knows how long a recording ran: Home
// Assistant stops listening at a fixed limit, decides internally that it has
// timed out, and then passes on the words without saying so.
//
// The report arrives before the question does. The bridge calls this and
// only then hands the transcript back, so by the time Home Assistant turns
// those words into a chat, the server already knows they are a fragment.
package speech

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/world/speech"
)

// maxHeard : The longest run of words a hearing notice carries; anything
// longer is cut from the front, keeping what is being said now.
const maxHeard = 600

// Notifier : Where a hearing notice is sent: the live hub.
type Notifier interface {
	Notify(e livehub.Event)
}

// HearingRequest : The words heard so far in a turn.
type HearingRequest struct {
	Text string `json:"text"`
	// Final : Whether the turn has ended and these are all its words.
	Final bool `json:"final"`
	// Wait : How many seconds the turn will still wait for another word
	// before the bridge ends it, as of now; absent when it will not.
	Wait *float64 `json:"wait,omitempty"`
}

// longestWait : The most a hearing notice may say the turn will wait. The
// bridge waits five seconds at most; anything far past that is a fault,
// and a client counting down a minute would show it as one.
const longestWait = 30.0

// maxRequestBody : A transcript and a number. Generous for the longest
// recording the limit allows, and far short of anything worth buffering.
const maxRequestBody = 64 << 10

// ElsewhereRequest : A spoken turn that Home Assistant answered itself.
type ElsewhereRequest struct {
	// Heard : What the person said.
	Heard string `json:"heard"`
	// Said : What Home Assistant answered.
	Said string `json:"said"`
}

// Recorder : Where a turn answered elsewhere is passed on: the live hub, which tells the clients.
type Recorder interface {
	Elsewhere(ctx context.Context, userID, heard, said string)
}

// CutRequest : What the bridge reports.
type CutRequest struct {
	// Text : The transcript of everything that was captured.
	Text string `json:"text"`
	// Seconds : How long the recording ran before it was stopped.
	Seconds float64 `json:"seconds"`
}

// CutResponse : That the report was understood.
type CutResponse struct {
	// Noted : Whether this will be matched against a coming question.
	Noted bool `json:"noted"`
}

// Handler : Serves the speech endpoints.
type Handler struct {
	httpx.Responder
	cut  *speech.Cut
	tell Notifier
	rec  Recorder
}

// New : Builds the handler.
func New(logger *slog.Logger, cut *speech.Cut) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, cut: cut}
}

// Tells : Gives the handler somewhere to send the words being heard.
// Without it they are accepted and go nowhere.
func (h *Handler) Tells(n Notifier) *Handler {
	h.tell = n
	return h
}

// Records : Gives the handler somewhere to write turns Home Assistant
// answered itself. Without it they are accepted and go nowhere.
func (h *Handler) Records(r Recorder) *Handler {
	h.rec = r
	return h
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/speech", func(r chi.Router) {
		r.Post("/cut", h.Cut)
		r.Post("/hearing", h.Hearing)
		r.Post("/answered", h.Answered)
	})
}

// Hearing : The words heard so far while somebody speaks, passed straight
// to that person's clients over their live connection, to be shown as
// they are said. Nothing is kept: it is a view of the microphone, not a
// record -- the turn itself is recorded when it arrives as a question.
func (h *Handler) Hearing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req HearingRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	text := req.Text
	if runes := []rune(text); len(runes) > maxHeard {
		text = string(runes[len(runes)-maxHeard:])
	}
	if h.tell != nil {
		h.tell.Notify(livehub.Event{Kind: livehub.Hearing, Text: text, Final: req.Final, Wait: clampWait(req.Wait), UserID: authn.Of(ctx).User.ID, At: time.Now().UTC()})
	}
	w.WriteHeader(http.StatusNoContent)
}

// Answered : A spoken turn Home Assistant answered from its own sentences,
// reported by it after answering. The server never saw the question; the
// clients are told, for the core to show what was said, and nothing is
// written: such a turn is not part of the conversation.
func (h *Handler) Answered(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ElsewhereRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}
	if h.rec != nil {
		h.rec.Elsewhere(ctx, authn.Of(ctx).User.ID, req.Heard, req.Said)
	}
	w.WriteHeader(http.StatusNoContent)
}

// clampWait : A wait the clients can count down: none for none, and never
// below nothing or past longestWait.
func clampWait(w *float64) *float64 {
	if w == nil || math.IsNaN(*w) {
		return nil
	}
	v := math.Min(math.Max(*w, 0), longestWait)
	return &v
}

// Cut : A recording was stopped while somebody was still speaking.
//
// Answers that it was noted rather than silently accepting, so the bridge
// can log when a report was ignored. An empty transcript is accepted and not
// recorded: a recording can run its full length and catch no words, and
// matching that against every future empty question would mark unrelated
// turns as fragments.
func (h *Handler) Cut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CutRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That request could not be read.")
		return
	}

	h.cut.Note(req.Text, req.Seconds)
	_, noted := h.cut.Was(req.Text)

	h.Logger.InfoContext(ctx, "a recording was cut off",
		slog.Float64("seconds", req.Seconds),
		slog.Int("characters", len(req.Text)),
		slog.Bool("noted", noted))

	httpx.WriteJSON(ctx, w, http.StatusOK, CutResponse{Noted: noted})
}
