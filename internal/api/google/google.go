// Package google serves the connecting and disconnecting of a Google
// account.
//
// Three endpoints and no tools. Whatever ends up reaching Gmail or
// Calendar asks the link for a client; this is only how the permission
// gets there and how somebody sees whether it is still good.
package google

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
)

// AccountResponse : What is connected, and whether it still works.
//
// No token, ever. Not the refresh token and not an access token: the
// screen has no use for either, and a secret that reaches a browser is
// a secret in a log somewhere.
type AccountResponse struct {
	// Connected : Whether there is a usable permission.
	Connected bool `json:"connected"`
	// Email : Which Google account, when one is connected.
	Email string `json:"email,omitempty"`
	// Scopes : What was actually granted, which is not always what was
	// asked for.
	Scopes []string `json:"scopes,omitempty"`
	// ConnectedAt, RefreshedAt : When permission was given, and when it
	// was last known to work.
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	RefreshedAt *time.Time `json:"refreshed_at,omitempty"`
	// Broken : Why it stopped working, when it has. Empty while it
	// works.
	Broken string `json:"broken,omitempty"`
	// Configurable : Whether this server has client credentials at all.
	// False means there is nothing to connect and the screen should say
	// so rather than offer a button that cannot work.
	Configurable bool `json:"configurable"`
}

// BeginResponse : Where to send somebody to grant permission.
type BeginResponse struct {
	// URL : The Google consent screen, carrying the state that will
	// come back.
	URL string `json:"url"`
	// Scopes : What will be asked for, so a screen can say so before
	// sending somebody off to a page full of permissions.
	Scopes []string `json:"scopes"`
}

// Handler : Serves the Google endpoints.
type Handler struct {
	httpx.Responder
	link *google.Link
	// done : Where the browser is sent after the callback, which is
	// the settings screen rather than a bare JSON body.
	done string
}

// New : Builds the handler. A nil link means nothing is configured,
// which every endpoint reports rather than failing.
func New(logger *slog.Logger, link *google.Link, done string) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, link: link, done: done}
}

// Mount : Registers the endpoints that a signed-in caller uses.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/google", func(r chi.Router) {
		r.Get("/account", h.Account)
		r.Delete("/account", h.Disconnect)
		r.Post("/authorize", h.Authorize)
	})
}

// MountPublic : Registers the callback, which cannot require a token.
//
// What arrives here is a browser Google has just redirected, and a
// redirect carries no Authorization header. Mounted inside the
// authenticated group it answered 401 every time and the connection
// could never be completed -- which is how this was found, by trying
// it rather than by reading it.
//
// The state is what stands in for a token, and is what OAuth state is
// for. It is 32 bytes from crypto/rand, issued by this server against
// one person, spent on first use, and dead after ten minutes. A
// caller who cannot produce one gets nowhere, and one who replays a
// spent one gets nowhere either.
func (h *Handler) MountPublic(r chi.Router) {
	r.Get("/v1/google/callback", h.Callback)
}

// Account : What is connected.
func (h *Handler) Account(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := AccountResponse{Configurable: h.link != nil}

	account, err := h.link.Account(ctx, authn.Of(ctx).User.ID)
	switch {
	case errors.Is(err, google.ErrNotConfigured), errors.Is(err, google.ErrNotConnected):
		httpx.WriteJSON(ctx, w, http.StatusOK, out)
		return
	case err != nil:
		h.Fail(ctx, w, "reading the Google connection", err)
		return
	}

	out.Connected = account.Connected()
	out.Email = account.Email
	out.Scopes = account.Scopes
	out.ConnectedAt = &account.ConnectedAt
	out.RefreshedAt = account.RefreshedAt
	out.Broken = account.Broken
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

// Authorize : Starts the granting of permission.
//
// Returns the address rather than redirecting, because the caller is a
// script in a page and a redirect it cannot follow is no use to it.
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	url, _, err := h.link.Begin(authn.Of(ctx).User.ID)
	if errors.Is(err, google.ErrNotConfigured) {
		httpx.WriteError(ctx, w, http.StatusNotImplemented,
			"This server has no Google client configured.")
		return
	}
	if err != nil {
		h.Fail(ctx, w, "starting a Google sign-in", err)
		return
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, BeginResponse{URL: url, Scopes: h.link.Wanted()})
}

// Callback : Where Google sends the person back.
//
// Answers by redirecting to the settings screen with the outcome in the
// query, because what arrives here is a browser and a person, not a
// program: a JSON body would leave them looking at raw text.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	// A refusal on the consent screen comes back here as an error, not
	// as a missing code. It is not a failure of this server and is not
	// logged as one.
	if refused := q.Get("error"); refused != "" {
		h.back(w, r, refused)
		return
	}

	_, err := h.link.Complete(ctx, q.Get("state"), q.Get("code"))
	switch {
	case err == nil:
		h.back(w, r, "")
	case errors.Is(err, google.ErrUnknownState):
		h.back(w, r, "expired")
	case errors.Is(err, google.ErrWrongAccount):
		h.back(w, r, "wrong_account")
	default:
		h.Logger.WarnContext(ctx, "could not finish a Google sign-in", slog.Any("error", err))
		h.back(w, r, "failed")
	}
}

// back : Sends the browser to the settings screen, saying how it went.
func (h *Handler) back(w http.ResponseWriter, r *http.Request, failure string) {
	to := h.done
	if to == "" {
		// Nowhere to send them, so say it here rather than redirect to
		// nothing.
		if failure == "" {
			httpx.WriteJSON(r.Context(), w, http.StatusOK,
				map[string]string{"status": "connected"})
			return
		}
		httpx.WriteError(r.Context(), w, http.StatusBadRequest,
			"That sign-in did not complete: "+failure)
		return
	}
	if failure != "" {
		to += "?google=" + failure
	} else {
		to += "?google=connected"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// Disconnect : Forgets the permission.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := h.link.Disconnect(ctx, authn.Of(ctx).User.ID)
	if err != nil && !errors.Is(err, google.ErrNotConfigured) {
		h.Fail(ctx, w, "disconnecting Google", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
