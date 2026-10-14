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
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/base/world/event"
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
	// Amend : Rewrites one already recorded, matched on its dedupe key.
	// Settling needs it: a stay that has not ended yet keeps the key
	// its start gives it and grows.
	Amend(ctx context.Context, userID string, events []*event.Event) (int64, error)
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
	// Sources : What has reported, each source and device with how
	// many, counted across everything as the kinds are -- the choices a
	// filter by device offers.
	Sources []SourceCount `json:"sources"`
}

// SourceCount : One thing that reports events, and how many.
type SourceCount struct {
	Source string `json:"source"`
	Device string `json:"device,omitempty"`
	Count  int64  `json:"count"`
}

// Late : How far behind an event has to arrive to count as late, when
// only those are asked for. The client calls anything past a minute
// late, and so does this.
const Late = time.Minute

// sourcer : A store that can say what has reported. Optional: one that
// cannot lists no sources.
type sourcer interface {
	Sources(ctx context.Context, userID string) ([]event.Source, error)
}

// Incoming : One event as a device sends it.
type Incoming struct {
	// Kind : What happened, as a dotted name.
	Kind string `json:"kind"`
	// OccurredAt : When it happened, by the device's clock.
	//
	// Optional. Absent means the moment it arrived, which is right for
	// anything reported as it happens and wrong for anything spooled:
	// a device that holds events while offline must send this or a
	// whole morning is filed under the minute the signal returned.
	OccurredAt time.Time `json:"occurred_at,omitempty"`
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
	store  Store
	naming event.Naming
	// arrivals : Told which kinds landed, for rules that wait on one.
	arrivals Arrivals
	now      func() time.Time
}

// Naming : Sets what to ask about a place nobody has named. Without
// one, a stay somewhere new keeps its coordinates.
func (h *Handler) Naming(n event.Naming) *Handler { h.naming = n; return h }

// Arrivals : Where to say that readings have landed. Optional.
func (h *Handler) Arrivals(a Arrivals) *Handler { h.arrivals = a; return h }

// New : Builds the handler.
// Arrivals : Told which kinds of event have just been stored.
//
// For rules that wait on a reading rather than on a time. Done as
// readings land rather than on a timer, for the same reason the
// stays are: a reading is the only thing that can make one of
// these rules true, so there is nothing to poll for.
//
// Must not block. The phone is answered either way.
type Arrivals interface {
	Arrived(userID string, kinds []string)
}

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
	r.Get("/v1/events/ledger", h.Ledger)
}

// LedgerLine : One reporter's one action, as the ledger answers it.
type LedgerLine struct {
	Source string    `json:"source"`
	Device string    `json:"device,omitempty"`
	Kind   string    `json:"kind"`
	Count  int64     `json:"count"`
	First  time.Time `json:"first"`
	Last   time.Time `json:"last"`
	// EverySeconds : The usual gap between two of them; absent for one.
	EverySeconds float64 `json:"every_seconds,omitempty"`
	Unknown      int64   `json:"unknown"`
	Valueless    int64   `json:"valueless"`
	Repeats      int64   `json:"repeats"`
}

// LedgerPair : Two actions of one family counted side by side for one value.
type LedgerPair struct {
	Source string `json:"source"`
	Device string `json:"device,omitempty"`
	Family string `json:"family"`
	Value  string `json:"value"`
	A      string `json:"a"`
	B      string `json:"b"`
	NA     int64  `json:"a_count"`
	NB     int64  `json:"b_count"`
	Off    bool   `json:"off"`
}

// LedgerFault : One way a reporter's books are wrong.
type LedgerFault struct {
	Source string `json:"source"`
	Device string `json:"device,omitempty"`
	Kind   string `json:"kind"`
	Says   string `json:"says"`
}

// LedgerResponse : How each reporter has kept its books over the window.
type LedgerResponse struct {
	Days   int           `json:"days"`
	Lines  []LedgerLine  `json:"lines"`
	Pairs  []LedgerPair  `json:"pairs"`
	Faults []LedgerFault `json:"faults"`
}

// Ledger : Each source's each action over the past days -- how many, how
// often, when last, how cleanly kept -- with the pairs that should balance
// and what is wrong. ?days= chooses the window, 7 by default.
func (h *Handler) Ledger(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	books, ok := h.store.(event.Ledgers)
	if !ok {
		httpx.WriteError(ctx, w, http.StatusNotFound, "Nothing here keeps a ledger of the reports.")
		return
	}
	days := event.LedgerDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That number of days could not be read.")
			return
		}
		days = min(n, event.LedgerMostDays)
	}
	now := h.now().UTC()
	lines, pairs, faults, err := event.Ledgered(ctx, books, authn.Of(ctx).User.ID, now.AddDate(0, 0, -days), now, time.UTC)
	if err != nil {
		h.Fail(ctx, w, "keeping the ledger", err)
		return
	}
	out := LedgerResponse{Days: days, Lines: []LedgerLine{}, Pairs: []LedgerPair{}, Faults: []LedgerFault{}}
	for _, l := range lines {
		out.Lines = append(out.Lines, LedgerLine{Source: l.Source, Device: l.Device, Kind: l.Kind, Count: l.Count,
			First: l.First, Last: l.Last, EverySeconds: l.Every().Seconds(),
			Unknown: l.Unknown, Valueless: l.Valueless, Repeats: l.Repeats})
	}
	for _, p := range pairs {
		out.Pairs = append(out.Pairs, LedgerPair{Source: p.Source, Device: p.Device, Family: p.Family, Value: p.Value,
			A: p.A, B: p.B, NA: p.NA, NB: p.NB, Off: p.Off()})
	}
	for _, f := range faults {
		out.Faults = append(out.Faults, LedgerFault{Source: f.Source, Device: f.Device, Kind: f.Kind, Says: f.Says})
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
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
		// No occurred_at means now.
		//
		// A device reporting something the moment it happens has
		// nothing to say about when that was that this does not
		// already know, and making it say so anyway is the single
		// most awkward thing to produce on a phone: an RFC 3339
		// timestamp in UTC, from a tool whose clock variable is epoch
		// seconds. Every fault during the phone's setup was a
		// variable that had not been substituted, and this is the
		// field most likely to be one.
		//
		// A device that spools must still send it. Events held while
		// offline and flushed at teatime all arrive at once, and
		// stamping them with now would file a whole morning under the
		// moment the signal came back -- which is why this is a
		// default and not a replacement.
		at := in.OccurredAt
		payload := unpacked(in.Payload)
		if at.IsZero() {
			at, payload = stamped(payload, received)
		}
		e, err := event.New(user, batch.Source, batch.Device, in.Kind,
			at, received, labelled(payload), in.DedupeKey)
		if err != nil {
			rejected = append(rejected, Rejected{
				DedupeKey: in.DedupeKey, At: i, Why: err.Error()})
			continue
		}
		keep = append(keep, e)
	}

	keep, again := h.unrepeated(ctx, user, keep)

	stored, seen, err := h.store.Record(ctx, user, keep)
	if err != nil {
		h.Fail(ctx, w, "storing events", err)
		return
	}
	seen = append(seen, again...)

	h.Logger.InfoContext(ctx, "events taken from a device",
		slog.String("source", batch.Source),
		slog.String("device", batch.Device),
		slog.Int("stored", len(stored)),
		slog.Int("seen", len(seen)),
		slog.Int("sent_again", len(again)),
		slog.Int("rejected", len(rejected)))

	h.settle(ctx, user, keep)
	h.tell(user, keep)

	httpx.WriteJSON(ctx, w, http.StatusOK, BatchResponse{
		Stored: stored, Seen: seen, Rejected: rejected})
}

// unrepeated : The events in a batch that are not the same report sent
// again -- the same source, device, action and payload within
// event.Twice of one already stored or earlier in the batch -- and the
// dedupe keys of those that were, to be answered as seen so the device
// deletes them.
//
// A device's own key cannot catch these: Android fires a Bluetooth
// connection once per link, so one helmet connecting arrived as two or
// three reports a second apart, each with a key of its own. One read of
// the store for the whole batch, over the span the batch covers.
func (h *Handler) unrepeated(ctx context.Context, userID string, batch []*event.Event) ([]*event.Event, []string) {
	if len(batch) == 0 {
		return batch, nil
	}
	first, last := batch[0].OccurredAt, batch[0].OccurredAt
	kinds := map[string]bool{}
	from := map[string]bool{}
	for _, e := range batch {
		if e.OccurredAt.Before(first) {
			first = e.OccurredAt
		}
		if e.OccurredAt.After(last) {
			last = e.OccurredAt
		}
		kinds[e.Kind] = true
		from[e.Source+"/"+e.Device] = true
	}
	q := event.Query{Since: first.Add(-event.Twice), Until: last.Add(event.Twice)}
	for k := range kinds {
		q.Kinds = append(q.Kinds, k)
	}
	for f := range from {
		q.From = append(q.From, f)
	}
	stored, err := h.store.Recent(ctx, userID, q)
	if err != nil {
		// Better a repeat kept than a report lost.
		h.Logger.WarnContext(ctx, "cannot look for reports sent again", slog.Any("error", err))
		stored = nil
	}

	keep := make([]*event.Event, 0, len(batch))
	var again []string
	for _, e := range batch {
		repeat := false
		for i := range stored {
			if stored[i].DedupeKey != e.DedupeKey && event.Again(&stored[i], e) {
				repeat = true
				break
			}
		}
		for _, k := range keep {
			if !repeat && event.Again(k, e) {
				repeat = true
			}
		}
		if repeat {
			again = append(again, e.DedupeKey)
			continue
		}
		keep = append(keep, e)
	}
	return keep, again
}

// tell : Says which kinds landed, for the rules that wait on one.
//
// The kinds only. What a rule needs beyond that it reads for
// itself, because it needs the reading before this one as well,
// and that is a question for the store rather than for whatever
// happened to be in this batch.
func (h *Handler) tell(userID string, taken []*event.Event) {
	if h.arrivals == nil || len(taken) == 0 {
		return
	}
	seen := make(map[string]bool, len(taken))
	kinds := make([]string, 0, len(taken))
	for _, e := range taken {
		if e == nil || seen[e.Kind] {
			continue
		}
		seen[e.Kind] = true
		kinds = append(kinds, e.Kind)
	}
	h.arrivals.Arrived(userID, kinds)
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
			slog.String("called", s.Called),
			slog.String("where", s.Where()),
			slog.Duration("for", s.Long().Round(time.Minute)),
			slog.Time("from", s.From))
	}
}

// unpacked : A payload whose value is itself an object, flattened.
//
// For a device that can only send one field. The phone builds its
// payload in a snippet that takes a kind and a string and writes
// {"value": that string}, and it has no third parameter: Tasker's
// Perform Task passes two. So a reading with anything besides a name
// to report -- how long a call lasted, how long a stay was -- cannot
// be expressed at all.
//
// Rather than have the phone's one working sender rewritten to carry
// arbitrary JSON, which is a code field edited with a thumb and the
// only thing standing between this server and every event it gets, the
// device may put JSON in the string and this unfolds it. The result is
// an ordinary payload, so everything downstream -- the four-week
// counts, the lines in a greeting, the description -- sees what it
// would have seen anyway.
//
// Only an object, and only when "value" is the sole field. A payload
// that already carries more was built by something that did not need
// this, and a value that happens to look like a number or a list is
// left exactly as it is: "42" is a reading, not a structure.
//
// What comes out need not carry a value of its own. It used to have
// to, on the reasoning that something keyed on a value is what every
// reader expects -- true, and already guaranteed by [labelled], which
// runs next and calls an absent value unknown. Insisting on it here
// as well meant power.plugged, which reports only a level, was left
// wrapped: its value became the literal text {"level":"54"}, so the
// level was unreachable and the label was a different piece of
// nonsense every time the battery differed.
func unpacked(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return payload
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(payload, &outer); err != nil || len(outer) != 1 {
		return payload
	}
	raw, ok := outer["value"]
	if !ok {
		return payload
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return payload
	}
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return payload
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(text), &inner); err != nil || len(inner) == 0 {
		// Brackets at both ends: somebody was building an object and
		// did not finish. A value that merely begins with one --
		// "{not json" -- is a value, and is left exactly as it is.
		if strings.HasSuffix(text, "}") {
			return unparsed(text)
		}
		return payload
	}
	return json.RawMessage(text)
}

// unparsed : A structure the phone meant to send and did not finish
// building.
//
// Tasker assembles this JSON by joining strings, so a field holding
// a quotation mark ends it early and the rest is not JSON at all. A
// search for `"ramin djawadi ironm" in Search` did exactly that, and
// the whole broken object stayed in the value -- which made the
// label a sentence of its own, unique to that one song, counted as
// a thing the person had done once.
//
// Kept rather than dropped, beside the reading instead of as the
// reading: the same treatment [labelled] gives a telephone number
// that has no business being a name. Nothing is lost, and nothing
// that cannot identify anything is allowed to become a label.
func unparsed(text string) json.RawMessage {
	out, err := json.Marshal(map[string]string{"unparsed": text})
	if err != nil {
		// Unreachable: a string always marshals.
		return nil
	}
	return out
}

// labelled : A payload whose value can actually identify something.
//
// Everything that counts, folds or pairs events keys on the kind and
// the value together, so a value that is missing is a label that every
// occurrence shares. Four calls from one stranger and four calls from
// four strangers then look the same, which is the opposite of the one
// thing worth noticing about an unknown number.
//
// Two ways a value arrives useless, and both are the phone's doing.
//
// It can be empty: a caller who is not in the contacts has no name,
// and Tasker has no way to say "this one or that one" inside a
// parameter. So the device sends both and this prefers the name --
// "Priya" where there is one, the number where there is not.
//
// Or it can be a variable that was never substituted, which arrives
// looking like "%CNAME". Every fault during the phone's setup was one
// of these, and until now each landed as a real reading of a place
// called %CNAME. Treated as absent, so the fallback applies and the
// event is still worth something.
func labelled(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return payload
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil {
		return payload
	}

	value, _ := into["value"].(string)
	switch {
	case unset(value):
		// Nothing was sent. "unknown" either way, and the number that
		// came with it stays in the payload rather than becoming the
		// label.
	case event.Number(value):
		// A bare telephone number is not a name, whoever sent it.
		//
		// A cellular call says nothing at all when the caller is a
		// stranger, so the case above covers it. A call from an app
		// does not: the name comes from the notification, and Meet
		// puts the person's number in the title when they are not in
		// the contacts. That arrives set, so a test for being set
		// passes it through and the digits become the label -- the one
		// thing the owner asked to be rid of.
		//
		// Moved rather than dropped, so the record still has it when
		// the sender had nowhere else to put it.
		if number, _ := into["number"].(string); unset(number) {
			into["number"] = strings.TrimSpace(value)
		}
	default:
		return payload
	}

	into["value"] = event.Unknown
	out, err := json.Marshal(into)
	if err != nil {
		return payload
	}
	return out
}

// Stale : How far back inside the payload a device may say something
// happened.
//
// Long enough for the case this exists for -- a notification that
// outlives what it was about -- and short enough that a clock set
// wrongly, or a variable holding last week, cannot file something in
// the middle of a day that is already written.
const Stale = 6 * time.Hour

// Ahead : How far into the future a device's own time may be and
// still be believed, which is only as far as two clocks disagree.
const Ahead = 2 * time.Minute

// Earliest : A sanity floor. Epoch seconds that are really
// milliseconds land in the year 56000, and a variable that never
// substituted parses as nothing; both are caught before this.
var Earliest = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// stamped : When the payload says it happened, and the payload with
// that taken out of it.
//
// The batch already carries occurred_at per event, and the comment
// where it is read says why a phone rarely fills it in: it wants
// RFC 3339 in UTC from a tool whose clock is epoch seconds, and it is
// the field most likely to arrive unsubstituted.
//
// This is the other way round. A sender writes the moment into the
// event's own text, in the units it already has, and the server lifts
// it out. Nothing in here knows which sender did it or what for.
//
// The case it was built for: Spotify's notification outlives the
// music by minutes -- measured at six minutes forty on 3 October 2026
// -- so the phone learns that playing stopped long after it did. It
// keeps overwriting the last moment it saw music and sends that.
// Anything else that finds out late can do the same.
//
// Believed only within reason. A time outside the window is ignored
// rather than refused: the event is still worth having, and the hour
// it arrived is a better guess than a number nobody can explain.
func stamped(payload json.RawMessage, received time.Time) (time.Time, json.RawMessage) {
	if len(payload) == 0 {
		return received, payload
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil {
		return received, payload
	}
	raw, ok := into["occurred_at"]
	if !ok {
		return received, payload
	}
	delete(into, "occurred_at")
	rest, err := json.Marshal(into)
	if err != nil {
		rest = payload
	}

	at, ok := moment(raw)
	switch {
	case !ok:
	case at.Before(Earliest):
	case at.Before(received.Add(-Stale)):
	case at.After(received.Add(Ahead)):
	default:
		return at, rest
	}
	// Taken out of the payload even when it is not believed. Left in,
	// it reaches a prompt as a field called occurred_at that
	// disagrees with the time beside it.
	return received, rest
}

// moment : A time written the two ways a device writes one.
//
// Epoch seconds, because that is what Tasker's %TIMES holds and what
// a shell gives. RFC 3339 because that is what everything else gives.
// A number arrives from JSON as a float and as a string when it was
// quoted, which it will be, because it was written inside a quoted
// field by hand.
func moment(raw any) (time.Time, bool) {
	switch v := raw.(type) {
	case float64:
		return time.Unix(int64(v), 0).UTC(), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" || strings.HasPrefix(v, "%") {
			return time.Time{}, false
		}
		if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(secs, 0).UTC(), true
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// unset : Whether a field says nothing.
//
// Three ways a phone says nothing, and none of them is an absent key.
//
// Empty, which needs no explanation.
//
// A variable the device never filled in: Tasker leaves the reference
// in place when a variable has no value, so "%CNAME" means there was
// no caller name, not that somebody is called that.
//
// And zero. Measured on a real missed call, 2 October 2026: the caller
// arrived correctly as Alekhya Chintada, and forty-three seconds later
// the same profile fired again with both the name and the number set
// to the string "0". That is set, so a test for being set let it
// through, and left alone it would build four weeks of history for a
// person called 0 who rings constantly. A name is not a number and a
// telephone number is not one digit, so zero is nothing either way.
func unset(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return true
	}
	if !strings.HasPrefix(s, "%") {
		return false
	}
	for _, r := range s[1:] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return len(s) > 1
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

	var since, until time.Time
	for name, at := range map[string]*time.Time{"since": &since, "until": &until} {
		if v := r.URL.Query().Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httpx.WriteError(ctx, w, http.StatusBadRequest, "That time could not be read.")
				return
			}
			*at = t
		}
	}

	// Several kinds, devices and words at once. Owner, 13 October 2026,
	// on the desktop client's events: *"it should have advanced
	// filters"*. Asked of the store and not of a page already fetched,
	// so a filter reaches past the five hundred a page can hold.
	query := r.URL.Query()
	kinds := nonEmpty(query["kind"])
	late := time.Duration(0)
	if query.Get("late") == "true" {
		late = Late
	}

	// Position readings are left out unless they are asked for by name.
	//
	// A phone reports one every five minutes, so a screen of what
	// somebody's devices have done is three hundred coordinates a day
	// and everything that actually happened pushed off the end. They
	// are working material: settling turns runs of them into one
	// place.stayed, and that is the line worth reading.
	//
	// Asked for by name they are shown, which is the difference
	// between this and the events tool. A person typing
	// kind=location.fix wants to look at the readings; the model is
	// never shown them at all, because a kind listing offering three
	// thousand of something is an invitation to ask for it.
	var omit []string
	if query.Get("fixes") != "true" && !slices.Contains(kinds, event.Fixed) {
		omit = []string{event.Fixed}
	}

	events, err := h.store.Recent(ctx, user, event.Query{
		Since:    since,
		Until:    until,
		Kinds:    kinds,
		From:     nonEmpty(query["from"]),
		Contains: strings.TrimSpace(query.Get("contains")),
		Late:     late,
		Omit:     omit,
		Limit:    limit,
	})
	if err != nil {
		h.Fail(ctx, w, "reading events", err)
		return
	}

	// Counted across everything, not across the page. A filtered listing
	// still has to say which other kinds there are, or the filter has no
	// way back.
	counted, err := h.store.Kinds(ctx, user)
	if err != nil {
		h.Fail(ctx, w, "counting event kinds", err)
		return
	}

	out := ListResponse{Events: make([]Stored, 0, len(events)),
		Kinds: make([]KindCount, 0, len(counted)), Sources: []SourceCount{}}
	if src, ok := h.store.(sourcer); ok {
		from, err := src.Sources(ctx, user)
		if err != nil {
			h.Fail(ctx, w, "counting event sources", err)
			return
		}
		for _, f := range from {
			out.Sources = append(out.Sources, SourceCount{Source: f.Source, Device: f.Device, Count: f.Count})
		}
	}
	for _, e := range events {
		out.Events = append(out.Events, Stored{
			ID: e.ID, Source: e.Source, Device: e.Device, Kind: e.Kind,
			OccurredAt: e.OccurredAt, ReceivedAt: e.ReceivedAt,
			LateBy:  int64(e.ReceivedAt.Sub(e.OccurredAt).Seconds()),
			Payload: e.Payload,
		})
	}
	for _, k := range counted {
		out.Kinds = append(out.Kinds, KindCount{
			Kind: k.Kind, Count: k.Count, First: k.First, Last: k.Last})
	}

	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

// nonEmpty : The values given, blanks left out.
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
