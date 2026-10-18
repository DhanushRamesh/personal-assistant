package tavily

import (
	"strings"
	"testing"
	"time"
)

// TestAResultWithNoAddressIsLeftOut : One of several things that can
// be wrong in the mapping rather than in the request, which is why the
// mapping is its own function.
func TestAResultWithNoAddressIsLeftOut(t *testing.T) {
	said := found{}
	said.Results = append(said.Results,
		struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Content   string `json:"content"`
			Published string `json:"published_date"`
		}{Title: "Rain tomorrow", URL: "https://www.thehindu.com/weather", Content: "Showers.", Published: " 2026-10-19 "},
		struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Content   string `json:"content"`
			Published string `json:"published_date"`
		}{Title: "Nowhere", URL: "  "},
	)

	got := results(said, 5)
	if len(got) != 1 {
		t.Fatalf("%d results, want only the one with an address", len(got))
	}
	if got[0].Site != "thehindu.com" {
		t.Errorf("site = %q, want the host without www", got[0].Site)
	}
	if got[0].When != "2026-10-19" {
		t.Errorf("when = %q, want it trimmed", got[0].When)
	}
	if got[0].Snippet != "Showers." {
		t.Errorf("snippet = %q", got[0].Snippet)
	}
}

// TestNoMoreThanWereAskedFor : The service pages, and a limit asked
// for is a limit the caller is going to put in front of a model.
func TestNoMoreThanWereAskedFor(t *testing.T) {
	said := found{}
	for i := 0; i < 9; i++ {
		said.Results = append(said.Results, struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Content   string `json:"content"`
			Published string `json:"published_date"`
		}{Title: "one", URL: "https://x.test/" + strings.Repeat("a", i+1)})
	}
	if got := results(said, 3); len(got) != 3 {
		t.Errorf("%d results, want 3", len(got))
	}
}

// TestEveryPageFailingIsAFailure : A 200 with everything in
// failed_results is how this endpoint says nothing could be fetched.
// A reader handed an empty list and no error would say the pages were
// blank rather than that they could not be had.
func TestEveryPageFailingIsAFailure(t *testing.T) {
	var said got
	said.Failed = append(said.Failed, struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	}{URL: "https://x.test", Error: "Failed to retrieve content"})

	_, err := pages(said)
	if err == nil || !strings.Contains(err.Error(), "Failed to retrieve content") {
		t.Errorf("err = %v, want the service's own reason", err)
	}
}

func TestAPageThatWasReadComesBack(t *testing.T) {
	var said got
	said.Results = append(said.Results, struct {
		URL  string `json:"url"`
		Text string `json:"raw_content"`
	}{URL: "https://x.test", Text: "# Title\n\nwords"})

	out, err := pages(said)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || !strings.Contains(out[0].Text, "words") || out[0].Final != "https://x.test" {
		t.Errorf("pages = %+v", out)
	}
}

// TestSinceCountsBackFromToday : Their search has no "last N days", so
// the days asked for become a date.
func TestSinceCountsBackFromToday(t *testing.T) {
	now := time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC)
	if got := Since(3, now); got != "2026-10-15" {
		t.Errorf("three days back from the 18th = %q", got)
	}
}

// TestSpentIsToldApartFromBroken : 432 is their plan limit and 429 the
// rate limit. Both mean asking again at once is pointless, which is
// what ErrSpent says and why the Pair rests a service that returns it.
func TestSpentIsToldApartFromBroken(t *testing.T) {
	for _, code := range []int{429, 432} {
		if !spent(code) {
			t.Errorf("HTTP %d was not taken for a spent allowance", code)
		}
	}
	for _, code := range []int{400, 401, 500, 503} {
		if spent(code) {
			t.Errorf("HTTP %d was taken for a spent allowance", code)
		}
	}
}

func TestNoKeyIsNoClient(t *testing.T) {
	if c := New("   ", time.Second); c != nil {
		t.Error("a blank key made a client")
	}
}

func TestSiteIsTheHostWithoutWww(t *testing.T) {
	for address, want := range map[string]string{
		"https://www.thehindu.com/news/x": "thehindu.com",
		"https://arxiv.org/abs/1":         "arxiv.org",
		"not a url":                       "",
	} {
		if got := Site(address); got != want {
			t.Errorf("Site(%q) = %q, want %q", address, got, want)
		}
	}
}
