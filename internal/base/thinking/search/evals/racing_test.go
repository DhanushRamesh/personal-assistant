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
	"sort"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/thinking/search"
	"personal-assistant/internal/base/thinking/search/firecrawl"
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
