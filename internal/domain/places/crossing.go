package places

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"personal-assistant/internal/base/world/event"
)

// Prefix : What a place's identifier begins with.
const Prefix = "plc_"

// NewID : A new identifier for a place, thirty characters long.
func NewID() string { return Prefix + ulid.Make().String() }

// Reader : Where crossings and stays are read from.
type Reader interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Events : Where crossings are read from and written to.
type Events interface {
	Reader
	Record(ctx context.Context, userID string, events []*event.Event) (stored, seen []string, err error)
}

// Standing : Where somebody is with respect to one place.
type Standing struct {
	// Inside : Whether the latest crossing was into it.
	Inside bool
	// Since : When that crossing was. Zero when the server has never
	// written one for the place.
	Since time.Time
}

// lookback : How many of the server's crossings are read for one
// place to find the latest of them.
const lookback = 20

// Where : Where somebody stands with respect to a place, by the latest
// crossing the server wrote under its name.
//
// Only the server's own. Tasker's crossings for the same name are from
// before the place was drawn here, or ignored on arrival; reading them
// would make the state depend on a phone's geofence the owner has
// stopped trusting.
func Where(ctx context.Context, events Reader, userID string, p Place) (Standing, error) {
	q := event.Query{
		Kinds: []string{event.Entered, event.Exited},
		From:  []string{Source},
		Limit: lookback,
	}
	// Narrowed by the name where the stored payload is sure to carry
	// it as written. A quotation mark or a backslash is escaped in the
	// JSON, and a search for the raw name would then find nothing --
	// which would read as never having entered, every fix.
	if !strings.ContainsAny(p.Name, `"\`) {
		q.Contains = p.Name
	} else {
		q.Limit = 10 * lookback
	}
	got, err := events.Recent(ctx, userID, q)
	if err != nil {
		return Standing{}, fmt.Errorf("places: reading the crossings at %q: %w", p.Name, err)
	}
	return Latest(got, p.Name), nil
}

// Latest : The standing the newest of the server's crossings under a
// name gives, among those given.
func Latest(crossings []event.Event, name string) Standing {
	var out Standing
	for _, e := range crossings {
		if e.Source != Source || (e.Kind != event.Entered && e.Kind != event.Exited) {
			continue
		}
		if !Same(event.Value(e.Payload), name) {
			continue
		}
		if !out.Since.IsZero() && !e.OccurredAt.After(out.Since) {
			continue
		}
		out = Standing{Inside: e.Kind == event.Entered, Since: e.OccurredAt}
	}
	return out
}

// Crossed : The crossings a run of fixes makes, given where somebody
// stood at each place before them.
//
// Each place on its own: two that overlap can both be entered, and
// leaving one says nothing about the other. Fixes are taken oldest
// first, and one no later than the crossing a place already has is
// passed over for that place -- a spooled reading from before the
// last crossing cannot rewrite what came after it.
//
// Inside the radius enters; out past it by Margin leaves; anywhere
// between changes nothing, which is what keeps a phone at the gate
// from coming and going.
//
// Leaving takes two fixes in a row out past it, and is timed at the
// first. One alone is the phone's cached network reading as often as it
// is a step out of the door: on 16 October the same point 235 m east of
// home came back twice in the night, between fixes at home, and each
// wrote Home left and entered again (owner: *"I literally was in home
// .. and in home till now .. but it showed moving"*). Someone who has
// really left sends another fix within a minute or two, as the phone
// reports every 20 to 100 s once it moves. `before` are fixes already
// taken, newest last, that may be the first of the two; they cross
// nothing themselves.
func Crossed(userID string, kept []Place, standing map[string]Standing,
	fixes []*event.Event, received time.Time, before ...*event.Event) []*event.Event {
	type reading struct {
		e        *event.Event
		lat, lng float64
		earlier  bool
	}
	var readings []reading
	add := func(es []*event.Event, earlier bool) {
		for _, e := range es {
			if e == nil {
				continue
			}
			f, ok := event.ReadFix(*e)
			if !ok {
				continue
			}
			readings = append(readings, reading{e: e, lat: f.Lat, lng: f.Lon, earlier: earlier})
		}
	}
	add(before, true)
	add(fixes, false)
	sort.SliceStable(readings, func(i, j int) bool {
		return readings[i].e.OccurredAt.Before(readings[j].e.OccurredAt)
	})

	var out []*event.Event
	for _, p := range kept {
		now := standing[p.ID]
		// out : The first of the fixes in a row out past the edge, while inside.
		var outFirst *reading
		for i := range readings {
			r := &readings[i]
			if !now.Since.IsZero() && !r.e.OccurredAt.After(now.Since) {
				continue
			}
			var kind string
			at := r.e
			switch {
			case !now.Inside:
				outFirst = nil
				if r.earlier || !p.Inside(r.lat, r.lng) {
					continue
				}
				kind = event.Entered
			case p.Outside(r.lat, r.lng):
				if outFirst == nil {
					outFirst = r
					continue
				}
				if r.earlier {
					continue
				}
				kind, at = event.Exited, outFirst.e
				outFirst = nil
			default:
				outFirst = nil
				continue
			}
			e, err := event.New(userID, Source, r.e.Device, kind, at.OccurredAt,
				later(received, at.OccurredAt), named(p.Name), key(kind, p.ID, at.OccurredAt))
			if err != nil {
				// Unreachable for a fix that was itself stored, and not
				// worth the other places' crossings if it ever is.
				continue
			}
			out = append(out, e)
			now = Standing{Inside: kind == event.Entered, Since: at.OccurredAt}
		}
	}
	return out
}

// Before : How many fixes before a batch are read, for the first of two in a row out past a place's edge.
const Before = 2

// Cross : Works out and writes the crossings a batch of new fixes
// makes into and out of the places somebody keeps, and returns those
// that were written.
//
// The dedupe key is the place, the kind and the fix's moment, so the
// same fix arriving again under a key of its own still cannot write a
// second crossing.
func Cross(ctx context.Context, events Events, userID string, kept []Place,
	fixes []*event.Event, received time.Time) ([]*event.Event, error) {
	if len(kept) == 0 || len(fixes) == 0 {
		return nil, nil
	}
	standing := make(map[string]Standing, len(kept))
	for _, p := range kept {
		s, err := Where(ctx, events, userID, p)
		if err != nil {
			return nil, err
		}
		standing[p.ID] = s
	}
	crossed := Crossed(userID, kept, standing, fixes, received, earlier(ctx, events, userID, fixes)...)
	if len(crossed) == 0 {
		return nil, nil
	}
	stored, _, err := events.Record(ctx, userID, crossed)
	if err != nil {
		return nil, fmt.Errorf("places: writing the crossings: %w", err)
	}
	written := make(map[string]bool, len(stored))
	for _, k := range stored {
		written[k] = true
	}
	out := make([]*event.Event, 0, len(stored))
	for _, e := range crossed {
		if written[e.DedupeKey] {
			out = append(out, e)
		}
	}
	return out, nil
}

// later : The later of two moments. A crossing cannot have been
// received before the fix that made it happened.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// key : The dedupe key of a crossing.
func key(kind, placeID string, at time.Time) string {
	return kind + ":" + placeID + ":" + strconv.FormatInt(at.UTC().Unix(), 10)
}

// named : The payload of a crossing: the place's name as its value,
// exactly as Tasker sent it.
//
// Written without HTML escaping, so "Mum & Dad's" is stored as those
// characters and a search for the name finds it.
func named(name string) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]string{"value": name}); err != nil {
		// Unreachable: a string always encodes.
		return json.RawMessage(`{"value":""}`)
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes()))
}

// earlier : The fixes just before a batch's first, newest last; none when
// they cannot be read, which only makes leaving wait for one more fix.
func earlier(ctx context.Context, events Events, userID string, fixes []*event.Event) []*event.Event {
	var first time.Time
	for _, e := range fixes {
		if e != nil && e.Kind == event.Fixed && (first.IsZero() || e.OccurredAt.Before(first)) {
			first = e.OccurredAt
		}
	}
	if first.IsZero() {
		return nil
	}
	got, err := events.Recent(ctx, userID, event.Query{Kind: event.Fixed, Until: first.Add(-time.Millisecond), Limit: Before})
	if err != nil {
		return nil
	}
	out := make([]*event.Event, 0, len(got))
	for i := len(got) - 1; i >= 0; i-- {
		e := got[i]
		out = append(out, &e)
	}
	return out
}
