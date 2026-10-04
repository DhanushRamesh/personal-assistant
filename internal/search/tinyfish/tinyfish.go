// Package tinyfish : The way to look something up when nothing is
// waiting.
//
// Slower and slightly less accurate than the quick one -- 2.62s at
// 92.0% against 510ms at 95.3% -- and it has no monthly allowance
// at all. Thirty searches a minute, five hundred an hour, nothing
// counted and no card. That is what makes it both the fallback for
// a conversation and the only sensible choice for a background loop,
// which has all the time in the world and no business spending an
// allowance that somebody waiting may need.
package tinyfish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/search"
)

// URL : Where searches are sent.
const URL = "https://api.search.tinyfish.ai"

// Client : TinyFish, as this server uses it.
type Client struct {
	key  string
	http *http.Client
}

// New : One over the given key, or nil when there is none.
func New(key string, timeout time.Duration) *Client {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{key: strings.TrimSpace(key), http: &http.Client{Timeout: timeout}}
}

// Where : Which service this is.
func (c *Client) Where() string { return "tinyfish" }

// answer : What the service sends back.
type answer struct {
	Results []struct {
		Title    string `json:"title"`
		URL      string `json:"url"`
		Snippet  string `json:"snippet"`
		SiteName string `json:"site_name"`
		Date     string `json:"date"`
	} `json:"results"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Search : What the web says about this.
//
// The service takes a purpose as well as a query, and giving it one
// is free: it is the difference between ranking for the words and
// ranking for the question behind them.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]search.Result, error) {
	if c == nil {
		return nil, fmt.Errorf("tinyfish: no key")
	}
	if limit <= 0 {
		limit = 5
	}

	q := url.Values{}
	q.Set("query", query)
	q.Set("domain_type", "web")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL+"/?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("tinyfish: building the request: %w", err)
	}
	req.Header.Set("X-API-Key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tinyfish: sending the search: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		// Thirty a minute, and this is one person. Hitting it means
		// something is looping, which is worth saying plainly rather
		// than dressing up as a search that found nothing.
		return nil, fmt.Errorf("tinyfish: thirty searches a minute was exceeded (HTTP 429)")
	default:
		return nil, fmt.Errorf("tinyfish: the search was refused (HTTP %d)", resp.StatusCode)
	}

	var said answer
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		return nil, fmt.Errorf("tinyfish: reading the answer: %w", err)
	}
	if said.Error != nil {
		return nil, fmt.Errorf("tinyfish: %s (%s)", said.Error.Message, said.Error.Code)
	}

	// Trimmed here rather than asked for, because the service pages
	// rather than taking a count and a short page is a second call.
	out := make([]search.Result, 0, limit)
	for _, r := range said.Results {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		out = append(out, search.Result{
			Title: r.Title, URL: r.URL, Snippet: r.Snippet,
			Site: strings.TrimSpace(r.SiteName), When: strings.TrimSpace(r.Date),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Within : How long a background search may take.
//
// Generous on purpose. Nothing is waiting, and a loop that gives up
// at two seconds on a service measured at 2.62 never searches at
// all.
const Within = 30 * time.Second
