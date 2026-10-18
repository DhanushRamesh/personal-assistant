//go:build evals

package evals

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/embed/tei"
)

// chooser : Picks the tools to describe for one question, by meaning.
//
// Built once and asked per question, which is how the server would use
// it: the tools are embedded at startup and only the question is
// embedded per turn, at about 20ms.
type chooser struct {
	e      embed.Embedder
	names  []string
	vector []embed.Vector
	once   sync.Once
	err    error
}

// ranking : What a tool is ranked by -- what it does, and the questions
// it is for.
//
// Not the whole description. web_search is 3,999 characters against an
// embedder that reads about 512 tokens, so its examples never reach it;
// written this way the right tool is in the nearest twenty on 98% of
// the suite's cases, against 96% for the whole description and 79% for
// the hand-written list of twenty-five.
func ranking(x tool.Tool) string {
	qs := make([]string, 0, len(x.Examples))
	for _, e := range x.Examples {
		qs = append(qs, e.Ask)
	}
	return strings.TrimSpace(x.Name + ". " + x.Purpose + " " + strings.Join(qs, ". "))
}

// newChooser : A chooser over the given tools, embedding none of them
// until it is first asked.
func newChooser(tools []tool.Tool) *chooser {
	where := TEI
	if u := os.Getenv("EVAL_TEI"); u != "" {
		where = u
	}
	c := &chooser{e: tei.New(tei.Config{URL: where, Timeout: 30 * time.Second})}
	for _, x := range tools {
		c.names = append(c.names, x.Name)
	}
	c.readyWith(tools)
	return c
}

// readyWith : Embeds every tool, in batches of thirty-two, which is
// what the server accepts at once.
func (c *chooser) readyWith(tools []tool.Tool) {
	c.once.Do(func() {
		text := make([]string, 0, len(tools))
		for _, x := range tools {
			text = append(text, ranking(x))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		for i := 0; i < len(text); i += 32 {
			end := min(i+32, len(text))
			part, err := c.e.Documents(ctx, text[i:end])
			if err != nil {
				c.err = err
				return
			}
			c.vector = append(c.vector, part...)
		}
	})
}

// nearest : The n tools nearest the question, by name.
func (c *chooser) nearest(ctx context.Context, say string, n int) ([]string, error) {
	if c.err != nil {
		return nil, c.err
	}
	q, err := c.e.Query(ctx, say)
	if err != nil {
		return nil, err
	}
	type scored struct {
		name string
		sim  float64
	}
	all := make([]scored, 0, len(c.names))
	for i, v := range c.vector {
		all = append(all, scored{c.names[i], embed.Similarity(q, v)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	if n > len(all) {
		n = len(all)
	}
	out := make([]string, 0, n)
	for _, s := range all[:n] {
		out = append(out, s.name)
	}
	return out, nil
}

// chosen : Which tools a run describes in full, as EVAL_TOOLS names it.
//
//	all      every tool, which is what this suite did before and is not
//	         what a request carries
//	hot      the twenty-five in the hand-written list, which is what the
//	         server sends today
//	nearest  the EVAL_NEAREST closest to the question, default twenty
func chosen() string {
	if v := os.Getenv("EVAL_TOOLS"); v != "" {
		return v
	}
	return "all"
}

func nearestN() int {
	n := 20
	if v := os.Getenv("EVAL_NEAREST"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &n)
	}
	return n
}

// describing : The tools to describe in full for one question, out of
// those reachable on this channel.
//
// tool_describe is always among them: without it a model given names
// alone has no way to ask, and would be measured on a system nobody
// runs.
func describing(pick *chooser, reachable []tool.Tool, say string) (map[string]bool, error) {
	out := map[string]bool{tool.Describing(nil).Name: true}
	switch chosen() {
	case "hot":
		for _, x := range reachable {
			if tool.Hot(x.Name) {
				out[x.Name] = true
			}
		}
	case "nearest":
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		near, err := pick.nearest(ctx, say, nearestN())
		if err != nil {
			return nil, err
		}
		for _, n := range near {
			out[n] = true
		}
	default:
		for _, x := range reachable {
			out[x.Name] = true
		}
	}
	return out, nil
}
