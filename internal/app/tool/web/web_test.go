package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	webtool "personal-assistant/internal/app/tool/web"
	"personal-assistant/internal/base/conversation"
	"personal-assistant/internal/base/search"
)

// searcher : A web that answers however a test tells it to.
type searcher struct {
	found []search.Result
	err   error
	query string
	limit int
	days  int
	news  bool
	read  []string
}

func (s *searcher) Where() string { return "test" }
func (s *searcher) Read(_ context.Context, urls []string) ([]search.Page, error) {
	s.read = urls
	if s.err != nil {
		return nil, s.err
	}
	return []search.Page{{URL: urls[0], Title: "A page", Text: "what it says"}}, nil
}
func (s *searcher) Search(_ context.Context, ask search.Ask) ([]search.Result, error) {
	s.query, s.limit, s.days, s.news = ask.Query, ask.Limit, ask.Days, ask.News
	return s.found, s.err
}

func ask(t *testing.T, s search.Service, args string) tool.Result {
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

// TestFreshnessReachesTheService : Asked what was trending today,
// the search returned a Pinterest board called "510 Tamil memes
// ideas in 2026" and a Play Store listing, because an index ranks
// an evergreen page above a fresh one and nothing had told it the
// question was about now. Both services offer the filter; neither
// was being given it.
func TestFreshnessReachesTheService(t *testing.T) {
	s := &searcher{}
	ask(t, s, `{"query":"Tamil trending","days":1,"news":true}`)
	if s.days != 1 {
		t.Errorf("days reached the service as %d, want 1", s.days)
	}
	if !s.news {
		t.Error("news did not reach the service")
	}
}

// TestNoFreshnessIsAnyTime : Most questions are not about now, and
// a filter nobody asked for hides the answer to them.
func TestNoFreshnessIsAnyTime(t *testing.T) {
	s := &searcher{}
	ask(t, s, `{"query":"what is a transformer"}`)
	if s.days != 0 || s.news {
		t.Errorf("an ordinary question was narrowed: days=%d news=%v", s.days, s.news)
	}
}

// read : The reading tool rather than the searching one.
func read(t *testing.T, s search.Service, args string) tool.Result {
	t.Helper()
	for _, x := range webtool.Tools(s) {
		if x.Name == "web_read" {
			return x.Run(context.Background(), tool.Invocation{
				Caller: tool.Caller{UserID: "usr_1"},
				Args:   json.RawMessage(args),
			})
		}
	}
	t.Fatal("web_read is not registered")
	return tool.Result{}
}

// TestItReadsWhatItWasGiven : The addresses come from a search, and
// go through as they came.
func TestItReadsWhatItWasGiven(t *testing.T) {
	s := &searcher{}
	got := read(t, s, `{"urls":["https://x.test/a"]}`)
	if len(s.read) != 1 || s.read[0] != "https://x.test/a" {
		t.Fatalf("read %v", s.read)
	}
	if !strings.Contains(got.Content, "what it says") {
		t.Errorf("the page did not reach the model: %s", got.Content)
	}
}

// TestItWillNotReadAnInventedAddress : Anything that is not a URL a
// search can have given is the model having made one up, and
// fetching an invented address is a round trip to nowhere.
func TestItWillNotReadAnInventedAddress(t *testing.T) {
	s := &searcher{}
	got := read(t, s, `{"urls":["the BBC article about it","example.com"]}`)
	if len(s.read) != 0 {
		t.Errorf("an invented address was fetched: %v", s.read)
	}
	if got.Outcome != conversation.OutcomeFailed {
		t.Errorf("it pretended to read: %s", got.Content)
	}
}

// TestOnlyAFewPagesAreRead : Reading is the dear act -- seconds
// each, and a credit each on the service that counts them.
func TestOnlyAFewPagesAreRead(t *testing.T) {
	s := &searcher{}
	read(t, s, `{"urls":["https://x.test/1","https://x.test/2","https://x.test/3","https://x.test/4","https://x.test/5"]}`)
	if len(s.read) != webtool.Pages {
		t.Errorf("read %d pages, want at most %d", len(s.read), webtool.Pages)
	}
}
