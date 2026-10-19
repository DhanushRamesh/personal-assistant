// Package gliner finds names with a local GLiNER service.
//
// deployments/gliner-service.py is the other half of this.
package gliner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"personal-assistant/internal/base/thinking/naming"
)

// DefaultURL : Where the name service answers.
const DefaultURL = "http://127.0.0.1:8092"

// DefaultTimeout : How long one call may take.
//
// Short, because this runs before the question goes anywhere and every
// millisecond of it is in front of the first word. The service answers
// a sentence in about fifty.
const DefaultTimeout = 2 * time.Second

// maxResponseBytes : The most that will be read from a reply, so a
// misbehaving endpoint cannot exhaust memory.
const maxResponseBytes = 1 << 20

// Config : What is needed to reach the name service.
type Config struct {
	// URL : Where it answers. Empty selects DefaultURL.
	URL string
	// Timeout : How long a call may take. Zero selects DefaultTimeout.
	Timeout time.Duration
	// HTTP : The client to use. Optional.
	HTTP *http.Client
}

// Client : A naming.Finder backed by a GLiNER service.
type Client struct {
	url   string
	http  *http.Client
	ready bool
}

// New : Builds a Client. It makes no call, so a server whose name
// service is not running still starts.
func New(cfg Config) *Client {
	url := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if url == "" {
		url = DefaultURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := cfg.HTTP
	if client == nil {
		// One client, kept: a client per call pays to open the
		// connection every time.
		client = &http.Client{Timeout: timeout}
	}
	return &Client{url: url, http: client, ready: true}
}

// Available : Whether a service was configured at all.
func (c *Client) Available() bool { return c != nil && c.ready }

type asked struct {
	Text      string   `json:"text"`
	Labels    []string `json:"labels,omitempty"`
	Threshold float64  `json:"threshold,omitempty"`
}

type answered struct {
	Spans []struct {
		Text  string  `json:"text"`
		Label string  `json:"label"`
		Score float64 `json:"score"`
	} `json:"spans"`
	Error string `json:"error"`
}

// Names : The spans in text that look like one of labels.
func (c *Client) Names(ctx context.Context, text string, labels []string) ([]naming.Span, error) {
	if !c.Available() {
		return nil, naming.ErrUnavailable
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	if len(labels) == 0 {
		labels = naming.Labels
	}

	body, err := json.Marshal(asked{Text: text, Labels: labels, Threshold: naming.Threshold})
	if err != nil {
		return nil, fmt.Errorf("gliner: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/spans", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gliner: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gliner: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("gliner: reading the answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gliner: the service answered %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var got answered
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("gliner: unreadable answer: %w", err)
	}
	if got.Error != "" {
		return nil, fmt.Errorf("gliner: %s", got.Error)
	}

	out := make([]naming.Span, 0, len(got.Spans))
	for _, s := range got.Spans {
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		out = append(out, naming.Span{Text: s.Text, Label: s.Label, Score: s.Score})
	}
	return out, nil
}
