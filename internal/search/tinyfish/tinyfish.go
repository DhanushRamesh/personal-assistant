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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
func (c *Client) Search(ctx context.Context, ask search.Ask) ([]search.Result, error) {
	if c == nil {
		return nil, fmt.Errorf("tinyfish: no key")
	}
	limit := ask.Limit
	if limit <= 0 {
		limit = 5
	}

	q := url.Values{}
	q.Set("query", ask.Query)
	q.Set("domain_type", "web")
	if ask.News {
		q.Set("domain_type", "news")
	}
	if ask.Days > 0 {
		q.Set("recency_minutes", strconv.Itoa(ask.Days*24*60))
	}

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

// FetchURL : Where pages are read.
const FetchURL = "https://api.fetch.tinyfish.ai"

// page : What the fetch endpoint sends back for one URL.
type page struct {
	URL      string `json:"url"`
	FinalURL string `json:"final_url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	Error    string `json:"error"`
}

// Read : What these pages say.
//
// All of them in one call, which is what the endpoint takes and why
// it allows a hundred and fifty a minute: reading three results of
// a search is one request rather than three.
func (c *Client) Read(ctx context.Context, urls []string) ([]search.Page, error) {
	if c == nil {
		return nil, fmt.Errorf("tinyfish: no key")
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("tinyfish: nothing to read")
	}

	body, err := json.Marshal(map[string]any{"urls": urls})
	if err != nil {
		return nil, fmt.Errorf("tinyfish: writing the request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, FetchURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tinyfish: building the request: %w", err)
	}
	req.Header.Set("X-API-Key", c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tinyfish: reading the pages: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("tinyfish: a hundred and fifty pages a minute was exceeded (HTTP 429)")
	default:
		return nil, fmt.Errorf("tinyfish: the pages were refused (HTTP %d)", resp.StatusCode)
	}

	var said struct {
		Results []page `json:"results"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		return nil, fmt.Errorf("tinyfish: reading the answer: %w", err)
	}
	if said.Error != nil {
		return nil, fmt.Errorf("tinyfish: %s", said.Error.Message)
	}

	out := make([]search.Page, 0, len(said.Results))
	for _, p := range said.Results {
		out = append(out, search.Page{
			URL: p.URL, Final: p.FinalURL, Title: p.Title,
			Text: p.Text, Trouble: strings.TrimSpace(p.Error),
		})
	}
	return out, nil
}
