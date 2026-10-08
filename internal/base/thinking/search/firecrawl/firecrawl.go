// Package firecrawl : The quick way to look something up.
//
// Measured at 510ms and 95.3% right, which is what makes it the one
// to ask while somebody is waiting. It allows a thousand credits a
// month and a search costs two, so five hundred searches, after
// which it answers 402 and the caller falls back.
package firecrawl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/search"
)

// URL : Where searches are sent.
const URL = "https://api.firecrawl.dev/v2/search"

// Client : Firecrawl, as this server uses it.
type Client struct {
	key  string
	http *http.Client
}

// New : One over the given key, or nil when there is none.
//
// Nil rather than a client that fails on every call, so the caller
// can leave it out of the pair and the spare simply answers
// everything. A server with no key is a working server that cannot
// reach Firecrawl.
func New(key string, timeout time.Duration) *Client {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	// One client, kept: a new one per request pays a TLS handshake
	// every time, which on this service is more than the search.
	return &Client{key: strings.TrimSpace(key), http: &http.Client{Timeout: timeout}}
}

// Where : Which service this is.
func (c *Client) Where() string { return "firecrawl" }

// answer : What the service sends back.
//
// The shape changes with the request: asked for page content it
// returns a flat list, and asked for search alone it groups by
// source. Both are read, because a response that parsed yesterday
// and not today is worse than one that never worked.
type answer struct {
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

type found struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// Search : What the web says about this.
func (c *Client) Search(ctx context.Context, ask search.Ask) ([]search.Result, error) {
	if c == nil {
		return nil, fmt.Errorf("firecrawl: no key")
	}
	limit := ask.Limit
	if limit <= 0 {
		limit = 5
	}

	// No scrapeOptions. Snippets answer most questions, a page costs
	// a credit each and seconds each, and reading one is a separate
	// act somebody should choose rather than pay for by default.
	sending := map[string]any{"query": ask.Query, "limit": limit}
	if ask.News {
		sending["sources"] = []string{"news"}
	}
	// Google's own time filter, which is what this service passes
	// through: a day, a week, a month, a year, nearest above.
	if since := within(ask.Days); since != "" {
		sending["tbs"] = since
	}
	body, err := json.Marshal(sending)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: writing the request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("firecrawl: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: sending the search: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusPaymentRequired:
		// "Payment Required: Insufficient credits", and the service
		// says itself that it is not worth retrying.
		return nil, fmt.Errorf("firecrawl: %w (HTTP 402)", search.ErrSpent)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("firecrawl: asked to slow down (HTTP 429)")
	default:
		return nil, fmt.Errorf("firecrawl: the search was refused (HTTP %d)", resp.StatusCode)
	}

	var said answer
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		return nil, fmt.Errorf("firecrawl: reading the answer: %w", err)
	}
	if said.Error != "" {
		return nil, fmt.Errorf("firecrawl: %s", said.Error)
	}
	return read(said.Data), nil
}

// read : The results, whichever shape they came in.
func read(data json.RawMessage) []search.Result {
	var grouped struct {
		Web  []found `json:"web"`
		News []found `json:"news"`
	}
	var flat []found

	var items []found
	switch {
	case json.Unmarshal(data, &grouped) == nil && (len(grouped.Web) > 0 || len(grouped.News) > 0):
		items = append(grouped.Web, grouped.News...)
	case json.Unmarshal(data, &flat) == nil:
		items = flat
	}

	out := make([]search.Result, 0, len(items))
	for _, f := range items {
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		out = append(out, search.Result{
			Title: f.Title, URL: f.URL, Snippet: f.Description, Site: site(f.URL),
		})
	}
	return out
}

// site : The host a result came from, for saying where something was
// read without reading a URL aloud.
func site(raw string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	s = strings.TrimPrefix(s, "www.")
	if i := strings.IndexAny(s, "/?#"); i > 0 {
		s = s[:i]
	}
	return s
}

// within : A number of days as the time filter this service takes.
//
// Rounded up to the next bracket it understands rather than down,
// because a filter tighter than asked for hides things that should
// have been found, and one looser only lets a little extra through.
func within(days int) string {
	switch {
	case days <= 0:
		return ""
	case days <= 1:
		return "qdr:d"
	case days <= 7:
		return "qdr:w"
	case days <= 31:
		return "qdr:m"
	default:
		return "qdr:y"
	}
}

// ScrapeURL : Where one page is read.
const ScrapeURL = "https://api.firecrawl.dev/v2/scrape"

// Read : What these pages say.
//
// One request per page, because that is what this endpoint takes,
// and one credit per page with it. Sequential rather than at once:
// the free tier allows ten a minute and nothing here is reading
// more than a handful, so parallelism would buy a rate-limit
// refusal rather than speed.
//
// A page that cannot be read is carried as trouble rather than
// returned as an error. Two of three working is an answer; losing
// all three because one was behind a login is not.
func (c *Client) Read(ctx context.Context, urls []string) ([]search.Page, error) {
	if c == nil {
		return nil, fmt.Errorf("firecrawl: no key")
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("firecrawl: nothing to read")
	}

	out := make([]search.Page, 0, len(urls))
	for _, u := range urls {
		got, err := c.one(ctx, u)
		if err != nil {
			// A spent allowance is about the account, not this page,
			// so it stops the lot and lets the caller fall back.
			if errors.Is(err, search.ErrSpent) || ctx.Err() != nil {
				return nil, err
			}
			out = append(out, search.Page{URL: u, Trouble: err.Error()})
			continue
		}
		out = append(out, got)
	}
	return out, nil
}

// one : A single page.
func (c *Client) one(ctx context.Context, url string) (search.Page, error) {
	body, err := json.Marshal(map[string]any{"url": url, "formats": []string{"markdown"}})
	if err != nil {
		return search.Page{}, fmt.Errorf("firecrawl: writing the request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ScrapeURL, bytes.NewReader(body))
	if err != nil {
		return search.Page{}, fmt.Errorf("firecrawl: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return search.Page{}, fmt.Errorf("firecrawl: reading the page: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusPaymentRequired:
		return search.Page{}, fmt.Errorf("firecrawl: %w (HTTP 402)", search.ErrSpent)
	default:
		return search.Page{}, fmt.Errorf("the page was refused (HTTP %d)", resp.StatusCode)
	}

	var said struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Markdown string `json:"markdown"`
			Metadata struct {
				Title      string `json:"title"`
				SourceURL  string `json:"sourceURL"`
				StatusCode int    `json:"statusCode"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		return search.Page{}, fmt.Errorf("firecrawl: reading the answer: %w", err)
	}
	if said.Error != "" {
		return search.Page{}, fmt.Errorf("firecrawl: %s", said.Error)
	}

	return search.Page{
		URL: url, Final: said.Data.Metadata.SourceURL,
		Title: said.Data.Metadata.Title, Text: said.Data.Markdown,
	}, nil
}
