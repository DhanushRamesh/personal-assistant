// Package places : Serves the places a person draws on the client's
// map -- a name, a centre and a radius -- which the server then
// watches their fixes against.
//
// Owner, 16 October 2026: *"I can plot a point and set the radius and
// name that as location .. it should be added as geofence and not from
// Tasker"*. The crossings themselves are worked out where the fixes
// arrive, in the events module; this is only the keeping of the places
// and the reading of where somebody stands with respect to each.
package places

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/domain/places"
)

// Place : One place, as a client reads it.
type Place struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	RadiusM   int       `json:"radius_m"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Inside : Whether the latest crossing the server wrote for it was
	// into it.
	Inside bool `json:"inside"`
	// Since : When that crossing was; absent when there has been none.
	Since *time.Time `json:"since,omitempty"`
}

// ListResponse : Everything a person keeps.
type ListResponse struct {
	Places []Place `json:"places"`
}

// CreateRequest : A new place. Every field is required.
type CreateRequest struct {
	Name    string   `json:"name"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	RadiusM *int     `json:"radius_m"`
}

// PatchRequest : What to change about a place. Absent fields are kept.
type PatchRequest struct {
	Name    *string  `json:"name"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	RadiusM *int     `json:"radius_m"`
}

// Handler : Serves the place endpoints.
type Handler struct {
	httpx.Responder
	store  places.Store
	events places.Reader
	now    func() time.Time
}

// New : Builds the handler. events is where the crossings and stays are
// read from; without it every place reads as never entered and nothing
// is seeded.
func New(logger *slog.Logger, store places.Store, events places.Reader, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store, events: events, now: now}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Route("/v1/places", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Patch("/{id}", h.Patch)
		r.Delete("/{id}", h.Delete)
	})
}

// List : Every place they keep, by name, with whether they are inside
// each.
//
// The first time, Home and Office are made from the stays already
// written there (places.Seed). Done here, lazily, because this is the
// first moment anybody is looking; a failure to seed is logged and the
// listing goes on without it.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID
	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Places: []Place{}})
		return
	}
	if h.events != nil {
		made, err := places.Seed(ctx, h.store, h.events, user, h.now())
		if err != nil {
			h.Logger.WarnContext(ctx, "cannot make home and office from the stays", slog.Any("error", err))
		}
		for _, p := range made {
			h.Logger.InfoContext(ctx, "a place was made from the stays",
				slog.String("name", p.Name), slog.Float64("lat", p.Lat), slog.Float64("lng", p.Lng))
		}
	}
	kept, err := h.store.List(ctx, user)
	if err != nil {
		h.Fail(ctx, w, "listing places", err)
		return
	}
	out := ListResponse{Places: make([]Place, 0, len(kept))}
	for _, p := range kept {
		v, err := h.view(r, p)
		if err != nil {
			h.Fail(ctx, w, "reading where they are", err)
			return
		}
		out.Places = append(out.Places, v)
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

// Create : Keeps a new place.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil {
		httpx.WriteError(ctx, w, http.StatusNotFound, "Places are not kept here.")
		return
	}
	var req CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	var missing places.Problems
	if req.Lat == nil {
		missing = append(missing, "A place needs a latitude.")
	}
	if req.Lng == nil {
		missing = append(missing, "A place needs a longitude.")
	}
	if req.RadiusM == nil {
		missing = append(missing, "A place needs a radius.")
	}
	radius := places.MinRadius
	if req.RadiusM != nil {
		// A missing radius is said once, as missing, rather than a
		// second time as a zero out of range.
		radius = *req.RadiusM
	}
	now := h.now().UTC()
	p := places.Place{
		ID: places.NewID(), UserID: authn.Of(ctx).User.ID, Name: req.Name,
		Lat: deref(req.Lat), Lng: deref(req.Lng), Radius: radius,
		CreatedAt: now, UpdatedAt: now,
	}
	p, err := places.Clean(p)
	if len(missing) > 0 {
		err = append(missing, named(err)...)
	}
	if err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.Add(ctx, p); err != nil {
		h.refuse(w, r, "keeping a place", p.Name, err)
		return
	}
	h.Logger.InfoContext(ctx, "a place was drawn",
		slog.String("place_id", p.ID), slog.String("name", p.Name), slog.Int("radius_m", p.Radius))
	h.rename(ctx, p)
	h.answer(w, r, http.StatusCreated, p)
}

// Patch : Changes what was sent about a place and keeps the rest.
//
// Renaming one starts it afresh: its crossings were written under the
// old name, so under the new one it has never been entered, and the
// next fix inside enters it.
func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such place.")
		return
	}
	var req PatchRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := h.store.Get(ctx, authn.Of(ctx).User.ID, chi.URLParam(r, "id"))
	if err != nil {
		h.refuse(w, r, "reading a place", "", err)
		return
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Lat != nil {
		p.Lat = *req.Lat
	}
	if req.Lng != nil {
		p.Lng = *req.Lng
	}
	if req.RadiusM != nil {
		p.Radius = *req.RadiusM
	}
	p.UpdatedAt = h.now().UTC()
	p, err = places.Clean(p)
	if err != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.Change(ctx, p); err != nil {
		h.refuse(w, r, "changing a place", p.Name, err)
		return
	}
	h.Logger.InfoContext(ctx, "a place was changed",
		slog.String("place_id", p.ID), slog.String("name", p.Name), slog.Int("radius_m", p.Radius))
	h.rename(ctx, p)
	h.answer(w, r, http.StatusOK, p)
}

// Delete : Forgets a place. Its crossings stay in the record, as
// everything that happened does.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil {
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such place.")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.store.Remove(ctx, authn.Of(ctx).User.ID, id); err != nil {
		h.refuse(w, r, "deleting a place", "", err)
		return
	}
	h.Logger.InfoContext(ctx, "a place was deleted", slog.String("place_id", id))
	w.WriteHeader(http.StatusNoContent)
}

// rename : Gives the place's name to the stays already inside it. A
// failure is logged; the place is kept either way.
func (h *Handler) rename(ctx context.Context, p places.Place) {
	amender, ok := h.events.(places.Amender)
	if !ok {
		return
	}
	n, err := places.Rename(ctx, amender, p.UserID, p)
	if err != nil {
		h.Logger.WarnContext(ctx, "the stays in a place could not be renamed",
			slog.String("place_id", p.ID), slog.String("error", err.Error()))
		return
	}
	if n > 0 {
		h.Logger.InfoContext(ctx, "the stays in a place were renamed",
			slog.String("place_id", p.ID), slog.String("name", p.Name), slog.Int("stays", n))
	}
}

// answer : Writes one place, with where they stand.
func (h *Handler) answer(w http.ResponseWriter, r *http.Request, status int, p places.Place) {
	ctx := r.Context()
	v, err := h.view(r, p)
	if err != nil {
		h.Fail(ctx, w, "reading where they are", err)
		return
	}
	httpx.WriteJSON(ctx, w, status, v)
}

// refuse : Answers a store's refusal in words, or fails.
func (h *Handler) refuse(w http.ResponseWriter, r *http.Request, doing, name string, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, places.ErrNotFound):
		httpx.WriteError(ctx, w, http.StatusNotFound, "No such place.")
	case errors.Is(err, places.ErrTaken):
		httpx.WriteError(ctx, w, http.StatusConflict, "You already have a place called "+name+".")
	default:
		h.Fail(ctx, w, doing, err)
	}
}

// view : A place as a client reads it.
func (h *Handler) view(r *http.Request, p places.Place) (Place, error) {
	v := Place{ID: p.ID, Name: p.Name, Lat: p.Lat, Lng: p.Lng, RadiusM: p.Radius,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	if h.events == nil {
		return v, nil
	}
	s, err := places.Where(r.Context(), h.events, p.UserID, p)
	if err != nil {
		return Place{}, err
	}
	v.Inside = s.Inside
	if !s.Since.IsZero() {
		since := s.Since.UTC()
		v.Since = &since
	}
	return v, nil
}

// named : The problems in an error from places.Clean, if any.
func named(err error) places.Problems {
	var p places.Problems
	if errors.As(err, &p) {
		return p
	}
	return nil
}

// deref : The value, or zero.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
