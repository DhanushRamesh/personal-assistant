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

// Namer : Somewhere to ask what is at a position.
type Namer interface {
	Name(ctx context.Context, lat, lon float64) (string, error)
}

// Geoapify : Asks Geoapify.
type Geoapify struct {
	// Key : The owner's API key.
	Key string
	// URL : The reverse geocoding endpoint.
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
			Name          string `json:"name"`
			Street        string `json:"street"`
			Suburb        string `json:"suburb"`
			District      string `json:"district"`
			City          string `json:"city"`
			Neighbourhood string `json:"neighbourhood"`
		} `json:"properties"`
	} `json:"features"`
}

// Name : What is at a position, in as few words as will identify it.
//
// The most particular thing that came back, falling to the least. A
// building's own name is what somebody would say; a street is worth
// having; a city is not, on its own, a place you went -- but it is
// better than a pair of coordinates, and the person can correct any of
// it later.
func (g Geoapify) Name(ctx context.Context, lat, lon float64) (string, error) {
	if strings.TrimSpace(g.Key) == "" {
		return "", nil
	}

	endpoint := strings.TrimSpace(g.URL)
	if endpoint == "" {
		return "", fmt.Errorf("place: nowhere to ask")
	}
	ask, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("place: %w", err)
	}
	// The key goes in the query because that is the only way Geoapify
	// takes it. It is the owner's own key for their own account, and
	// the position is theirs: this is the one request in the server
	// where both are unavoidable.
	ask.RawQuery = url.Values{
		"lat":    {strconv.FormatFloat(lat, 'f', 6, 64)},
		"lon":    {strconv.FormatFloat(lon, 'f', 6, 64)},
		"format": {"geojson"},
		"apiKey": {g.Key},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ask.String(), nil)
	if err != nil {
		return "", fmt.Errorf("place: %w", err)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("place: asking what is there: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("place: it answered %s", resp.Status)
	}

	var got found
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return "", fmt.Errorf("place: reading the answer: %w", err)
	}
	if len(got.Features) == 0 {
		// Nothing there, which the sea and a motorway both produce.
		// Not a failure: there is genuinely no name for it.
		return "", nil
	}

	p := got.Features[0].Properties
	for _, candidate := range []string{p.Name, p.Street, p.Neighbourhood, p.Suburb, p.District, p.City} {
		if name := strings.TrimSpace(candidate); name != "" {
			return name, nil
		}
	}
	return "", nil
}

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
