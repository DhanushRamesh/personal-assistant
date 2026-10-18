package web_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	webtool "personal-assistant/internal/app/tool/web"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/search"
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
	// asks : every search made, because one call from the model may
	// now be two to the service -- a news search that finds almost
	// nothing is asked again of the web at large.
	asks []search.Ask
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
	s.asks = append(s.asks, ask)
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
	// The first call, not the last: a news search that finds nothing
	// is now asked again without the filter, so the fake sees two.
	if len(s.asks) == 0 || !s.asks[0].News {
		t.Error("news did not reach the service")
	}
}

// TestANewsSearchThatFindsNothingWidens : the news index is a narrow
// one, and the argument that chooses it cannot be described well
// enough to be got right every time.
//
// Asked who had been eliminated from Bigg Boss Tamil, the model set
// news -- an elimination is plainly something that happened -- and
// the answer is on entertainment sites no wire service indexes. It
// reported that the search only returned the Telugu version. The
// same query without the filter had the name, the date and the
// prize.
func TestANewsSearchThatFindsNothingWidens(t *testing.T) {
	s := &searcher{}
	ask(t, s, `{"query":"Bigg Boss Tamil elimination","days":7,"news":true}`)

	if len(s.asks) != 2 {
		t.Fatalf("a news search finding nothing should be asked again, got %d searches", len(s.asks))
	}
	if !s.asks[0].News {
		t.Error("the first search should be the one the model asked for")
	}
	if s.asks[1].News {
		t.Error("the second should be of the web at large")
	}
	if s.asks[1].Days != 7 || s.asks[1].Query != "Bigg Boss Tamil elimination" {
		t.Errorf("only the filter should change: %+v", s.asks[1])
	}
}

// TestANewsSearchThatWorksIsLeftAlone : widening costs a round trip
// and a credit, and a search that answered does not need one.
func TestANewsSearchThatWorksIsLeftAlone(t *testing.T) {
	s := &searcher{found: []search.Result{
		{Title: "a", URL: "https://a"}, {Title: "b", URL: "https://b"},
		{Title: "c", URL: "https://c"},
	}}
	ask(t, s, `{"query":"election result","days":1,"news":true}`)
	if len(s.asks) != 1 {
		t.Errorf("a news search that found things should not be repeated, got %d", len(s.asks))
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

// readsPages : A service that searches and reads, recording what it was
// asked to read.
type readsPages struct {
	found []search.Result
	pages []search.Page
	err   error
	asked []string
}

func (r *readsPages) Where() string { return "test" }

func (r *readsPages) Search(context.Context, search.Ask) ([]search.Result, error) {
	return r.found, nil
}

func (r *readsPages) Read(_ context.Context, urls []string) ([]search.Page, error) {
	r.asked = append(r.asked, urls...)
	return r.pages, r.err
}

func searchTool(t *testing.T, service search.Service) tool.Tool {
	t.Helper()
	for _, x := range webtool.Tools(service) {
		if x.Name == "web_search" {
			return x
		}
	}
	t.Fatal("no web_search")
	return tool.Tool{}
}

// TestADeepSearchReadsThePagesToo : Owner, 18 October 2026: *"I want
// deep research in the chat call only, because I'll ask to cross-check
// and research there."* One tool call and not a conversation: a turn is
// cut at thirty seconds and a round of the model is four and a half.
func TestADeepSearchReadsThePagesToo(t *testing.T) {
	s := &readsPages{
		found: []search.Result{
			{Title: "One", URL: "https://a.test", Snippet: "a"},
			{Title: "Two", URL: "https://b.test", Snippet: "b"},
			{Title: "Three", URL: "https://c.test", Snippet: "c"},
			{Title: "Four", URL: "https://d.test", Snippet: "d"},
		},
		pages: []search.Page{{URL: "https://a.test", Text: "the first page says forty thousand"}},
	}
	got := searchTool(t, s).Run(context.Background(), tool.Invocation{
		Args: []byte(`{"query":"the roof","deep":true}`),
	})
	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q: %s", got.Outcome, got.Content)
	}
	if len(s.asked) != 3 {
		t.Errorf("read %d pages, want the top %d", len(s.asked), 3)
	}
	if !strings.Contains(got.Content, "forty thousand") {
		t.Errorf("what the page said never reached the model:\n%s", got.Content)
	}
	if !strings.Contains(strings.ToLower(got.Content), "where they disagree") {
		t.Errorf("nothing told it to compare them:\n%s", got.Content)
	}
}

// TestAShallowSearchReadsNothing : The ordinary search is unchanged,
// and reading is the dear act -- seconds each, and a credit each on the
// service that counts them.
func TestAShallowSearchReadsNothing(t *testing.T) {
	s := &readsPages{found: []search.Result{{Title: "One", URL: "https://a.test"}}}
	got := searchTool(t, s).Run(context.Background(), tool.Invocation{
		Args: []byte(`{"query":"the roof"}`),
	})
	if got.Outcome != "ok" {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if len(s.asked) != 0 {
		t.Errorf("an ordinary search read %d pages", len(s.asked))
	}
}

// TestAReadThatFailedStillAnswers : The search stands. A read that
// failed is a thinner answer, not a failed one.
func TestAReadThatFailedStillAnswers(t *testing.T) {
	s := &readsPages{
		found: []search.Result{{Title: "One", URL: "https://a.test", Snippet: "the snippet"}},
		err:   errors.New("the pages could not be had"),
	}
	got := searchTool(t, s).Run(context.Background(), tool.Invocation{
		Args: []byte(`{"query":"the roof","deep":true}`),
	})
	if got.Outcome != "ok" {
		t.Fatalf("a failed read failed the whole search: %q %s", got.Outcome, got.Content)
	}
	if !strings.Contains(got.Content, "the snippet") {
		t.Errorf("the results were lost with the pages:\n%s", got.Content)
	}
}
