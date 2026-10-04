package place

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Spot : A place on the map, found by name or by looking around.
type Spot struct {
	// Name : What it is called. Empty for an address with nothing on
	// it, which a street corner is.
	Name string
	// Address : The rest of it -- road, area, town, postcode.
	Address string
	// Lat, Lon : Where it is.
	Lat, Lon float64
	// Metres : How far from where the question was asked. Zero when
	// nothing was asked from anywhere in particular.
	Metres float64
}

// Journey : Getting from one place to another.
type Journey struct {
	// Metres, Takes : How far and how long, by road and in traffic.
	Metres float64
	Takes  time.Duration
	// Mode : How it was worked out -- driving, walking.
	Mode string
}

// Driving, Walking : The ways of getting somewhere this asks about.
//
// Two, because they are the two somebody has a choice about. A
// motorbike routes as a car and a bus cannot be routed at all
// without a timetable nobody has.
const (
	Driving = "drive"
	Walking = "walk"
)

// Traffic : Whether the time allows for traffic.
//
// It must. Measured on one journey across Chennai: nineteen minutes
// without it, fifty-three with. A travel time three times short is
// not a worse answer than none, it is a worse answer than none --
// somebody acts on it and misses the thing.
const Traffic = "approximated"

// Find : Where a named place is.
//
// Biased to the country rather than to the person's position, on
// purpose: "the airport" means the one they fly from, which may be
// thirty kilometres away, while a proximity bias would offer an
// airstrip in the next street.
func (g Geoapify) Find(ctx context.Context, text, country string, most int) ([]Spot, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("place: nothing to look for")
	}
	if most <= 0 {
		most = 5
	}

	args := url.Values{
		"text":  {text},
		"limit": {strconv.Itoa(most)},
	}
	if c := strings.TrimSpace(country); c != "" {
		args.Set("filter", "countrycode:"+strings.ToLower(c))
	}

	var got found
	if err := g.ask(ctx, g.search(), args, &got); err != nil {
		return nil, err
	}

	out := make([]Spot, 0, len(got.Features))
	for _, f := range got.Features {
		p := f.Properties
		if p.Lat == 0 && p.Lon == 0 {
			continue
		}
		out = append(out, Spot{
			Name: strings.TrimSpace(p.Name), Address: rest(p.Formatted, p.Name),
			Lat: p.Lat, Lon: p.Lon,
		})
	}
	return out, nil
}

// Near : What is around a position.
//
// What, when given, is matched against the categories this knows.
// An unknown one is refused rather than passed through: Geoapify
// answers a bad category with an empty list and no error, which is
// indistinguishable from there being nothing there, and a question
// about restaurants would come back "nothing near you".
func (g Geoapify) Near(ctx context.Context, lat, lon float64, what string, metres float64, most int) ([]Spot, error) {
	if metres <= 0 {
		metres = 1000
	}
	if most <= 0 {
		most = 8
	}

	categories := Categories
	if asked := strings.TrimSpace(strings.ToLower(what)); asked != "" {
		known, ok := Kinds[asked]
		if !ok {
			return nil, fmt.Errorf("place: nothing here knows what %q is; "+
				"ask without saying what sort, or use one of: %s", what, Sorts())
		}
		categories = known
	}

	var got found
	if err := g.ask(ctx, strings.TrimSpace(g.Places), url.Values{
		"categories": {categories},
		"filter":     {fmt.Sprintf("circle:%s,%s,%d", six(lon), six(lat), int(metres))},
		"bias":       {"proximity:" + six(lon) + "," + six(lat)},
		"limit":      {strconv.Itoa(most)},
	}, &got); err != nil {
		return nil, err
	}

	out := make([]Spot, 0, len(got.Features))
	for _, f := range got.Features {
		p := f.Properties
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		out = append(out, Spot{
			Name: strings.TrimSpace(p.Name), Address: rest(p.Formatted, p.Name),
			Lat: p.Lat, Lon: p.Lon, Metres: p.Distance,
		})
	}
	return out, nil
}

// Route : How far and how long from one place to another.
func (g Geoapify) Route(ctx context.Context, fromLat, fromLon, toLat, toLon float64, mode string) (Journey, error) {
	if mode != Walking {
		mode = Driving
	}

	var got struct {
		Features []struct {
			Properties struct {
				Distance float64 `json:"distance"`
				Time     float64 `json:"time"`
			} `json:"properties"`
		} `json:"features"`
	}
	args := url.Values{
		"waypoints": {fmt.Sprintf("%s,%s|%s,%s", six(fromLat), six(fromLon), six(toLat), six(toLon))},
		"mode":      {mode},
	}
	if mode == Driving {
		args.Set("traffic", Traffic)
	}
	if err := g.ask(ctx, g.routing(), args, &got); err != nil {
		return Journey{}, err
	}
	if len(got.Features) == 0 {
		// No road between them, which an island and a wrong
		// coordinate both produce.
		return Journey{}, nil
	}

	p := got.Features[0].Properties
	return Journey{
		Metres: p.Distance,
		Takes:  time.Duration(p.Time * float64(time.Second)),
		Mode:   mode,
	}, nil
}

// Kinds : The sorts of place somebody asks for by name, and the
// categories each means.
//
// A list rather than passing the words through, because Geoapify
// answers an unknown category with an empty list and no error. A
// typed-through "coffee" would come back as "nothing near you",
// which is a lie that looks like an answer.
//
// None of accommodation, sport or tourism appears: each poisons the
// whole call, silently, even paired with one that works.
var Kinds = map[string]string{
	"food":      "catering.restaurant,catering.fast_food",
	"coffee":    "catering.cafe",
	"shop":      "commercial",
	"grocery":   "commercial.supermarket,commercial.convenience",
	"pharmacy":  "healthcare.pharmacy",
	"hospital":  "healthcare.hospital,healthcare.clinic",
	"fuel":      "service.vehicle.fuel",
	"atm":       "service.financial.atm",
	"bank":      "service.financial.bank",
	"transport": "public_transport",
	"cinema":    "entertainment.cinema",
}

// Sorts : The sorts of place that can be asked for, for a refusal
// that tells somebody what they can say instead.
func Sorts() string {
	out := make([]string, 0, len(Kinds))
	for k := range Kinds {
		out = append(out, k)
	}
	sortStrings(out)
	return strings.Join(out, ", ")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// search, routing : The endpoints, worked out from the reverse
// geocoding one so that a server pointed at a test does not have to
// be told three separate addresses.
func (g Geoapify) search() string {
	return swap(g.URL, "/geocode/reverse", "/geocode/search")
}

func (g Geoapify) routing() string {
	return swap(g.URL, "/v1/geocode/reverse", "/v1/routing")
}

func swap(base, from, to string) string {
	base = strings.TrimSpace(base)
	if strings.Contains(base, from) {
		return strings.Replace(base, from, to, 1)
	}
	return base
}
