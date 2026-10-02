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
	"strconv"
	"strings"
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
	// Recent : What happened, newest first by when it happened.
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
	// Kinds : Which kinds exist, and how many of each.
	Kinds(ctx context.Context, userID string) ([]event.Kind, error)
}

const (
	// DefaultLimit : How many events a listing returns when none is asked
	// for.
	DefaultLimit = 100
	// MaxLimit : The most it will return at once.
	//
	// This is for a person looking at a screen. Anything that wants the
	// whole table wants a different endpoint, and building that before
	// anybody needs it would be guessing at its shape.
	MaxLimit = 500
)

// Stored : One event, as a listing shows it.
type Stored struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Device     string    `json:"device,omitempty"`
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
	ReceivedAt time.Time `json:"received_at"`
	// LateBy : Seconds between the two, so a screen does not have to work
	// out the one number that says the phone had been offline.
	LateBy  int64           `json:"late_by_seconds"`
	Payload json.RawMessage `json:"payload"`
}

// KindCount : One kind, and how much of it there is.
type KindCount struct {
	Kind  string    `json:"kind"`
	Count int64     `json:"count"`
	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

// ListResponse : A page of events, and what kinds exist.
//
// The kinds come with the list because nothing else can say what may be
// filtered on: any device may invent a kind, so the only honest answer is
// what has actually arrived.
type ListResponse struct {
	Events []Stored    `json:"events"`
	Kinds  []KindCount `json:"kinds"`
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
// Greeter : Somewhere to say hello when somebody comes in.
type Greeter interface {
	Greet(ctx context.Context, userID string)
}

// Fresh : How recently an arrival must have happened to be worth
// greeting.
//
// The phone spools what it sees and arrives with a backlog, so a
// flush after a day underground delivers this morning's arrival at
// midnight. Welcoming somebody home for the third time that day,
// hours after they got there, is worse than saying nothing.
const Fresh = 10 * time.Minute

type Handler struct {
	httpx.Responder
	store   Store
	naming  event.Naming
	greeter Greeter
	here    string
	now     func() time.Time
}

// Welcomes : Sets who says hello, and what the person calls the place
// the assistant is in. Without both, an arrival is recorded and
// nothing is said.
func (h *Handler) Welcomes(g Greeter, here string) *Handler {
	h.greeter, h.here = g, strings.TrimSpace(here)
	return h
}

// Naming : Sets what to ask about a place nobody has named. Without
// one, a stay somewhere new keeps its coordinates.
func (h *Handler) Naming(n event.Naming) *Handler { h.naming = n; return h }

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
	r.Get("/v1/events", h.List)
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

	h.settle(ctx, user, keep)
	h.welcome(ctx, user, keep, stored)

	httpx.WriteJSON(ctx, w, http.StatusOK, BatchResponse{
		Stored: stored, Seen: seen, Rejected: rejected})
}

// welcome : Says hello if one of these events is the person walking in.
//
// The arrival used to come from Home Assistant watching a watch's
// Bluetooth signal, which on 2 October 2026 announced six arrivals to
// somebody who had not moved. It comes from their own phone crossing
// the geofence they drew now -- one event, reported once, by the thing
// they actually carry.
//
// Only an arrival stored for the first time counts: a resend is the
// same crossing arriving twice, and the person did not walk in twice.
func (h *Handler) welcome(ctx context.Context, userID string, taken []*event.Event, stored []string) {
	if h.greeter == nil || h.here == "" || len(stored) == 0 {
		return
	}
	fresh := map[string]bool{}
	for _, key := range stored {
		fresh[key] = true
	}

	now := h.now()
	for _, e := range taken {
		if e.Kind != event.Entered || !fresh[e.DedupeKey] {
			continue
		}
		if !strings.EqualFold(placeIn(e.Payload), h.here) {
			continue
		}
		if now.Sub(e.OccurredAt) > Fresh {
			h.Logger.InfoContext(ctx, "an arrival was too old to greet",
				slog.Duration("ago", now.Sub(e.OccurredAt).Round(time.Minute)))
			continue
		}
		// Not waited for. Speaking blocks until the words have
		// finished playing, and the phone flushing its spool must not
		// hold the connection open for a greeting and three reminders.
		go h.greeter.Greet(context.WithoutCancel(ctx), userID)
		return
	}
}

// placeIn : What a crossing says the place is called.
func placeIn(payload json.RawMessage) string {
	var into struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil {
		return ""
	}
	return strings.TrimSpace(into.Value)
}

// settle : Works out which stays have ended, now that new readings have
// arrived.
//
// Done as readings land rather than on a timer, because a reading is
// the only thing that can end a stay: the one taken somewhere else is
// what says the last place was left. Nothing waits on it -- the phone
// is told its batch was taken either way -- so a failure is logged and
// the stays are worked out again on the next batch.
func (h *Handler) settle(ctx context.Context, userID string, taken []*event.Event) {
	fixes := false
	for _, e := range taken {
		if e.Kind == event.Fixed {
			fixes = true
			break
		}
	}
	if !fixes {
		return
	}

	stays, err := event.Settler{
		Store: h.store, Naming: h.naming, Logger: h.Logger,
	}.Settle(ctx, userID, h.now())
	if err != nil {
		h.Logger.ErrorContext(ctx, "cannot work out where they stayed", slog.Any("error", err))
		return
	}
	for _, s := range stays {
		h.Logger.InfoContext(ctx, "a stay ended",
			slog.String("where", s.Where()),
			slog.Duration("for", s.Long().Round(time.Minute)),
			slog.Time("from", s.From))
	}
}

// List : What the person's devices have reported.
//
// Newest first by when it happened, not by when it arrived. A phone that
// spent the morning without a signal delivers the morning at teatime, and
// ordering by arrival would scatter a day through the list.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID

	limit := DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That limit could not be read.")
			return
		}
		limit = min(n, MaxLimit)
	}

	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That time could not be read.")
			return
		}
		since = t
	}

	events, err := h.store.Recent(ctx, user, event.Query{
		Since: since,
		Kind:  r.URL.Query().Get("kind"),
		Limit: limit,
	})
	if err != nil {
		h.Fail(ctx, w, "reading events", err)
		return
	}

	// Counted across everything, not across the page. A filtered listing
	// still has to say which other kinds there are, or the filter has no
	// way back.
	kinds, err := h.store.Kinds(ctx, user)
	if err != nil {
		h.Fail(ctx, w, "counting event kinds", err)
		return
	}

	out := ListResponse{Events: make([]Stored, 0, len(events)),
		Kinds: make([]KindCount, 0, len(kinds))}
	for _, e := range events {
		out.Events = append(out.Events, Stored{
			ID: e.ID, Source: e.Source, Device: e.Device, Kind: e.Kind,
			OccurredAt: e.OccurredAt, ReceivedAt: e.ReceivedAt,
			LateBy:  int64(e.ReceivedAt.Sub(e.OccurredAt).Seconds()),
			Payload: e.Payload,
		})
	}
	for _, k := range kinds {
		out.Kinds = append(out.Kinds, KindCount{
			Kind: k.Kind, Count: k.Count, First: k.First, Last: k.Last})
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}
