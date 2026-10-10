// Package about : Says what the assistant is called, to anyone who asks.
//
// A sign-in screen shows the name before there is a token to present, so
// this is open. The name is the owner's choice from [assistant] name and is
// not a secret: the assistant says it out loud.
package about

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/httpx"
)

// Response : The body of GET /v1/about.
type Response struct {
	// Name : What the assistant calls itself. Empty when it has no name,
	// and a client then shows none rather than one of its own.
	Name string `json:"name"`
}

// Handler : Serves the assistant's name.
type Handler struct {
	name string
}

// New : Builds the handler for an assistant called name.
func New(name string) *Handler {
	return &Handler{name: name}
}

// Mount : Registers the endpoint on r, outside authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/about", h.About)
}

// About : Answers with the assistant's name.
func (h *Handler) About(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(r.Context(), w, http.StatusOK, Response{Name: h.name})
}
