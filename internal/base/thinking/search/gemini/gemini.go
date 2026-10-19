// Package gemini : Looking something up by asking a model that has
// already looked.
//
// Not a search service. Google's grounding answers the question and
// names what it read, where the others hand back a ranked list and
// leave the reading to whoever asked. That difference is the reason to
// try it and the reason to be careful with it: an answer is more
// useful than ten links and less checkable, since the words come from
// the model rather than from a page.
//
// Owner, 20 October 2026, told the measurements showed nothing wrong
// with the search we have: *"my web search isn't broken but gemini is
// exceptional results"*. What he has is 32 calls of 32 succeeding;
// what he is judging is whether the answers are any good, which the
// accuracy eval measures and the timings never did.
//
// It is reached directly. The Zoho gateway takes ai_vendor anthropic
// and openai and refuses google, so this carries its own key and shows
// in no credit figure the gateway reports.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/search"
)

// URL : Where a grounded answer is asked for. The model is in the path.
const URL = "https://generativelanguage.googleapis.com/v1beta/models/"

// Model : Which model answers. A fast one: this sits in a turn that
// has thirty seconds, beside everything else that turn must do.
const Model = "gemini-2.5-flash"

// Client : Google's grounded search, as this server would use it.
type Client struct {
	key   string
	model string
	http  *http.Client
}

// New : One over the given key, or nil when there is none.
//
// Nil rather than an error, like the others: a server with no key for
// a service simply does not have that service, and must still start.
func New(key, model string, timeout time.Duration) *Client {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	if strings.TrimSpace(model) == "" {
		model = Model
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		key:   strings.TrimSpace(key),
		model: strings.TrimSpace(model),
		http:  &http.Client{Timeout: timeout},
	}
}

// Where : Which service this is.
func (c *Client) Where() string { return "gemini" }

// asked : The request, with search turned on.
type asked struct {
	Contents []content `json:"contents"`
	Tools    []tool    `json:"tools"`
}

type content struct {
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

// tool : An empty google_search object is how grounding is asked for.
type tool struct {
	GoogleSearch struct{} `json:"google_search"`
}

// answer : What comes back. The text is the model's, and the chunks
// are what it read to write it.
type answer struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
		Grounding struct {
			Chunks []struct {
				Web struct {
					URI    string `json:"uri"`
					Title  string `json:"title"`
					Domain string `json:"domain"`
				} `json:"web"`
			} `json:"groundingChunks"`
		} `json:"groundingMetadata"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Search : What the web says about this.
//
// The answer comes back first, as a result with no URL of its own,
// and the pages it was written from come after it. Put that way round
// because the thing worth reading is the answer, and because the
// caller takes Limit results from the front.
//
// Its own result carries no URL deliberately. Nothing should offer
// the model's sentence as a page to go and read.
func (c *Client) Search(ctx context.Context, ask search.Ask) ([]search.Result, error) {
	body, err := json.Marshal(asked{
		Contents: []content{{Parts: []part{{Text: ask.Query}}}},
		Tools:    []tool{{}},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini: building the request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		URL+c.model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gemini: building the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// In a header rather than the query string, so a key is never
	// written into a URL that something may log.
	req.Header.Set("x-goog-api-key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: asking: %w", err)
	}
	defer resp.Body.Close()

	var got answer
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, fmt.Errorf("gemini: reading the reply (HTTP %d): %w", resp.StatusCode, err)
	}
	if got.Error != nil {
		return nil, fmt.Errorf("gemini: %s (HTTP %d)", got.Error.Message, got.Error.Code)
	}
	if len(got.Candidates) == 0 {
		return nil, fmt.Errorf("gemini: nothing came back (HTTP %d)", resp.StatusCode)
	}

	var said strings.Builder
	for _, p := range got.Candidates[0].Content.Parts {
		said.WriteString(p.Text)
	}

	out := make([]search.Result, 0, len(got.Candidates[0].Grounding.Chunks)+1)
	if answered := strings.TrimSpace(said.String()); answered != "" {
		out = append(out, search.Result{
			Title:   ask.Query,
			Snippet: answered,
			Site:    "Google Search",
		})
	}
	for _, chunk := range got.Candidates[0].Grounding.Chunks {
		if chunk.Web.URI == "" {
			continue
		}
		out = append(out, search.Result{
			Title: chunk.Web.Title,
			URL:   chunk.Web.URI,
			Site:  chunk.Web.Domain,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gemini: it read nothing and said nothing")
	}
	return out, nil
}
