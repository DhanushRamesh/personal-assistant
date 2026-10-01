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
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/speech"
)

// maxRequestBody : A transcript and a number. Generous for the longest
// recording the limit allows, and far short of anything worth buffering.
const maxRequestBody = 64 << 10

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
	cut *speech.Cut
}

// New : Builds the handler.
func New(logger *slog.Logger, cut *speech.Cut) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, cut: cut}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/speech", func(r chi.Router) {
		r.Post("/cut", h.Cut)
	})
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
