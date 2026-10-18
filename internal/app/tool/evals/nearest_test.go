//go:build evals

package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/embed/tei"
)

// TEI : Where the embedding server answers, the same one memory recall
// uses. Overridden with EVAL_TEI.
const TEI = "http://127.0.0.1:8090"

// laptopTool : One of the tools the laptop offers over MCP, as its own
// source declares it.
//
// A fixture rather than a live reading: the laptop's tools reach a real
// request at runtime and there is nothing to ask here. Regenerated from
// personal-assistant-client/src/main/mcp/*.ts when those change; 29 of
// them, 5,839 bytes of description, which is the smaller half of what
// a request carries.
type laptopTool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// written : How much of a tool's own words to rank it by.
//
// The whole description is what the model reads, and it is mostly
// rules: when not to reach for this, what to do when the answer
// disagrees with you, what the first search already told you.
// web_search is 1,900 characters of which the topic -- who somebody is,
// what a thing costs -- is perhaps two lines. A vector averages over
// all of it, so a rulebook buries the subject it is about.
//
// Which is a guess, and this is three ways of making it so it can be
// measured instead.
type written int

const (
	// whole : Purpose, UseWhen and Avoid, as the model is shown them.
	whole written = iota
	// saying : The one line of what it does, and nothing else.
	saying
	// reaching : What it does and when to reach for it, without the
	// warnings against reaching for it.
	reaching
	// questioned : What it does, and the questions it is for -- the Ask of
	// each worked example, without the arguments.
	//
	// Written to fit. bge-base reads about 512 tokens and web_search's
	// description is 3,999 characters, so its examples -- which contain
	// "what time does Phoenix Mall close" and "is it going to rain
	// tomorrow", two of the questions it fails on -- are cut off before
	// the embedder ever sees them. Seven of the eighty-eight tools are
	// past that line.
	questioned
)

func (w written) String() string {
	return [...]string{"purpose + usewhen + avoid", "purpose only", "purpose + usewhen", "purpose + the questions it is for"}[w]
}

// reachable : Every tool that reaches a real request, server and laptop
// together, as the text a ranking would be given about each.
func reachable(t *testing.T, how written) (names []string, text []string) {
	t.Helper()
	_, _, _ = harness(t) // skips the suite where there is no config

	for _, x := range registered(t).All() {
		say := strings.TrimSpace(x.Purpose)
		switch how {
		case whole:
			say = x.Description()
		case reaching:
			say = strings.TrimSpace(x.Purpose + " " + x.UseWhen)
		case questioned:
			qs := make([]string, 0, len(x.Examples))
			for _, e := range x.Examples {
				qs = append(qs, e.Ask)
			}
			say = strings.TrimSpace(x.Purpose + " " + strings.Join(qs, ". "))
		}
		names = append(names, x.Name)
		text = append(text, strings.TrimSpace(x.Name+". "+say))
	}

	raw, err := os.ReadFile("laptop_tools.json")
	if err != nil {
		t.Fatalf("the laptop's tools: %v", err)
	}
	var laptop []laptopTool
	if err := json.Unmarshal(raw, &laptop); err != nil {
		t.Fatalf("the laptop's tools: %v", err)
	}
	for _, x := range laptop {
		// The laptop declares one string and no parts, so its tools are
		// the same text whichever way the server's are cut.
		names = append(names, x.Name)
		text = append(text, strings.TrimSpace(x.Name+". "+x.Title+". "+x.Description))
	}
	return names, text
}

// wanted : The tools a case will accept, as a set.
func wanted(c eval) map[string]bool {
	set := make(map[string]bool, len(c.Tool))
	for _, n := range c.Tool {
		set[n] = true
	}
	return set
}

// TestWhetherTheRightToolIsNearTheQuestion : Whether choosing which
// tools to describe by what the question means would beat the
// hand-written list of twenty-five.
//
// No gateway and no model that answers: the embedding server and
// arithmetic. Every case in the suite already names the tool a right
// first move would reach for, so the question is only whether that tool
// sits in the nearest N of all fifty.
//
// What it must beat is not zero. It is `hot`, which is what reaches a
// request today, and which gets the right tool in front of the model on
// every case where the answer is one of its twenty-five.
func TestWhetherTheRightToolIsNearTheQuestion(t *testing.T) {
	for _, how := range []written{whole, saying, reaching, questioned} {
		rank(t, how)
	}
}

// rank : One way of writing a tool down, scored.
func rank(t *testing.T, how written) {
	names, text := reachable(t, how)
	t.Logf("%d tools reach a request, ranked by %s", len(names), how)

	where := TEI
	if u := os.Getenv("EVAL_TEI"); u != "" {
		where = u
	}
	e := tei.New(tei.Config{URL: where, Timeout: 30 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// In batches, because the server refuses more than thirty-two at a
	// time and seventy-seven tools reach a request.
	began := time.Now()
	var vectors []embed.Vector
	for i := 0; i < len(text); i += 32 {
		end := min(i+32, len(text))
		part, err := e.Documents(ctx, text[i:end])
		if err != nil {
			t.Skipf("no embedding server at %s: %v", where, err)
		}
		vectors = append(vectors, part...)
	}
	t.Logf("embedded %d tool descriptions in %v", len(vectors), time.Since(began).Round(time.Millisecond))

	// Only the cases that name a tool. One that must be answered without
	// any has nothing to retrieve and nothing to miss.
	var asked []eval
	for _, c := range cases {
		if len(c.Tool) > 0 {
			asked = append(asked, c)
		}
	}
	t.Logf("%d of %d cases name a tool a right first move would reach for", len(asked), len(cases))

	ns := []int{5, 10, 15, 20, 25, 30}
	found := make(map[int]int, len(ns))
	var misses []string
	var took []time.Duration

	for _, c := range asked {
		at := time.Now()
		q, err := e.Query(ctx, c.Say)
		if err != nil {
			t.Fatalf("embedding %q: %v", c.Say, err)
		}
		took = append(took, time.Since(at))

		type scored struct {
			name string
			sim  float64
		}
		ranked := make([]scored, 0, len(names))
		for i, v := range vectors {
			ranked = append(ranked, scored{names[i], embed.Similarity(q, v)})
		}
		sort.Slice(ranked, func(i, j int) bool { return ranked[i].sim > ranked[j].sim })

		want := wanted(c)
		best := len(names) + 1
		for i, r := range ranked {
			if want[r.name] {
				best = i + 1
				break
			}
		}
		for _, n := range ns {
			if best <= n {
				found[n]++
			}
		}
		if best > ns[len(ns)-1] {
			top := make([]string, 0, 3)
			for _, r := range ranked[:3] {
				top = append(top, r.name)
			}
			misses = append(misses, fmt.Sprintf("%-46s wanted %v, nearest was #%d; top: %s",
				first(c.Say, 46), c.Tool, best, strings.Join(top, ", ")))
		}
	}

	// What the hand-written list gets right on the same cases, which is
	// the thing to beat.
	hot := 0
	for _, c := range asked {
		for n := range wanted(c) {
			if tool.Hot(n) {
				hot++
				break
			}
		}
	}

	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	fmt.Printf("\n  ranked by %s -- the right tool is among the nearest N, over %d cases:\n\n", how, len(asked))
	fmt.Printf("    %6s  %6s  %s\n", "N", "hit", "")
	for _, n := range ns {
		fmt.Printf("    %6d  %5.0f%%  %d of %d\n", n, 100*float64(found[n])/float64(len(asked)), found[n], len(asked))
	}
	fmt.Printf("\n    %6s  %5.0f%%  %d of %d   (the 25 described today, whatever was asked)\n",
		"hot", 100*float64(hot)/float64(len(asked)), hot, len(asked))
	if len(took) > 0 {
		fmt.Printf("\n  embedding one question: median %v\n", took[len(took)/2].Round(time.Millisecond))
	}
	if len(misses) > 0 && how == questioned {
		fmt.Printf("\n  outside the nearest %d (%d):\n", ns[len(ns)-1], len(misses))
		for _, m := range misses {
			fmt.Printf("    %s\n", m)
		}
	}
	fmt.Println()
}
