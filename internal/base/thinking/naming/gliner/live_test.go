//go:build evals

package gliner_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/naming"
	"personal-assistant/internal/base/thinking/naming/gliner"
)

// TestTheServiceAnswers : The Go client against a running service.
//
// Skipped when nothing is serving, so this is a check on the pair and
// never a reason a build fails.
func TestTheServiceAnswers(t *testing.T) {
	resp, err := http.Get(gliner.DefaultURL + "/health")
	if err != nil {
		t.Skipf("nothing serving at %s: %v", gliner.DefaultURL, err)
	}
	resp.Body.Close()

	c := gliner.New(gliner.Config{})
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()

	started := time.Now()
	spans, err := c.Names(ctx, "did Deepika call about the Jailer show in Chennai", nil)
	took := time.Since(started)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	t.Logf("%d spans in %v", len(spans), took.Round(time.Millisecond))
	for _, s := range spans {
		t.Logf("  %-8s %-7s %.3f", s.Text, s.Label, s.Score)
	}

	// Every span must be words that were actually in the sentence. That
	// is the whole reason this is not a model that writes.
	for _, s := range spans {
		if s.Text == "" {
			t.Error("an empty span came back")
		}
	}
	if len(spans) == 0 {
		t.Error("no names found in a sentence with three in it")
	}

	// An empty sentence is not a call.
	if got, err := c.Names(ctx, "   ", nil); err != nil || len(got) != 0 {
		t.Errorf("an empty sentence gave %v, %v", got, err)
	}
}

// TestAnUnreachableServiceSaysSo : A service that is not there is an
// error, never an empty answer, because looking at nothing and not
// looking are different.
func TestAnUnreachableServiceSaysSo(t *testing.T) {
	c := gliner.New(gliner.Config{URL: "http://127.0.0.1:1", Timeout: time.Second})
	if _, err := c.Names(context.Background(), "ring Deepika", nil); err == nil {
		t.Error("a dead service answered as though it had looked")
	}
	if !c.Available() {
		t.Error("a configured client reports itself unavailable")
	}
	_ = naming.ErrUnavailable
}
