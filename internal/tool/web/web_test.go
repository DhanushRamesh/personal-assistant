package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/search"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	webtool "github.com/DhanushRamesh/personal-assistant/internal/tool/web"
)

// searcher : A web that answers however a test tells it to.
type searcher struct {
	found []search.Result
	err   error
	query string
	limit int
}

func (s *searcher) Where() string { return "test" }
func (s *searcher) Search(_ context.Context, query string, limit int) ([]search.Result, error) {
	s.query, s.limit = query, limit
	return s.found, s.err
}

func ask(t *testing.T, s search.Searcher, args string) tool.Result {
	t.Helper()
	return webtool.Tools(s)[0].Run(context.Background(), tool.Invocation{
		Caller: tool.Caller{UserID: "usr_1"},
		Args:   json.RawMessage(args),
	})
}

// TestItSaysWhatItFound : A snippet and where it came from, which is
// what an answer is built out of.
func TestItSaysWhatItFound(t *testing.T) {
	s := &searcher{found: []search.Result{
		{Title: "Phoenix Marketcity", URL: "https://x.test/p", Snippet: "Open till 10pm", Site: "x.test"},
	}}
	got := ask(t, s, `{"query":"Phoenix Marketcity hours","limit":3}`)

	if s.query != "Phoenix Marketcity hours" || s.limit != 3 {
		t.Errorf("searched %q limit %d", s.query, s.limit)
	}
	for _, want := range []string{"Phoenix Marketcity", "Open till 10pm", "x.test"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("missing %q in: %s", want, got.Content)
		}
	}
}

// TestAnEmptySearchIsNotProof : "Nothing came back" and "there is no
// such thing" are different sentences, and only one of them is true.
func TestAnEmptySearchIsNotProof(t *testing.T) {
	got := ask(t, &searcher{}, `{"query":"a thing"}`)
	if got.Outcome == conversation.OutcomeFailed {
		t.Errorf("an empty search was reported as a failure: %s", got.Content)
	}
	if !strings.Contains(got.Content, "not what is true") {
		t.Errorf("it let an empty search stand as proof: %s", got.Content)
	}
}

// TestNoWebIsSaidPlainly : A server with no keys cannot reach the
// web, and that is a different thing from a search that found
// nothing.
func TestNoWebIsSaidPlainly(t *testing.T) {
	got := ask(t, nil, `{"query":"anything"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Error("a server with no web pretended to search")
	}
}

// TestAFailureIsAFailure : A service that refused told us nothing
// about the world, and must not read as an empty result.
func TestAFailureIsAFailure(t *testing.T) {
	got := ask(t, &searcher{err: errors.New("the service is down")}, `{"query":"x"}`)
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("a refusal was reported as an answer: %s", got.Content)
	}
}

// TestItWillNotSearchForNothing : An empty query is a round trip
// that cannot answer anything.
func TestItWillNotSearchForNothing(t *testing.T) {
	s := &searcher{}
	if got := ask(t, s, `{"query":"   "}`); got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it searched for nothing: %s", got.Content)
	}
	if s.query != "" {
		t.Error("an empty query reached the service")
	}
}

// TestTooManyResultsAreTrimmed : A model handed ten reads several
// out loud.
func TestTooManyResultsAreTrimmed(t *testing.T) {
	s := &searcher{}
	ask(t, s, `{"query":"x","limit":500}`)
	if s.limit != webtool.Most {
		t.Errorf("asked for %d, want it capped at %d", s.limit, webtool.Most)
	}
}
