// Package place turns a position into the name of somewhere, for the
// places a person never drew a geofence around.
//
// Their own geofences are tried first and win, which is the owner's
// rule: a name somebody chose for a place they marked is not improved
// on by anything worked out afterwards. This names what is left -- the
// mall, the restaurant, the friend's house -- and only for a stay that
// has already ended, so it is a handful of calls a day rather than one
// per reading.
//
// Everything here fails quietly. A name is an improvement on a pair of
// coordinates and never a thing the rest of the server waits for: with
// no key, no network, or no answer, a stay keeps the coordinates it
// already had and nothing else behaves differently.
package place

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Named : What is at a position, and how sure that is.
type Named struct {
	// Name : What to call it. Empty when nothing could be found.
	Name string
	// Address : The rest of the address -- the road, the area, the
	// town, the postcode -- with the name taken off the front, since
	// Name already carries it.
	//
	// Kept because a name on its own can be wrong in a way nobody
	// can see. "Flavors non veg restaurant" told the owner nothing
	// about having been at a cinema; "Grand Southern Trunk Road,
	// Urapakkam, Vandalur - 603210" would at least have been the
	// right stretch of road.
	Address string
	// Metres : How far the named thing was from where they actually
	// stopped. Zero when the name came from the address rather than
	// from a business.
	//
	// The honesty of the whole answer is in this number. A shop you
	// are inside is twenty metres away; one across a trunk road is a
	// hundred and twenty-seven, and that is how a cinema came to be
	// recorded as a restaurant.
	Metres float64
}

// Namer : Somewhere to ask what is at a position.
type Namer interface {
	Name(ctx context.Context, lat, lon float64) (Named, error)
}

// Categories : The kinds of place worth being at.
//
// Somewhere a person spends ten minutes is a shop, a restaurant, a
// clinic, an office, a court. Left unfiltered the search also returns
// post boxes, benches and bus stops, and the nearest of those is not
// where somebody was.
//
// "building" is deliberately absent, being every structure there is.
// A real venue carries a category of its own as well: the mall that
// prompted this came back under commercial with "building" only as
// its first label.
//
// "accommodation", "sport" and "tourism" are absent for a worse
// reason: each one poisons the whole call. "commercial" alone finds
// twenty places at a shopping mall; "commercial,accommodation" finds
// none, and so do the other two. Not an error -- an empty answer,
// which is indistinguishable from there being nothing there.
//
// Found on 2 October 2026 by pairing each category with one that
// works. Worth knowing before adding to this list: a category that
// looks reasonable can silently name nothing, for ever, and the
// feature goes on looking as though it is running.
const Categories = "commercial,catering,entertainment,leisure," +
	"healthcare,education,service,office"

// Reach : How close a place has to be to be the place they were, in
// metres.
//
// The same distance that counts as one place when the readings are
// clustered, so the two agree. A shop six hundred metres away is not
// where somebody sat, however much it is the nearest named thing.
const Reach = 150.0

// Most : Places asked for at once. Twenty or fewer is one credit, and
// only the nearest is ever used.
const Most = 20

// Geoapify : Asks Geoapify.
type Geoapify struct {
	// Key : The owner's API key.
	Key string
	// Places : The point-of-interest endpoint, which answers with the
	// name of a business. Asked first: "Phoenix Marketcity" is where
	// somebody went and "Velachery Main Road" is how they got there.
	Places string
	// URL : The reverse geocoding endpoint, which answers with a
	// street. Asked when nothing is named nearby, which is most of a
	// residential neighbourhood.
	URL string
	// Client : How the call is made. Nil uses a client of its own with
	// the timeout below.
	Client *http.Client
	// Timeout : How long one lookup may take.
	Timeout time.Duration
}

// found : The parts of Geoapify's answer this uses.
//
// A handful of fields out of about forty. Taking only what is wanted
// means an answer that grows a field cannot change what this does, and
// means the rest of somebody's address is never carried around by
// accident.
type found struct {
	Features []struct {
		Properties struct {
			Name          string  `json:"name"`
			Street        string  `json:"street"`
			Suburb        string  `json:"suburb"`
			District      string  `json:"district"`
			City          string  `json:"city"`
			Neighbourhood string  `json:"neighbourhood"`
			Formatted     string  `json:"formatted"`
			Postcode      string  `json:"postcode"`
			Distance      float64 `json:"distance"`
			Lat           float64 `json:"lat"`
			Lon           float64 `json:"lon"`
		} `json:"properties"`
	} `json:"features"`
}

// Name : What is at a position, in as few words as will identify it.
//
// The owner's order, 2 October 2026: "for knowing the location from
// coords we can use reverse geocoding for street name, place api for
// place name." A business is what somebody would say they went to; a
// street is what is there when no business is.
//
// One credit when a place is found, two when the street has to be
// asked for instead. Only ever for a stay that has ended and that no
// geofence of theirs already names, so a handful a day against a free
// allowance of three thousand.
func (g Geoapify) Name(ctx context.Context, lat, lon float64) (Named, error) {
	if strings.TrimSpace(g.Key) == "" {
		return Named{}, nil
	}

	// A named business first. A failure here is not the end of it: the
	// street is still worth having, and asking for it is another
	// credit rather than another problem.
	near, err := g.nearby(ctx, lat, lon)

	// The address either way. It costs the credit that was going to
	// be spent on the street anyway when no business was found, and
	// when one was it is what says which road that business is on --
	// which is the only thing that would have shown the name was
	// wrong.
	where, whereErr := g.street(ctx, lat, lon)
	if near.Name != "" {
		near.Address = where.Address
		return near, nil
	}
	if whereErr != nil {
		if err != nil {
			return Named{}, err
		}
		return Named{}, whereErr
	}
	return where, nil
}

// nearby : The nearest named place worth being at, or empty.
func (g Geoapify) nearby(ctx context.Context, lat, lon float64) (Named, error) {
	endpoint := strings.TrimSpace(g.Places)
	if endpoint == "" {
		return Named{}, nil
	}

	var got struct {
		Features []struct {
			Properties struct {
				Name     string  `json:"name"`
				Distance float64 `json:"distance"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := g.ask(ctx, endpoint, url.Values{
		"categories": {Categories},
		"filter":     {fmt.Sprintf("circle:%s,%s,%d", six(lon), six(lat), int(Reach))},
		"bias":       {"proximity:" + six(lon) + "," + six(lat)},
		"limit":      {strconv.Itoa(Most)},
	}, &got); err != nil {
		return Named{}, err
	}

	// The nearest named one. They come back nearest first with the
	// proximity bias, but that is their ordering and not a promise.
	nearest := Named{}
	best := Reach
	for _, f := range got.Features {
		name := strings.TrimSpace(f.Properties.Name)
		if name == "" || f.Properties.Distance > best {
			continue
		}
		nearest, best = Named{Name: name, Metres: f.Properties.Distance}, f.Properties.Distance
	}
	return nearest, nil
}

// street : The street, suburb or city at a position, or empty.
//
// The most particular thing that came back, falling to the least. A
// street is worth having; a city is not, on its own, a place you went
// -- but it is better than a pair of coordinates, and the person can
// correct it later.
func (g Geoapify) street(ctx context.Context, lat, lon float64) (Named, error) {
	endpoint := strings.TrimSpace(g.URL)
	if endpoint == "" {
		return Named{}, fmt.Errorf("place: nowhere to ask")
	}

	var got found
	if err := g.ask(ctx, endpoint, url.Values{
		"lat":    {six(lat)},
		"lon":    {six(lon)},
		"format": {"geojson"},
	}, &got); err != nil {
		return Named{}, err
	}
	if len(got.Features) == 0 {
		// Nothing there, which the sea and a motorway both produce.
		// Not a failure: there is genuinely no name for it.
		return Named{}, nil
	}

	p := got.Features[0].Properties
	out := Named{Address: rest(p.Formatted, p.Name), Metres: p.Distance}
	for _, candidate := range []string{p.Name, p.Street, p.Neighbourhood, p.Suburb, p.District, p.City} {
		if name := strings.TrimSpace(candidate); name != "" {
			out.Name = name
			break
		}
	}
	return out, nil
}

// rest : The address with its leading name taken off.
//
// Geoapify puts the name at the front of what it formats, and the
// name is already carried on its own. "Flavors non veg restaurant,
// Grand Southern Trunk Road, Urapakkam" becomes the road onwards,
// which is the part that says where rather than what.
func rest(formatted, name string) string {
	formatted = strings.TrimSpace(formatted)
	name = strings.TrimSpace(name)
	if name != "" && strings.HasPrefix(formatted, name+",") {
		return strings.TrimSpace(formatted[len(name)+1:])
	}
	return formatted
}

// ask : One call, read into into.
//
// The key goes in the query because that is the only way Geoapify
// takes it. It is the owner's own key for their own account, and the
// position is theirs: these are the only requests in the server where
// both are unavoidable.
func (g Geoapify) ask(ctx context.Context, endpoint string, args url.Values, into any) error {
	asking, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("place: %w", err)
	}
	args.Set("apiKey", g.Key)
	asking.RawQuery = args.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asking.String(), nil)
	if err != nil {
		return fmt.Errorf("place: %w", err)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		return fmt.Errorf("place: asking what is there: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("place: it answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("place: reading the answer: %w", err)
	}
	return nil
}

// six : A coordinate, to about a tenth of a metre -- well under the
// twenty the phone is out by.
func six(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }

// client : The one to call with.
func (g Geoapify) client() *http.Client {
	if g.Client != nil {
		return g.Client
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &http.Client{Timeout: timeout}
}
