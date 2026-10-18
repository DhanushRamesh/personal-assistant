// Package tavily : A search built for answering a model rather than a
// person.
//
// The third service, added 18 October 2026. Owner, having looked at
// what is free: *"yes add tavily behind web_search."*
//
// It is free at this person's rate by a wide margin. A basic search
// costs one credit and a thousand come free each month, against
// thirty-three searches in the whole history of this server and two to
// four on a busy day -- about a tenth of the allowance. When the
// credits are gone it refuses rather than bills, which is the
// behaviour [search.ErrSpent] exists for.
//
// What makes it worth having beside the other two is what it returns:
// cleaned page text rather than a list of links, which is what a model
// about to answer actually needs.
package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/search"
)

// URL : Where searches are sent.
const URL = "https://api.tavily.com"

// Client : Tavily, as this server uses it.
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
func (c *Client) Where() string { return "tavily" }

// spent : The statuses that mean the allowance is gone rather than the
// request being wrong.
//
// 432 is theirs for a plan limit, and 429 is the ordinary rate limit.
// Both mean asking again at once is pointless, which is what ErrSpent
// says and why the Pair rests a service that returns it.
func spent(code int) bool {
	return code == 429 || code == 432
}

// send : One request to an endpoint, with the answer decoded into out.
func (c *Client) send(ctx context.Context, path string, body any, out any) error {
	if c == nil {
		return fmt.Errorf("tavily: no key")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("tavily: writing the request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, URL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("tavily: building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tavily: sending the request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case spent(resp.StatusCode):
		return fmt.Errorf("tavily: %w (HTTP %d)", search.ErrSpent, resp.StatusCode)
	default:
		return fmt.Errorf("tavily: the request was refused (HTTP %d)", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tavily: reading the answer: %w", err)
	}
	return nil
}

// found : What the search endpoint sends back.
type found struct {
	Results []struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Content   string `json:"content"`
		Published string `json:"published_date"`
	} `json:"results"`
}

// Since : A day in the form the service takes, counted back from now.
//
// Their search has no "last N days": it takes time_range in whole
// days, weeks, months or years, or a start_date. The date is used,
// because "the last three days" is what was asked and "the last week"
// is not.
func Since(days int, now time.Time) string {
	return now.AddDate(0, 0, -days).Format("2006-01-02")
}

// Search : What the web says about this.
func (c *Client) Search(ctx context.Context, ask search.Ask) ([]search.Result, error) {
	limit := ask.Limit
	if limit <= 0 {
		limit = 5
	}
	body := map[string]any{
		"query": ask.Query,
		// Explicitly basic. Their auto_parameters costs two credits
		// because it may choose advanced for you, and a thousand a
		// month is only generous while each one costs one.
		"search_depth":           "basic",
		"max_results":            limit,
		"topic":                  "general",
		"include_published_date": true,
	}
	if ask.News {
		body["topic"] = "news"
	}
	if ask.Days > 0 {
		body["start_date"] = Since(ask.Days, time.Now())
	}

	var said found
	if err := c.send(ctx, "/search", body, &said); err != nil {
		return nil, err
	}
	return results(said, limit), nil
}

// results : What the service sent, as this server's own shape.
//
// Its own function so it can be read without a server to answer:
// everything that can be wrong here -- a result with no address, more
// than were asked for, a missing date -- is wrong in the mapping and
// not in the request.
func results(said found, limit int) []search.Result {
	out := make([]search.Result, 0, limit)
	for _, r := range said.Results {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		out = append(out, search.Result{
			Title: r.Title, URL: r.URL, Snippet: r.Content,
			Site: Site(r.URL), When: strings.TrimSpace(r.Published),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Site : Who published it, from the address.
//
// The service does not say, and the other two do. Taken from the host
// rather than left empty, because the name of the paper is half of
// what tells somebody whether to believe a headline.
func Site(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// Most : How many pages their extract endpoint takes at once.
const Most = 20

// Read : What these pages say.
//
// Markdown rather than text: it is their default, it is quicker, and
// what is wanted from a page is its words rather than its layout.
func (c *Client) Read(ctx context.Context, urls []string) ([]search.Page, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("tavily: nothing to read")
	}
	if len(urls) > Most {
		urls = urls[:Most]
	}

	var said got
	if err := c.send(ctx, "/extract", map[string]any{
		"urls": urls, "extract_depth": "basic", "format": "markdown",
	}, &said); err != nil {
		return nil, err
	}
	return pages(said)
}

// got : What the extract endpoint sends back.
type got struct {
	Results []struct {
		URL  string `json:"url"`
		Text string `json:"raw_content"`
	} `json:"results"`
	Failed []struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	} `json:"failed_results"`
}

// pages : The pages it managed, or why it managed none.
//
// Both arrays are read. A 200 with everything in failed_results is how
// this endpoint says nothing could be fetched, and a reader handed an
// empty list with no error would say the pages were blank rather than
// that they could not be had.
func pages(said got) ([]search.Page, error) {
	out := make([]search.Page, 0, len(said.Results))
	for _, p := range said.Results {
		out = append(out, search.Page{URL: p.URL, Final: p.URL, Text: p.Text})
	}
	if len(out) == 0 {
		why := "none of them could be read"
		if len(said.Failed) > 0 && strings.TrimSpace(said.Failed[0].Error) != "" {
			why = said.Failed[0].Error
		}
		return nil, fmt.Errorf("tavily: %s", why)
	}
	return out, nil
}
