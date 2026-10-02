package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
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

// Entered, Exited : The kinds a geofence reports when somebody crosses
// into or out of a place they drew themselves.
const (
	Entered = "place.entered"
	Exited  = "place.exited"
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
// written down, or empty if it is somewhere new.
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
	return nearest
}

// sortByTime : Oldest first.
func sortByTime(fixes []Fix) {
	for i := 1; i < len(fixes); i++ {
		for j := i; j > 0 && fixes[j].At.Before(fixes[j-1].At); j-- {
			fixes[j], fixes[j-1] = fixes[j-1], fixes[j]
		}
	}
}

// Lingering : An arrival with no departure after this long is a
// departure that was missed, not one that has not happened yet.
//
// A phone that went flat at the office does not mean somebody slept
// there, and the geofences drop departures often enough that the
// difference has to be said rather than guessed at.
const Lingering = 16 * time.Hour

// Fence : A named place the person drew, and a stretch of time they
// were inside it.
type Fence struct {
	Name     string
	From, To time.Time
}

// Holds : Whether a moment falls inside this stretch.
func (f Fence) Holds(at time.Time) bool {
	return !at.Before(f.From) && !at.After(f.To)
}

// Long : How long the stretch is. What decides which of two overlapping
// places is the more particular.
func (f Fence) Long() time.Duration { return f.To.Sub(f.From) }

// Fences : The stretches of time spent inside named places, from the
// crossings a phone reported.
//
// A crossing that was never closed runs to now rather than being
// dropped: somebody who is still at the office has not stopped being
// there because they have not left yet. A crossing out of somewhere
// nothing says they entered is ignored, which is the safe direction --
// the alternative is an unbounded stretch swallowing every stay before
// it.
//
// Arriving somewhere else ends wherever they were. Android drops
// geofence departures, and without this a missed one leaves somebody
// at the office from last night until the end of time: the real
// reading said "office, from 19:14 and still there" at eleven the next
// morning, with two crossings into home in between. They cannot be in
// both, and the arrival that was reported is better evidence than the
// departure that was not.
func Fences(crossings []Event, now time.Time) []Fence {
	in := append([]Event(nil), crossings...)
	sort.SliceStable(in, func(i, j int) bool { return in[i].OccurredAt.Before(in[j].OccurredAt) })

	open := map[string]time.Time{}
	var out []Fence

	shut := func(name string, at time.Time) {
		from, waiting := open[name]
		if !waiting {
			return
		}
		delete(open, name)
		out = append(out, Fence{Name: name, From: from, To: at})
	}

	for _, e := range in {
		name := placeName(e)
		if name == "" {
			continue
		}
		switch e.Kind {
		case Entered:
			if _, already := open[name]; already {
				continue
			}
			for other := range open {
				shut(other, e.OccurredAt)
			}
			open[name] = e.OccurredAt
		case Exited:
			shut(name, e.OccurredAt)
		}
	}

	// Whatever is left is somewhere they have not reported leaving.
	// At most one, now that arriving anywhere closes the rest.
	for name, from := range open {
		out = append(out, Fence{Name: name, From: from, To: now})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].From.Before(out[j].From) })
	return out
}

// In : The named place a stay happened inside, or empty.
//
// Judged on the middle of the stay rather than either end, because the
// crossing and the first reading inside it are minutes apart and the
// middle is nowhere near either boundary.
//
// The shortest stretch wins where two overlap. A place drawn inside
// another is the more particular answer, and "the badminton court" is
// what somebody would say rather than "home".
func In(s Stay, fences []Fence) string {
	middle := s.From.Add(s.Long() / 2)
	best, found := "", time.Duration(0)
	for _, f := range fences {
		if !f.Holds(middle) {
			continue
		}
		if best == "" || f.Long() < found {
			best, found = f.Name, f.Long()
		}
	}
	return best
}

// placeName : What a crossing says the place is called.
func placeName(e Event) string {
	var into struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(e.Payload, &into); err != nil {
		return ""
	}
	return strings.TrimSpace(into.Value)
}

// Named : A stay, and what the place it sits in is called.
type Named struct {
	Stay
	// Called : The name the place was given -- a geofence the owner
	// drew, a place they have been named before, or whatever is there
	// according to whoever is asked. Coordinates when nothing knows.
	Called string
}

// Merged : Consecutive stays in the same place, joined into one.
//
// Stays are clustered by distance from where the cluster started, and
// Near is 150 metres. That is right for a desk and wrong for anywhere
// larger than itself: walk from one end of a shopping centre to the
// other and the cluster breaks, and what comes out is two visits and a
// departure for somewhere nobody left. Indoor readings drift fifty to a
// hundred metres on their own, which can break it sitting still.
//
// Geometry cannot fix this -- a radius wide enough for a mall merges
// the restaurant next door -- so the name does it instead. Both halves
// of the mall are named the same thing, and two stays with one name are
// one stay.
//
// Only consecutive ones, and only across a gap no longer than Adrift.
// Leaving somewhere and coming back in the evening is two visits and
// must stay two; a gap longer than Adrift means the readings stopped
// and where they went in between is not known. The short clusters of a
// journey are dropped before this for being under Settled, so driving
// home and back tomorrow leaves a gap far wider than Adrift and does
// not merge.
//
// What this does not fix: it needs both halves to come back with the
// same name. A reverse lookup at opposite ends of a large building can
// answer with two different ones -- the building at one end, a unit
// inside it at the other -- and then nothing here can tell they are the
// same place. A geofence drawn round it always can, which is the
// argument for drawing one.
func Merged(in []Named) []Named {
	if len(in) < 2 {
		return in
	}
	out := make([]Named, 0, len(in))
	out = append(out, in[0])
	for _, next := range in[1:] {
		last := &out[len(out)-1]
		if next.Called != last.Called || next.From.Sub(last.To) > Adrift {
			out = append(out, next)
			continue
		}
		*last = join(*last, next)
	}
	return out
}

// join : Two stays in one place, as one stay.
//
// The middle is weighted by how long each half lasted rather than taken
// halfway between them. An hour at a table and two minutes by the door
// is a visit to the table, and an unweighted midpoint would put it in
// the corridor -- which matters, because the next visit is matched to
// this one by how far apart their middles are.
func join(a, b Named) Named {
	wa, wb := a.Long().Seconds(), b.Long().Seconds()
	if total := wa + wb; total > 0 {
		a.Lat = (a.Lat*wa + b.Lat*wb) / total
		a.Lon = (a.Lon*wa + b.Lon*wb) / total
	}
	if b.To.After(a.To) {
		a.To = b.To
	}
	return a
}

// Looking : How far back settling reads for readings.
//
// Long enough to hold a whole stay, including an afternoon somewhere
// and a night at home, so a stay is never cut in half by the edge of
// the window and written down as two.
const Looking = 36 * time.Hour

// Keep : How long a raw reading is worth storing.
//
// Nothing reads location.fix except settling, and settling never looks
// further back than Looking. Everything older is weight: at one reading
// every five minutes it is 288 rows a day, about 80MB a year, growing
// for ever and read by nothing.
//
// Seven days rather than two, which would also be correct. The margin
// is for the days the server is off, for a phone delivering a backlog
// late, and for being able to look at yesterday's readings by hand when
// a stay comes out wrong. Dropping it to just over Looking would save
// 6MB and remove the only copy of the evidence.
//
// What this does not touch is place.stayed, place.entered and
// place.exited. Those are the history, they are small, and they are
// kept for ever.
const Keep = 7 * 24 * time.Hour

// Places : The most previously-named places a new stay is matched
// against.
const Places = 500

// Watching : What settling needs of a store.
type Watching interface {
	Recent(ctx context.Context, userID string, q Query) ([]Event, error)
	Record(ctx context.Context, userID string, events []*Event) (stored, seen []string, err error)
}

// Naming : Somewhere to ask what is at a position, for a stay that no
// geofence of theirs covers and that is nowhere they have been before.
type Naming interface {
	Name(ctx context.Context, lat, lon float64) (string, error)
}

// Settler : Works out where somebody stopped, and writes it down.
type Settler struct {
	// Store : Where the readings are and where the stays go. Required.
	Store Watching
	// Naming : What to ask about somewhere new. Optional: without it a
	// place nobody has named keeps its coordinates, which is what it
	// had before anything could name it.
	Naming Naming
	// Logger : Where a failed lookup goes. Nil is silent.
	Logger *slog.Logger
}

// Settle : Writes down the stays that have ended among somebody's
// recent readings.
//
// Worked out again from the readings every time rather than kept
// anywhere. The same readings produce the same stays and the same keys,
// so a stay already written is recognised as a resend and the work is
// wasted rather than wrong. That is the cheaper mistake: the
// alternative is a cursor to keep, and a cursor that is ever wrong
// loses a day of somebody's life quietly.
func (st Settler) Settle(ctx context.Context, userID string, now time.Time) ([]Named, error) {
	seen, err := st.Store.Recent(ctx, userID, Query{Kind: Fixed, Since: now.Add(-Looking)})
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
	known, err := st.Store.Recent(ctx, userID, Query{Kind: Stayed, Limit: Places})
	if err != nil {
		return nil, fmt.Errorf("event: reading the places they know: %w", err)
	}

	// And the places they drew themselves, which outrank everything
	// else. The owner's rule, 2 October 2026: "my geo fences are the
	// first priority". A name somebody chose for a place they marked is
	// not improved on by anything worked out afterwards.
	crossings, err := st.Store.Recent(ctx, userID, Query{
		Prefix: "place.", Since: now.Add(-2 * Looking)})
	if err != nil {
		return nil, fmt.Errorf("event: reading the places they drew: %w", err)
	}
	fences := Fences(crossings, now)

	// Named before they are merged, because the name is what says two
	// of them are one place. One lookup per cluster, so crossing a
	// building that breaks into three costs three -- which is the price
	// of not having to guess a radius that fits every place at once.
	named := make([]Named, 0, len(stays))
	for _, s := range stays {
		named = append(named, Named{Stay: s, Called: st.called(ctx, s, fences, known)})
	}
	named = Merged(named)

	out := make([]*Event, 0, len(named))
	for _, n := range named {
		e, err := New(userID, "server", "", Stayed, n.From, now,
			n.Payload(n.Called), n.Key())
		if err != nil {
			// Unreachable for a stay worked out here, and not worth
			// losing the others over if it ever is.
			continue
		}
		out = append(out, e)
	}
	if _, _, err := st.Store.Record(ctx, userID, out); err != nil {
		return nil, fmt.Errorf("event: writing down where they stayed: %w", err)
	}
	return named, nil
}

// called : What to write this stay down as.
//
// In the owner's order. A geofence they drew wins. Failing that, the
// name an earlier stay at the same spot was given, which keeps two
// evenings at one restaurant together and costs no lookup. Failing
// that, whatever the naming service makes of it. And failing all of
// those, the coordinates, which name nothing but group correctly and
// can be corrected later.
func (st Settler) called(ctx context.Context, s Stay, fences []Fence, known []Event) string {
	if name := In(s, fences); name != "" {
		return name
	}
	if name := Called(s, known); name != "" {
		return name
	}
	if st.Naming == nil {
		return s.Where()
	}

	name, err := st.Naming.Name(ctx, s.Lat, s.Lon)
	if err != nil {
		// Logged and stepped over. A stay with coordinates for a name
		// is the whole of what is lost, and it can be named later.
		if st.Logger != nil {
			st.Logger.WarnContext(ctx, "could not find out what is at a place",
				slog.String("where", s.Where()), slog.Any("error", err))
		}
		return s.Where()
	}
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return s.Where()
}
