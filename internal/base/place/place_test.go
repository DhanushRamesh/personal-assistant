package place_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/base/place"
)

// answering : A stand-in for Geoapify that returns a fixed body.
func answering(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &asked
}

// The building's own name is what somebody would say.
func TestTheMostParticularNameWins(t *testing.T) {
	s, _ := answering(t, 200, `{"features":[{"properties":{
		"name":"Phoenix Marketcity","street":"Velachery Main Road","city":"Chennai"}}]}`)

	got, err := place.Geoapify{Key: "k", URL: s.URL}.Name(context.Background(), 12.9, 80.06)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Phoenix Marketcity" {
		t.Errorf("called it %q, want the building's name", got.Name)
	}
}

// Without a building, a street is still worth having.
func TestItFallsBackThroughWhatItHas(t *testing.T) {
	s, _ := answering(t, 200, `{"features":[{"properties":{"street":"Velachery Main Road","city":"Chennai"}}]}`)

	got, _ := place.Geoapify{Key: "k", URL: s.URL}.Name(context.Background(), 12.9, 80.06)
	if got.Name != "Velachery Main Road" {
		t.Errorf("called it %q, want the street", got.Name)
	}
}

// The sea and a motorway both produce nothing, and that is not a
// failure: there is genuinely no name for it.
func TestNothingThereIsNotAFailure(t *testing.T) {
	s, _ := answering(t, 200, `{"features":[]}`)

	got, err := place.Geoapify{Key: "k", URL: s.URL}.Name(context.Background(), 0, 0)
	if err != nil || got.Name != "" {
		t.Errorf("got.Name %q, %v; want no name and no error", got.Name, err)
	}
}

// No key is not an error either. It is how this behaved before it
// existed, and a stay keeps its coordinates.
func TestNoKeyAsksNobody(t *testing.T) {
	s, asked := answering(t, 200, `{"features":[{"properties":{"name":"somewhere"}}]}`)

	got, err := place.Geoapify{URL: s.URL}.Name(context.Background(), 12.9, 80.06)
	if err != nil || got.Name != "" {
		t.Errorf("got.Name %q, %v; want nothing", got.Name, err)
	}
	if len(*asked) != 0 {
		t.Errorf("it asked anyway: %v", *asked)
	}
}

// A refusal is reported rather than swallowed, so a key that has run
// out says so instead of every place quietly losing its name.
func TestARefusalIsReported(t *testing.T) {
	s, _ := answering(t, 401, `{"error":"no"}`)

	if _, err := (place.Geoapify{Key: "k", URL: s.URL}).Name(context.Background(), 12.9, 80.06); err == nil {
		t.Error("a 401 was treated as success")
	}
}

// The position asked about is the position given, to six places --
// about a tenth of a metre, well under the twenty the phone is out by.
func TestItAsksAboutThePositionItWasGiven(t *testing.T) {
	s, asked := answering(t, 200, `{"features":[]}`)

	if _, err := (place.Geoapify{Key: "k", URL: s.URL}).Name(context.Background(), 12.9108472, 80.0622726); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked %d times", len(*asked))
	}
	for _, want := range []string{"lat=12.910847", "lon=80.062273", "apiKey=k"} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("query %q is missing %q", (*asked)[0], want)
		}
	}
}

// two : A stand-in for both endpoints, counting what each was asked.
func two(t *testing.T, places, reverse string) (p, r string, asked *map[string]int) {
	t.Helper()
	count := map[string]int{}
	serve := func(which, body string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			count[which]++
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(s.Close)
		return s
	}
	return serve("places", places).URL, serve("reverse", reverse).URL, &count
}

// A business is what somebody would say they went to. The street is
// how they got there, and is now asked for either way.
//
// That is a second credit on every named stay, where before it was
// one, and it was bought deliberately. A cinema was recorded as
// "Flavors non veg restaurant" because the nearest named thing was
// a hundred and twenty-seven metres away across a trunk road, and
// nothing in the record said so. The address and the distance are
// the only two things that would have shown it. Three thousand
// credits a month against a handful of named stays a day leaves
// room for both.
func TestABusinessKeepsTheStreetBesideIt(t *testing.T) {
	p, r, asked := two(t,
		`{"features":[{"properties":{"name":"Phoenix Marketcity","distance":55}}]}`,
		`{"features":[{"properties":{"name":"Phoenix Marketcity","street":"Velachery Main Road",`+
			`"formatted":"Phoenix Marketcity, Velachery Main Road, Chennai - 600042"}}]}`)

	got, err := place.Geoapify{Key: "k", Places: p, URL: r}.Name(context.Background(), 12.99, 80.21)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Phoenix Marketcity" {
		t.Errorf("called it %q, want the business", got.Name)
	}
	if got.Metres != 55 {
		t.Errorf("it kept %v metres, want 55", got.Metres)
	}
	// The name is not repeated inside the address; it is already the
	// name.
	if got.Address != "Velachery Main Road, Chennai - 600042" {
		t.Errorf("the address is %q", got.Address)
	}
	if (*asked)["reverse"] != 1 {
		t.Errorf("the address was not asked for: %v", *asked)
	}
}

// Most of a residential neighbourhood has no business in it. Theirs
// has two within a kilometre.
func TestWithNoBusinessItFallsBackToTheStreet(t *testing.T) {
	p, r, asked := two(t, `{"features":[]}`,
		`{"features":[{"properties":{"street":"krishna nagar 9th street"}}]}`)

	got, _ := place.Geoapify{Key: "k", Places: p, URL: r}.Name(context.Background(), 12.91, 80.06)
	if got.Name != "krishna nagar 9th street" {
		t.Errorf("called it %q, want the street", got.Name)
	}
	if (*asked)["places"] != 1 || (*asked)["reverse"] != 1 {
		t.Errorf("want one of each, got %v", *asked)
	}
}

// A shop six hundred metres away is not where somebody sat, however
// much it is the nearest named thing.
func TestSomethingTooFarAwayIsNotWhereTheyWere(t *testing.T) {
	p, r, _ := two(t,
		`{"features":[{"properties":{"name":"Bharat Petroleum","distance":707}}]}`,
		`{"features":[{"properties":{"street":"krishna nagar 9th street"}}]}`)

	got, _ := place.Geoapify{Key: "k", Places: p, URL: r}.Name(context.Background(), 12.91, 80.06)
	if got.Name != "krishna nagar 9th street" {
		t.Errorf("called it %q: a place 707 m away was taken as where they were", got.Name)
	}
}

// The nearest named one, not the first returned.
func TestTheNearestNamedPlaceWins(t *testing.T) {
	p, r, _ := two(t, `{"features":[
		{"properties":{"name":"Burger King","distance":120}},
		{"properties":{"distance":5}},
		{"properties":{"name":"Starbucks","distance":40}}]}`,
		`{"features":[{"properties":{"street":"somewhere"}}]}`)

	got, _ := place.Geoapify{Key: "k", Places: p, URL: r}.Name(context.Background(), 12.99, 80.21)
	if got.Name != "Starbucks" {
		t.Errorf("called it %q, want the nearest named one", got.Name)
	}
}

// The places search failing is not the end of it: the street is still
// worth having, and asking for it is another credit rather than
// another problem.
func TestAFailedPlacesSearchStillGetsTheStreet(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(broken.Close)
	_, r, _ := two(t, `{}`, `{"features":[{"properties":{"street":"krishna nagar 9th street"}}]}`)

	got, err := place.Geoapify{Key: "k", Places: broken.URL, URL: r}.Name(context.Background(), 12.91, 80.06)
	if err != nil || got.Name != "krishna nagar 9th street" {
		t.Errorf("got.Name %q, %v; want the street and no error", got.Name, err)
	}
}

// TestTheAddressWouldHaveShownTheNameWasWrong : Owner went to a
// cinema at Sky Hub Mall, Kilambakkam, and the stay was recorded as
// "Flavors non veg restaurant". The mall is not in OpenStreetMap,
// so the nearest named thing Geoapify had was a restaurant across
// the trunk road.
//
// Nothing can invent the mall. What can be carried is the road and
// the distance, and both say plainly that the name is a guess: a
// shop somebody is inside is twenty metres away, not a hundred and
// twenty-seven.
func TestTheAddressWouldHaveShownTheNameWasWrong(t *testing.T) {
	p, r, _ := two(t,
		`{"features":[{"properties":{"name":"Flavors non veg restaurant","distance":126.95}}]}`,
		`{"features":[{"properties":{"name":"Flavors non veg restaurant",`+
			`"street":"Grand Southern Trunk Road","suburb":"Urapakkam","city":"Vandalur",`+
			`"postcode":"603210","distance":126.95,`+
			`"formatted":"Flavors non veg restaurant, Grand Southern Trunk Road, `+
			`Urapakkam, Vandalur - 603210, TN, India"}}]}`)

	got, err := place.Geoapify{Key: "k", Places: p, URL: r}.Name(context.Background(), 12.87382, 80.07728)
	if err != nil {
		t.Fatal(err)
	}
	if got.Metres < 100 {
		t.Errorf("the distance was lost: %v", got.Metres)
	}
	for _, want := range []string{"Grand Southern Trunk Road", "Urapakkam", "603210"} {
		if !strings.Contains(got.Address, want) {
			t.Errorf("the address is missing %q: %q", want, got.Address)
		}
	}
}
