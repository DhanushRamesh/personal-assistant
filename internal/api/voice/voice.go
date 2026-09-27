// Package voice serves what the speech side of the assistant is configured
// with.
//
// Read-only, and on the settings screen rather than buried in a container's
// arguments, because the word list is built from what the person has
// written and can therefore contain a word they never said. "Kitla BMRs"
// was stored as a reminder title and would be primed as vocabulary from
// there. Showing the list is what makes that visible and fixable: correct
// the reminder and the word goes.
package voice

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/vocabulary"
)

// VocabularyResponse : The word list speech-to-text is primed with.
type VocabularyResponse struct {
	// Prompt : Exactly what is passed to Whisper.
	Prompt string `json:"prompt"`
	// Core : The words primed by hand, which are not derivable from
	// anything stored.
	Core []string `json:"core"`
	// Found : The words taken from what the person has written, in the
	// order they would be dropped if the budget ran out.
	Found []string `json:"found"`
	// Used and Budget : How much of the room Whisper allows is taken.
	Used   int `json:"used"`
	Budget int `json:"budget"`
}

// Handler : Serves the voice endpoints.
type Handler struct {
	httpx.Responder
	sources vocabulary.Sources
}

// New : Builds the handler from wherever the person's own words are kept.
func New(logger *slog.Logger, sources vocabulary.Sources) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, sources: sources}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/voice", func(r chi.Router) {
		r.Get("/vocabulary", h.Vocabulary)
	})
}

// Vocabulary : What speech-to-text is primed with, and where it came from.
func (h *Handler) Vocabulary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID

	said, err := h.sources.Gather(ctx, user)
	if err != nil {
		h.Fail(ctx, w, "gathering vocabulary", err)
		return
	}

	found := vocabulary.Found(said)
	prompt := vocabulary.Prompt(vocabulary.Core, found)

	httpx.WriteJSON(ctx, w, http.StatusOK, VocabularyResponse{
		Prompt: prompt,
		Core:   vocabulary.Words(vocabulary.Core),
		// What actually made it in, not what was offered: a term dropped
		// for being a core word misheard, or for not fitting, is not
		// being primed and should not be listed as though it were.
		Found:  vocabulary.Kept(prompt, vocabulary.Core),
		Used:   len(prompt),
		Budget: vocabulary.Budget,
	})
}
