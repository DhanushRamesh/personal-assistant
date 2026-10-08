package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/search"
)

var at = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

// fake : A searcher that answers however a test tells it to.
type fake struct {
	where string
	found []search.Result
	err   error
	asked int
	slow  time.Duration
}

func (f *fake) Where() string { return f.where }
func (f *fake) Read(ctx context.Context, urls []string) ([]search.Page, error) {
	f.asked++
	if f.slow > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.slow):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return []search.Page{{URL: urls[0], Title: "a", Text: "some words"}}, nil
}
func (f *fake) Search(ctx context.Context, _ search.Ask) ([]search.Result, error) {
	f.asked++
	if f.slow > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.slow):
		}
	}
	return f.found, f.err
}

func pair(quick, spare *fake) *search.Pair {
	return &search.Pair{Quick: quick, Spare: spare, Now: func() time.Time { return at }}
}

func one(where string) *fake {
	return &fake{where: where, found: []search.Result{{Title: "a", URL: "https://x.test/a"}}}
}

// TestTheQuickOneAnswersWhenItCan : Owner: *"Firecrawl if I ask a
// question in conversation."* 510ms against 2.62s is the whole
// reason there are two.
func TestTheQuickOneAnswersWhenItCan(t *testing.T) {
	quick, spare := one("firecrawl"), one("tinyfish")
	if _, err := pair(quick, spare).Search(context.Background(), search.Ask{Query: "q", Limit: 3}); err != nil {
		t.Fatalf("searching: %v", err)
	}
	if quick.asked != 1 || spare.asked != 0 {
		t.Errorf("quick asked %d, spare %d; want 1 and 0", quick.asked, spare.asked)
	}
}

// TestItFallsBackWhenTheQuickOneCannot : Owner: *"if Firecrawl
// doesn't work or credit is over, use TinyFish as fallback."*
func TestItFallsBackWhenTheQuickOneCannot(t *testing.T) {
	for _, err := range []error{errors.New("down"), search.ErrSpent} {
		quick := &fake{where: "firecrawl", err: err}
		spare := one("tinyfish")
		got, gotErr := pair(quick, spare).Search(context.Background(), search.Ask{Query: "q", Limit: 3})
		if gotErr != nil {
			t.Fatalf("%v: the fallback did not answer: %v", err, gotErr)
		}
		if len(got) != 1 || spare.asked != 1 {
			t.Errorf("%v: spare asked %d, got %d results", err, spare.asked, len(got))
		}
	}
}

// TestASpentServiceIsLeftAlone : A spent allowance is monthly and
// the service says itself that retrying is pointless. Asking it on
// every search for the rest of the month is a wasted round trip
// every time, in the one place where the whole point was speed.
func TestASpentServiceIsLeftAlone(t *testing.T) {
	quick := &fake{where: "firecrawl", err: search.ErrSpent}
	spare := one("tinyfish")
	p := pair(quick, spare)

	for i := 0; i < 5; i++ {
		if _, err := p.Search(context.Background(), search.Ask{Query: "q", Limit: 3}); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}
	if quick.asked != 1 {
		t.Errorf("the spent service was asked %d times, want 1", quick.asked)
	}
	if spare.asked != 5 {
		t.Errorf("the spare answered %d of 5", spare.asked)
	}
}

// TestItTriesAgainOnceRested : Spent is not broken. The allowance
// renews, and a server left running for a month would otherwise
// never go back to the quick one.
func TestItTriesAgainOnceRested(t *testing.T) {
	quick := &fake{where: "firecrawl", err: search.ErrSpent}
	spare := one("tinyfish")
	now := at
	p := &search.Pair{Quick: quick, Spare: spare, Rested: time.Hour,
		Now: func() time.Time { return now }}

	p.Search(context.Background(), search.Ask{Query: "q", Limit: 3})
	now = at.Add(2 * time.Hour)
	p.Search(context.Background(), search.Ask{Query: "q", Limit: 3})

	if quick.asked != 2 {
		t.Errorf("the quick one was asked %d times, want 2", quick.asked)
	}
}

// TestAHangingQuickOneDoesNotEatTheTurn : A turn is cut at thirty
// seconds. A quick service that stops answering must cost its slice
// and no more, or it spends the budget the one that would have
// answered needed.
func TestAHangingQuickOneDoesNotEatTheTurn(t *testing.T) {
	quick := &fake{where: "firecrawl", slow: 5 * time.Second}
	spare := one("tinyfish")
	p := &search.Pair{Quick: quick, Spare: spare, Within: 50 * time.Millisecond,
		Now: func() time.Time { return at }}

	began := time.Now()
	got, err := p.Search(context.Background(), search.Ask{Query: "q", Limit: 3})
	if err != nil || len(got) != 1 {
		t.Fatalf("the fallback did not answer: %v", err)
	}
	if time.Since(began) > time.Second {
		t.Errorf("it waited %s on a service that had stopped answering", time.Since(began))
	}
}

// TestTheTurnEndingIsNotTheServiceFailing : Asking the spare after
// the person has gone is a second failure, more slowly.
func TestTheTurnEndingIsNotTheServiceFailing(t *testing.T) {
	quick := &fake{where: "firecrawl", slow: time.Second}
	spare := one("tinyfish")
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()

	if _, err := pair(quick, spare).Search(ctx, search.Ask{Query: "q", Limit: 3}); err == nil {
		t.Error("a turn that ended was reported as a search that worked")
	}
	if spare.asked != 0 {
		t.Error("the spare was asked after the turn was over")
	}
}

// TestNothingFoundIsNotNothingThere : The difference matters to
// somebody being told.
func TestNothingFoundIsNotNothingThere(t *testing.T) {
	said := search.Describe("a thing", nil)
	if !strings.Contains(said, "nothing came back") {
		t.Errorf("it did not say the search was empty: %q", said)
	}
	if !strings.Contains(said, "not what is true") {
		t.Errorf("it let an empty search stand as proof: %q", said)
	}
}

// TestWhatIsFoundSaysWhereItCameFrom : A snippet is somebody's
// claim, and a URL read aloud is a wasted sentence.
func TestWhatIsFoundSaysWhereItCameFrom(t *testing.T) {
	said := search.Describe("rain", []search.Result{{
		Title: "Chennai weather", URL: "https://imd.test/chennai",
		Snippet: "Rain expected", Site: "imd.test", When: "2026-10-04",
	}})
	for _, want := range []string{"Chennai weather", "Rain expected", "imd.test", "2026-10-04"} {
		if !strings.Contains(said, want) {
			t.Errorf("missing %q in: %s", want, said)
		}
	}
	if !strings.Contains(said, "do not read a URL aloud") {
		t.Error("nothing warns against reading a URL out")
	}
}

// TestReadingFallsBackTheSameWay : The fallback is one piece of
// code for both, because the four things it does -- resting a spent
// service, giving the quick one a slice, not mistaking the turn
// ending for a failure, and saying when it fell back -- are each
// the result of something going wrong, and none is worth writing
// twice.
func TestReadingFallsBackTheSameWay(t *testing.T) {
	quick := &fake{where: "firecrawl", err: search.ErrSpent}
	spare := one("tinyfish")
	p := pair(quick, spare)

	got, err := p.Read(context.Background(), []string{"https://x.test/a"})
	if err != nil || len(got) != 1 {
		t.Fatalf("the fallback did not read: %v", err)
	}
	// And the spent one is left alone for reading as for searching.
	p.Search(context.Background(), search.Ask{Query: "q", Limit: 3})
	if quick.asked != 1 {
		t.Errorf("the spent service was asked %d times, want 1", quick.asked)
	}
}

// TestAPageIsCutAndSaidToBeCut : A page can be a hundred thousand
// words and a turn is thirty seconds. Handing the whole of one over
// spends the budget on text nobody asked for; cutting it silently
// lets the model read half a sentence as the whole of something.
func TestAPageIsCutAndSaidToBeCut(t *testing.T) {
	long := strings.Repeat("word ", 4000)
	said := search.Said([]search.Page{{URL: "https://x.test/a", Title: "A", Text: long}}, 100)
	if len([]rune(said)) > 400 {
		t.Errorf("the page was not cut: %d characters", len([]rune(said)))
	}
	if !strings.Contains(said, "cut here") {
		t.Errorf("it was cut without saying so: %q", said[len(said)-80:])
	}
}

// TestOnePageFailingDoesNotLoseTheOthers : Two of three working is
// an answer. Losing all three because one was behind a login is
// not.
func TestOnePageFailingDoesNotLoseTheOthers(t *testing.T) {
	said := search.Said([]search.Page{
		{URL: "https://x.test/a", Title: "A", Text: "it says this"},
		{URL: "https://x.test/b", Trouble: "behind a login"},
	}, 1000)
	if !strings.Contains(said, "it says this") {
		t.Error("the page that worked was lost")
	}
	if !strings.Contains(said, "behind a login") {
		t.Error("the page that failed was not explained")
	}
}
