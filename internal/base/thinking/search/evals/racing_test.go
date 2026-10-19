//go:build evals

// Package evals asks all three search services the same real questions
// and prints what each came back with, and how long it took.
//
// By eye on the answers and by the clock on the speed. There is no
// scoring: what makes one search better than another for this is
// whether the words it returns let the model answer, and nothing here
// can judge that but a person reading them.
//
// Costs real searches. Tavily's thousand free credits a month against
// this person's ninety means a run of five questions is half a per
// cent of the allowance.
package evals

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/search"
	"personal-assistant/internal/base/thinking/search/firecrawl"
	"personal-assistant/internal/base/thinking/search/gemini"
	"personal-assistant/internal/base/thinking/search/tavily"
	"personal-assistant/internal/base/thinking/search/tinyfish"
)

// asked : Real questions, of the kinds he actually asks.
//
// Two that failed this morning for want of searching at all, two about
// a thing happening now, and one about a place -- which is the kind an
// index ranks worst, because an evergreen page beats a fresh one.
var asked = []struct {
	Say  string
	News bool
	Days int
}{
	{Say: "who is the chief minister of Tamil Nadu"},
	{Say: "how was the movie Jailer 2 reviews"},
	{Say: "petrol price in Chennai today", Days: 2},
	{Say: "Chennai weather forecast tomorrow", Days: 1},
	{Say: "Phoenix Marketcity Chennai opening hours"},
}

func TestWhatEachSearchComesBackWith(t *testing.T) {
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	_ = slog.New(slog.NewTextHandler(io.Discard, nil))

	services := []search.Service{}
	for _, s := range []search.Service{
		tinyfish.New(string(cfg.Search.TinyFish), 30*time.Second),
		tavily.New(string(cfg.Search.Tavily), 30*time.Second),
		firecrawl.New(string(cfg.Search.Firecrawl), 30*time.Second),
	} {
		if s != nil && !nilUnder(s) {
			services = append(services, s)
		}
	}
	if len(services) < 2 {
		t.Skip("fewer than two services have keys, so there is nothing to compare")
	}

	took := map[string][]time.Duration{}
	failed := map[string]int{}

	for _, q := range asked {
		fmt.Printf("\n  \"%s\"\n", q.Say)
		for _, s := range services {
			began := time.Now()
			got, err := s.Search(context.Background(), search.Ask{
				Query: q.Say, Limit: 3, News: q.News, Days: q.Days,
			})
			spent := time.Since(began)
			took[s.Where()] = append(took[s.Where()], spent)
			if err != nil {
				failed[s.Where()]++
				fmt.Printf("    %-10s %6s  FAILED: %v\n", s.Where(), spent.Round(time.Millisecond), err)
				continue
			}
			fmt.Printf("    %-10s %6s  %d results\n", s.Where(), spent.Round(time.Millisecond), len(got))
			for i, r := range got {
				if i >= 2 {
					break
				}
				fmt.Printf("               · %s\n", first(r.Title, 72))
				fmt.Printf("                 %s\n", first(oneLine(r.Snippet), 110))
			}
		}
	}

	fmt.Printf("\n  %-10s %10s %10s %10s %8s\n", "", "median", "worst", "words", "failed")
	names := make([]string, 0, len(took))
	for n := range took {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		v := append([]time.Duration(nil), took[n]...)
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		fmt.Printf("  %-10s %10v %10v %10s %8d\n", n,
			v[len(v)/2].Round(time.Millisecond), v[len(v)-1].Round(time.Millisecond),
			"", failed[n])
	}
	fmt.Println()
}

// nilUnder : Whether a service is a typed nil, which satisfies the
// interface and then fails on every call.
func nilUnder(s search.Service) bool {
	switch v := s.(type) {
	case *tinyfish.Client:
		return v == nil
	case *tavily.Client:
		return v == nil
	case *firecrawl.Client:
		return v == nil
	}
	return false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func first(s string, n int) string {
	s = oneLine(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// answerable : Questions with a fact that can be checked in what comes
// back, so accuracy is counted rather than judged by eye.
//
// The fact is looked for in the titles and snippets together, which is
// all the model is given before it decides whether to read a page. A
// service that returns the right pages but says nothing useful about
// them has not answered: the next step costs another round.
//
// Facts chosen to be stable or to have been seen in a real run, never
// invented. Want is any-of: several wordings of the same fact.
var answerable = []struct {
	Say  string
	Days int
	Want []string
	Why  string
}{
	{Say: "who is the chief minister of Tamil Nadu",
		Want: []string{"vijay"}, Why: "the name"},
	{Say: "what is the capital of Japan",
		Want: []string{"tokyo"}, Why: "the city"},
	{Say: "Phoenix Marketcity Chennai mall timings",
		Want: []string{"10:00 am", "10 am", "10am"}, Why: "when it opens"},
	// Widened on 20 October 2026, when it was found to be scoring the
	// wrong answer right. The price that day was 107.77, which was in
	// nobody's wanted list: Gemini said it and was marked wrong, and
	// the services that matched "100." matched a figure from months
	// earlier. A checker that names the answer it expects goes stale
	// with the answer. This one takes any plausible rupee figure.
	{Say: "petrol price in Chennai today", Days: 2,
		Want: []string{"99.", "100.", "101.", "102.", "103.", "104.",
			"105.", "106.", "107.", "108.", "109.", "110."},
		Why: "a rupee figure"},
	{Say: "Chennai weather forecast tomorrow", Days: 1,
		Want: []string{"chennai"}, Why: "the right city"},
	{Say: "latest Claude model from Anthropic",
		Want: []string{"claude"}, Why: "the subject at all"},
}

// TestHowOftenEachSearchAnswers : Accuracy, counted.
//
// Owner, 18 October 2026, told that Tavily was the slowest: *"based on
// accuracy."* Speed was the easy half; this is the half that decides
// which service is worth asking first.
func TestHowOftenEachSearchAnswers(t *testing.T) {
	services := searchers(t)
	if len(services) < 2 {
		t.Skip("fewer than two services have keys, so there is nothing to compare")
	}

	right := map[string]int{}
	for _, q := range answerable {
		fmt.Printf("\n  \"%s\"  -- wanted: %s\n", q.Say, q.Why)
		for _, s := range services {
			got, err := s.Search(context.Background(), search.Ask{
				Query: q.Say, Limit: 3, Days: q.Days,
			})
			if err != nil {
				fmt.Printf("    %-10s FAILED: %v\n", s.Where(), err)
				continue
			}
			var all strings.Builder
			for _, r := range got {
				all.WriteString(strings.ToLower(r.Title + " " + r.Snippet + " "))
			}
			found := ""
			for _, w := range q.Want {
				if strings.Contains(all.String(), w) {
					found = w
					break
				}
			}
			if found != "" {
				right[s.Where()]++
				fmt.Printf("    %-10s yes   (%q)\n", s.Where(), found)
			} else {
				fmt.Printf("    %-10s no    top: %s\n", s.Where(), first(topOf(got), 86))
			}
		}
	}

	fmt.Printf("\n  answered, out of %d:\n", len(answerable))
	names := make([]string, 0, len(right))
	for _, s := range services {
		names = append(names, s.Where())
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Printf("    %-10s %d of %d\n", n, right[n], len(answerable))
	}
	fmt.Println()
}

// topOf : The first result's title, for saying what came back instead.
func topOf(got []search.Result) string {
	if len(got) == 0 {
		return "(nothing)"
	}
	return got[0].Title
}

// configured : The services that have a key, in the order the server
// asks them.
// searchers : Everything that can search, which is one more than
// everything that can also read.
//
// Gemini answers a question and names what it read; it does not fetch
// a page on demand, so it is not a search.Service and cannot go in the
// list below. Accuracy only needs searching, so it is compared here.
//
// Its key is read from ~/.config/jarvis/gemini-key, where the Home
// Assistant token already lives, rather than from config.ini: nothing
// in the server uses it yet, and an experiment should not need a line
// of configuration to run.
func searchers(t *testing.T) []search.Searcher {
	t.Helper()
	out := []search.Searcher{}
	for _, s := range configured(t) {
		out = append(out, s)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	key, err := os.ReadFile(filepath.Join(home, ".config", "jarvis", "gemini-key"))
	if err != nil {
		t.Logf("no gemini key at ~/.config/jarvis/gemini-key, so it is not compared")
		return out
	}
	if g := gemini.New(strings.TrimSpace(string(key)), "", 30*time.Second); g != nil {
		out = append(out, g)
	}
	return out
}

func configured(t *testing.T) []search.Service {
	t.Helper()
	cfg, err := config.Load("../../../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	out := []search.Service{}
	for _, s := range []search.Service{
		tinyfish.New(string(cfg.Search.TinyFish), 30*time.Second),
		tavily.New(string(cfg.Search.Tavily), 30*time.Second),
		firecrawl.New(string(cfg.Search.Firecrawl), 30*time.Second),
	} {
		if s != nil && !nilUnder(s) {
			out = append(out, s)
		}
	}
	return out
}
