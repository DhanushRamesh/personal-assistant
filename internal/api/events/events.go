// Package events : Serves the endpoint the person's own devices report to.
//
// Called by Tasker on the phone, directly and not through Home Assistant:
// Home Assistant is the voice and the hands, and what the person does
// should not have to pass through the room to be remembered.
//
// A batch rather than one at a time. The phone is offline exactly when the
// interesting things happen -- underground, abroad, asleep -- so it spools
// what it sees and arrives with a backlog. One request per event over
// mobile data is slow, costs battery, and multiplies the chance of losing
// some of them.
//
// The reply names every key it was given and says what became of it, so
// the phone can truncate its spool exactly. A count would not do: the
// first time two flushes overlap, deleting "the first n lines" deletes the
// wrong ones.
package events

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
)

// maxRequestBody : The largest batch that will be read.
//
// MaxBatch events at MaxPayload each would be eight megabytes, which no
// phone should ever send and this should never buffer. The real ceiling is
// the batch length, checked after decoding; this is only so that something
// pointed at the endpoint by mistake cannot hold memory open.
const maxRequestBody = 4 << 20

// Store : Where events are kept.
type Store interface {
	// Record : Stores what is not stored already, and says which keys were
	// already known.
	Record(ctx context.Context, userID string, events []*event.Event) (stored, seen []string, err error)
}

// Incoming : One event as a device sends it.
type Incoming struct {
	// Kind : What happened, as a dotted name.
	Kind string `json:"kind"`
	// OccurredAt : When it happened, by the device's clock.
	OccurredAt time.Time `json:"occurred_at"`
	// Payload : Whatever this kind carries. May be absent.
	Payload json.RawMessage `json:"payload,omitempty"`
	// DedupeKey : What makes a resend harmless.
	DedupeKey string `json:"dedupe_key"`
}

// Batch : What one flush of a device's spool looks like.
type Batch struct {
	// Source : What kind of thing is reporting -- tasker, watch, laptop.
	Source string `json:"source"`
	// Device : Which particular one. Optional while there is only one.
	Device string `json:"device,omitempty"`
	// Events : What it saw, oldest first by convention and in any order
	// in practice.
	Events []Incoming `json:"events"`
}

// Rejected : One event that could not be stored, and why.
type Rejected struct {
	// DedupeKey : Which one, so the phone can act on it.
	DedupeKey string `json:"dedupe_key"`
	// At : Where it was in the batch, for one with no usable key.
	At int `json:"at"`
	// Why : What was wrong with it.
	Why string `json:"why"`
}

// BatchResponse : What became of everything in the batch.
//
// Stored and seen may both be deleted from the spool: one is now kept and
// the other was already. Rejected may not, and the phone should stop
// resending them -- they will be rejected identically forever.
type BatchResponse struct {
	Stored   []string   `json:"stored"`
	Seen     []string   `json:"seen"`
	Rejected []Rejected `json:"rejected,omitempty"`
}

// Handler : Serves the event endpoints.
type Handler struct {
	httpx.Responder
	store Store
	now   func() time.Time
}

// New : Builds the handler.
func New(logger *slog.Logger, store Store, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store, now: now}
}

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/events", h.Record)
}

// Record : Takes a batch of events from one of the person's devices.
//
// A bad event does not fail the batch. The phone cannot fix a malformed
// one by sending it again, and refusing the whole flush over it would mean
// a single broken profile stops every other event the phone has -- the
// failure would be total, permanent, and nowhere near its cause.
func (h *Handler) Record(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID
	received := h.now().UTC()

	var batch Batch
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	raw, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That batch could not be read.")
		return
	}
	if err := json.Unmarshal(raw, &batch); err != nil {
		// Logged, because the thing sending this is a phone and whoever
		// is holding it cannot see why it was refused. "That batch could
		// not be read" is true and useless; the reason names the field
		// and what was wrong with it, which is usually a variable the
		// phone did not substitute.
		//
		// The reason only, never the body: this carries what somebody
		// did today.
		// The reason, never the body: this carries what somebody did
		// today. The reason alone named every fault during the phone's
		// setup -- an unsubstituted variable, a splitter that did not
		// split, a wifi name with quotes in it -- except the last, where
		// the body was logged for one afternoon and then taken out
		// again.
		h.Logger.WarnContext(ctx, "a batch could not be read",
			slog.String("reason", err.Error()))
		httpx.WriteError(ctx, w, http.StatusBadRequest, "That batch could not be read.")
		return
	}
	if len(batch.Events) == 0 {
		httpx.WriteJSON(ctx, w, http.StatusOK, BatchResponse{})
		return
	}
	if len(batch.Events) > event.MaxBatch {
		httpx.WriteError(ctx, w, http.StatusRequestEntityTooLarge,
			"That batch is too large to take at once.")
		return
	}

	keep := make([]*event.Event, 0, len(batch.Events))
	var rejected []Rejected
	for i, in := range batch.Events {
		e, err := event.New(user, batch.Source, batch.Device, in.Kind,
			in.OccurredAt, received, in.Payload, in.DedupeKey)
		if err != nil {
			rejected = append(rejected, Rejected{
				DedupeKey: in.DedupeKey, At: i, Why: err.Error()})
			continue
		}
		keep = append(keep, e)
	}

	stored, seen, err := h.store.Record(ctx, user, keep)
	if err != nil {
		h.Fail(ctx, w, "storing events", err)
		return
	}

	h.Logger.InfoContext(ctx, "events taken from a device",
		slog.String("source", batch.Source),
		slog.String("device", batch.Device),
		slog.Int("stored", len(stored)),
		slog.Int("seen", len(seen)),
		slog.Int("rejected", len(rejected)))

	httpx.WriteJSON(ctx, w, http.StatusOK, BatchResponse{
		Stored: stored, Seen: seen, Rejected: rejected})
}
