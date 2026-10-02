package event

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Fixed, Stayed : The kinds this works in. A phone reports the first;
// the server works out the second and writes it back as an event of its
// own, so it shows on the events screen and counts towards the
// description of them like anything else they did.
const (
	Fixed  = "location.fix"
	Stayed = "place.stayed"
)

// Near : How far apart two readings can be and still be one place, in
// metres.
//
// Measured rather than chosen: three fixes taken while sitting
// perfectly still, on 2 October 2026, were spread over about twenty
// metres. Network location is roughly that accurate and the scatter is
// the floor on what any radius has to absorb. A hundred and fifty
// swallows it without putting a café and the shop next door in the same
// place.
const Near = 150.0

// Settled : How long in one place before being there counts as having
// gone somewhere.
//
// The owner's number: "if im there in a location for more than 10 mins
// it gets recorded". It is also why the phone reports every five
// minutes -- at a ten-minute interval a ten-minute stop falls between
// two readings and leaves nothing behind.
const Settled = 10 * time.Minute

// Adrift : With no reading for longer than this, a stay is closed at
// its last one rather than run on.
//
// A phone that went flat at the office did not mean somebody slept
// there, and the same mistake made from fixes rather than geofences is
// the same mistake.
const Adrift = 30 * time.Minute

// perDegree : Metres in a degree of latitude.
//
// Flat-earth arithmetic, deliberately. Over the hundred and fifty
// metres this has to judge, the error against a proper great-circle
// distance is centimetres, and the phone's own reading is twenty metres
// out.
const perDegree = 111320.0

// Fix : One reading of where somebody was.
type Fix struct {
	At       time.Time
	Lat, Lon float64
}

// Stay : Somewhere somebody was, for long enough to count.
type Stay struct {
	// Lat, Lon : The middle of the readings, rather than the first of
	// them, so a stay is not anchored to wherever the scatter happened
	// to start.
	Lat, Lon float64
	// From, To : The first and last reading inside it. Not the reading
	// that ended it: that one was taken somewhere else, and counting it
	// would stretch every stay out to the next.
	From, To time.Time
}

// Long : How long they were there.
func (s Stay) Long() time.Duration { return s.To.Sub(s.From) }

// Stays : The stays among a run of readings, oldest first.
//
// Only the ones that have ended. A run of readings still arriving from
// the same place is somebody who is still there, and its length is not
// known yet -- writing it down now would record a ten-minute visit to a
// restaurant they are still sitting in.
func Stays(fixes []Fix) []Stay {
	in := append([]Fix(nil), fixes...)
	sortByTime(in)

	var out []Stay
	var cluster []Fix

	close := func(ended bool) {
		if ended && len(cluster) > 0 {
			if s, ok := settled(cluster); ok {
				out = append(out, s)
			}
		}
		cluster = nil
	}

	for _, f := range in {
		if len(cluster) == 0 {
			cluster = []Fix{f}
			continue
		}
		// Measured from the first of the cluster rather than its
		// middle. Against the middle, a slow walk drags the centre
		// along with it and a mile of pavement reads as one place.
		gap := f.At.Sub(cluster[len(cluster)-1].At)
		if gap > Adrift {
			// Nothing was heard for long enough that what happened in
			// between is unknown. The cluster ended at its last
			// reading, whatever this one says.
			close(true)
			cluster = []Fix{f}
			continue
		}
		if apart(cluster[0], f) > Near {
			close(true)
			cluster = []Fix{f}
			continue
		}
		cluster = append(cluster, f)
	}
	// Whatever is still open is somebody who has not left yet.
	return out
}

// settled : A cluster as a stay, if it lasted long enough to be one.
func settled(cluster []Fix) (Stay, bool) {
	from, to := cluster[0].At, cluster[len(cluster)-1].At
	if to.Sub(from) < Settled {
		return Stay{}, false
	}
	var lat, lon float64
	for _, f := range cluster {
		lat, lon = lat+f.Lat, lon+f.Lon
	}
	n := float64(len(cluster))
	return Stay{Lat: lat / n, Lon: lon / n, From: from, To: to}, true
}

// apart : How far two readings are, in metres.
func apart(a, b Fix) float64 {
	dy := (b.Lat - a.Lat) * perDegree
	dx := (b.Lon - a.Lon) * perDegree * math.Cos(a.Lat*math.Pi/180)
	return math.Sqrt(dx*dx + dy*dy)
}

// Apart : How far a stay is from a reading, in metres. What decides
// whether two stays are the same place.
func (s Stay) Apart(lat, lon float64) float64 {
	return apart(Fix{Lat: s.Lat, Lon: s.Lon}, Fix{Lat: lat, Lon: lon})
}

// Where : The stay's position, written the way a payload carries it.
func (s Stay) Where() string { return Coordinates(s.Lat, s.Lon) }

// Coordinates : A position, as the phone writes it and as a payload
// carries it back.
func Coordinates(lat, lon float64) string {
	return strconv.FormatFloat(lat, 'f', 5, 64) + "," + strconv.FormatFloat(lon, 'f', 5, 64)
}

// Key : What makes writing this stay twice harmless.
//
// The moment it started, which does not move: the same stay worked out
// again from the same readings produces the same key, and the row
// already there is recognised as a resend rather than written again.
func (s Stay) Key() string {
	return Stayed + ":" + strconv.FormatInt(s.From.UTC().Unix(), 10)
}

// Payload : What the stay carries, as the event stores it.
//
// [called] is what this place is known as -- the label an earlier stay
// nearby was written under, or its coordinates when it is somewhere new.
// It goes under "value" because that is the field everything else reads,
// and because what the description of somebody should group on is the
// place rather than the particular readings.
func (s Stay) Payload(called string) json.RawMessage {
	body, err := json.Marshal(struct {
		Value   string `json:"value"`
		Minutes int    `json:"minutes"`
		At      string `json:"at"`
	}{Value: called, Minutes: int(s.Long().Round(time.Minute).Minutes()), At: s.Where()})
	if err != nil {
		// Unreachable: three strings and an integer.
		return json.RawMessage(`{"value":""}`)
	}
	return body
}

// ReadFix : A reading out of a stored event, if that is what it is.
//
// The payload a phone sends is {"value": "12.91100,80.06242"} and
// nothing here assumes more than that. A row that will not parse is not
// an error worth stopping for: it is one reading of several hundred,
// and a stay is none the worse for missing it.
func ReadFix(e Event) (Fix, bool) {
	if e.Kind != Fixed {
		return Fix{}, false
	}
	var into struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(e.Payload, &into); err != nil {
		return Fix{}, false
	}
	lat, lon, ok := LatLon(into.Value)
	if !ok {
		return Fix{}, false
	}
	return Fix{At: e.OccurredAt, Lat: lat, Lon: lon}, true
}

// LatLon : A "lat,lon" pair, if that is what it is.
func LatLon(s string) (lat, lon float64, ok bool) {
	before, after, found := strings.Cut(strings.TrimSpace(s), ",")
	if !found {
		return 0, 0, false
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
	if err != nil || lat < -90 || lat > 90 {
		return 0, 0, false
	}
	lon, err = strconv.ParseFloat(strings.TrimSpace(after), 64)
	if err != nil || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	return lat, lon, true
}

// Called : What a place is already known as, among the stays already
// written down.
//
// A place names itself the first time somebody stays there, and every
// later stay within reach of it borrows that name. Without this, two
// evenings at the same restaurant are two different places -- the
// readings are twenty metres apart and the coordinates never repeat --
// and nothing counting where somebody goes would ever count to two.
//
// The radius is wider than Near, because two stays at one place are
// each already a scatter of readings and their middles can sit a Near
// apart without either being anywhere else.
func Called(s Stay, known []Event) string {
	nearest, best := "", 2*Near
	for _, e := range known {
		if e.Kind != Stayed {
			continue
		}
		var into struct {
			Value string `json:"value"`
			At    string `json:"at"`
		}
		if err := json.Unmarshal(e.Payload, &into); err != nil || into.Value == "" {
			continue
		}
		lat, lon, ok := LatLon(into.At)
		if !ok {
			// A place already given a proper name keeps its
			// coordinates in "at"; one without them cannot be
			// measured against and is left alone.
			continue
		}
		if d := s.Apart(lat, lon); d < best {
			nearest, best = into.Value, d
		}
	}
	if nearest != "" {
		return nearest
	}
	return s.Where()
}

// sortByTime : Oldest first.
func sortByTime(fixes []Fix) {
	for i := 1; i < len(fixes); i++ {
		for j := i; j > 0 && fixes[j].At.Before(fixes[j-1].At); j-- {
			fixes[j], fixes[j-1] = fixes[j-1], fixes[j]
		}
	}
}

// Looking : How far back settling reads for readings.
//
// Long enough to hold a whole stay, including an afternoon somewhere
// and a night at home, so a stay is never cut in half by the edge of
// the window and written down as two.
const Looking = 36 * time.Hour

// Places : The most previously-named places a new stay is matched
// against.
const Places = 500

// Watching : What settling needs of a store.
type Watching interface {
	Recent(ctx context.Context, userID string, q Query) ([]Event, error)
	Record(ctx context.Context, userID string, events []*Event) (stored, seen []string, err error)
}

// Settle : Writes down the stays that have ended among somebody's
// recent readings.
//
// Worked out again from the readings every time rather than kept
// anywhere. The same readings produce the same stays and the same keys,
// so a stay already written is recognised as a resend and the work is
// wasted rather than wrong. That is the cheaper mistake: the alternative
// is a cursor to keep, and a cursor that is ever wrong loses a day of
// somebody's life quietly.
func Settle(ctx context.Context, store Watching, userID string, now time.Time) ([]Stay, error) {
	seen, err := store.Recent(ctx, userID, Query{Kind: Fixed, Since: now.Add(-Looking)})
	if err != nil {
		return nil, fmt.Errorf("event: reading where they have been: %w", err)
	}

	fixes := make([]Fix, 0, len(seen))
	for _, e := range seen {
		if f, ok := ReadFix(e); ok {
			fixes = append(fixes, f)
		}
	}
	stays := Stays(fixes)
	if len(stays) == 0 {
		return nil, nil
	}

	// Every place already named, so a second evening at the same
	// restaurant is written under the name the first one gave it.
	known, err := store.Recent(ctx, userID, Query{Kind: Stayed, Limit: Places})
	if err != nil {
		return nil, fmt.Errorf("event: reading the places they know: %w", err)
	}

	out := make([]*Event, 0, len(stays))
	for _, s := range stays {
		e, err := New(userID, "server", "", Stayed, s.From, now,
			s.Payload(Called(s, known)), s.Key())
		if err != nil {
			// Unreachable for a stay worked out here, and not worth
			// losing the others over if it ever is.
			continue
		}
		out = append(out, e)
	}
	if _, _, err := store.Record(ctx, userID, out); err != nil {
		return nil, fmt.Errorf("event: writing down where they stayed: %w", err)
	}
	return stays, nil
}
