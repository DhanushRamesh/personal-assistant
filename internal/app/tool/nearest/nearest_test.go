package nearest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/nearest"
	"personal-assistant/internal/base/thinking/embed"
)

// words : An embedder that scores by how many words two texts share.
//
// Not a model, and it does not need to be: what is under test is which
// names come back and what happens when none can, not whether a vector
// is any good at meaning.
type words struct{ broken error }

func (words) Model() string { return "words" }

func (w words) Available() bool { return w.broken == nil }

func (w words) Documents(_ context.Context, texts []string) ([]embed.Vector, error) {
	if w.broken != nil {
		return nil, w.broken
	}
	out := make([]embed.Vector, 0, len(texts))
	for _, t := range texts {
		out = append(out, vector(t))
	}
	return out, nil
}

func (w words) Query(_ context.Context, text string) (embed.Vector, error) {
	if w.broken != nil {
		return nil, w.broken
	}
	return vector(text), nil
}

// vector : A unit vector over a fixed little vocabulary.
var vocabulary = []string{"diary", "roof", "battery", "terminal", "remember", "tomorrow"}

func vector(text string) embed.Vector {
	v := make(embed.Vector, len(vocabulary))
	low := strings.ToLower(text)
	var any bool
	for i, w := range vocabulary {
		if strings.Contains(low, w) {
			v[i] = 1
			any = true
		}
	}
	if !any {
		v[0] = 0.001
	}
	return embed.Normalize(v)
}

func toolsFor(t *testing.T) []tool.Tool {
	t.Helper()
	return []tool.Tool{
		{Name: "calendar_events", Purpose: "Read the diary.", Examples: []tool.Example{{Ask: "what is on tomorrow"}}},
		{Name: "memory_search", Purpose: "Find what they told you to remember.", Examples: []tool.Example{{Ask: "what did I say about the roof"}}},
		{Name: "event_recent", Purpose: "What their devices reported, the battery for one."},
		{Name: "read_terminal", Purpose: "Read a terminal on the laptop."},
	}
}

// TestItKeepsTheNearestAndEverythingItCannotRank : The second half is
// the safety. A tool it has no vector for is kept rather than hidden,
// so a question is never answered without a tool for a reason nobody
// can see.
func TestItKeepsTheNearestAndEverythingItCannotRank(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	tools := toolsFor(t)
	if _, err := p.Warm(context.Background(), tools[:2]); err != nil {
		t.Fatal(err)
	}

	keep, ok := p.Keep(context.Background(), "what did I say about the roof", tools, 1)
	if !ok {
		t.Fatal("it could not rank at all")
	}
	if !keep["memory_search"] {
		t.Errorf("the nearest was not kept: %v", keep)
	}
	for _, name := range []string{"event_recent", "read_terminal"} {
		if !keep[name] {
			t.Errorf("%s has no vector and was hidden anyway: %v", name, keep)
		}
	}
	if keep["calendar_events"] {
		t.Errorf("a ranked tool that was not near was kept: %v", keep)
	}
}

// TestAColdPickerOffersWhatTheServerWould : Before it knows anything,
// it must not be the reason a turn loses its tools.
func TestAColdPickerOffersWhatTheServerWould(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	if _, ok := p.Keep(context.Background(), "anything", toolsFor(t), 2); ok {
		t.Error("a picker holding no vectors claimed to have chosen")
	}
	var none *nearest.Picker
	if _, ok := none.Keep(context.Background(), "anything", toolsFor(t), 2); ok {
		t.Error("no picker at all claimed to have chosen")
	}
}

// TestAnEmbedderThatHasGoneAwayCostsNothing : The server has run
// without one before, and the container has been down for days at a
// time. A turn must lose a fraction of a second, not its tools.
func TestAnEmbedderThatHasGoneAwayCostsNothing(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	if _, err := p.Warm(context.Background(), toolsFor(t)); err != nil {
		t.Fatal(err)
	}
	p.Embed = words{broken: errors.New("no embedding server")}
	if _, ok := p.Keep(context.Background(), "the roof", toolsFor(t), 1); ok {
		t.Error("a broken embedder still claimed to have chosen")
	}
}

// TestWarmingIsDoneOnce : The same tool embedded twice is a call to a
// server for an answer already held.
func TestWarmingIsDoneOnce(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	tools := toolsFor(t)
	first, err := p.Warm(context.Background(), tools)
	if err != nil || first != len(tools) {
		t.Fatalf("warmed %d of %d: %v", first, len(tools), err)
	}
	again, err := p.Warm(context.Background(), tools)
	if err != nil || again != 0 {
		t.Errorf("warmed %d again: %v", again, err)
	}
}

// TestAToolIsRankedByItsQuestionsAndNotItsRules : web_search is 3,999
// characters against an embedder that reads about 512 tokens, and what
// is cut off is the examples -- the questions it is for.
func TestAToolIsRankedByItsQuestionsAndNotItsRules(t *testing.T) {
	got := nearest.Text(tool.Tool{
		Name: "web_search", Purpose: "Look something up on the web.",
		UseWhen:  "A question whose answer lives in the world.",
		Avoid:    "Not for anything about them.",
		Examples: []tool.Example{{Ask: "how much is a litre of petrol", Args: `{"query":"petrol price"}`}},
	})
	if !strings.Contains(got, "how much is a litre of petrol") {
		t.Errorf("the question it is for is missing: %q", got)
	}
	if strings.Contains(got, "Not for anything about them") {
		t.Errorf("the warnings against reaching for it were included: %q", got)
	}
	if strings.Contains(got, `{"query"`) {
		t.Errorf("an example's arguments were included: %q", got)
	}
}
