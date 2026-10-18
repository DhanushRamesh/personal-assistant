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
	if _, err := p.Warm(context.Background(), nearest.Server, tools[:2]); err != nil {
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
	if _, err := p.Warm(context.Background(), nearest.Server, toolsFor(t)); err != nil {
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
	first, err := p.Warm(context.Background(), nearest.Server, tools)
	if err != nil || first != len(tools) {
		t.Fatalf("warmed %d of %d: %v", first, len(tools), err)
	}
	again, err := p.Warm(context.Background(), nearest.Server, tools)
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

// TestTheTwoStoresAreKeptApart : One engine, two stores -- the owner's
// decision of 18 October 2026. The server's tools are fixed at a build
// and embedded once; a device's come and go with the laptop, so they
// must be droppable without dropping the rest.
func TestTheTwoStoresAreKeptApart(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	tools := toolsFor(t)
	if _, err := p.Warm(context.Background(), nearest.Server, tools[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Warm(context.Background(), nearest.Device, tools[2:]); err != nil {
		t.Fatal(err)
	}
	if server, device := p.Held(); server != 2 || device != 2 {
		t.Fatalf("held %d server and %d device, want 2 and 2", server, device)
	}

	// A laptop that went away takes its own vectors and nothing else.
	p.Forget(nearest.Device)
	if server, device := p.Held(); server != 2 || device != 0 {
		t.Errorf("after forgetting the device: %d server and %d device, want 2 and 0", server, device)
	}
	keep, ok := p.Keep(context.Background(), "what did I say about the roof", tools, 1)
	if !ok {
		t.Fatal("the server's own vectors went with the device's")
	}
	if !keep["memory_search"] {
		t.Errorf("the nearest of the server's own was lost: %v", keep)
	}
	// And what it no longer holds is kept rather than hidden.
	if !keep["event_recent"] || !keep["read_terminal"] {
		t.Errorf("a forgotten device tool was hidden rather than kept: %v", keep)
	}
}

// TestADeviceToolIsNotWarmedTwice : Its description is the same every
// chat, so the second chat must cost no calls at all.
func TestADeviceToolIsNotWarmedTwice(t *testing.T) {
	p := &nearest.Picker{Embed: words{}}
	tools := toolsFor(t)
	if _, err := p.Warm(context.Background(), nearest.Device, tools); err != nil {
		t.Fatal(err)
	}
	again, err := p.Warm(context.Background(), nearest.Device, tools)
	if err != nil || again != 0 {
		t.Errorf("warmed %d again: %v", again, err)
	}
}

// TestWhatIsSearchedWithCarriesTheTurnBefore : A bare "yes" is about
// nothing, and a vector of it finds nothing. Measured against the real
// embedder on 18 October 2026: alone it put calendar_events 75th of
// 88, and with the line before it 4th.
func TestWhatIsSearchedWithCarriesTheTurnBefore(t *testing.T) {
	got := nearest.Asking("yes", "Are you sure you want Deepika's birthday, sir?")
	if !strings.Contains(got, "birthday") {
		t.Errorf("the subject was left behind: %q", got)
	}
	if !strings.HasSuffix(got, "yes") {
		t.Errorf("what they said is not at the end: %q", got)
	}

	// A long answer is cut, or it drowns the question.
	long := strings.Repeat("a paragraph of an answer. ", 60)
	if cut := nearest.Asking("yes", long); len(cut) > nearest.Before+10 {
		t.Errorf("a long answer was not cut: %d characters", len(cut))
	}

	// And nothing before it is simply the question.
	if got := nearest.Asking("what is in my diary", ""); got != "what is in my diary" {
		t.Errorf("with nothing before = %q", got)
	}
}

// TestWhatAToolIsRankedOnIncludesItsArguments : Measured over the tool
// suite's 91 questions: with the argument names the right tool is in
// the nearest five on 88% rather than 82%, and the nearest ten on 95%
// rather than 92%.
func TestWhatAToolIsRankedOnIncludesItsArguments(t *testing.T) {
	got := nearest.Text(tool.Tool{
		Name: "calendar_events", Purpose: "Read events.",
		Examples: []tool.Example{{Ask: "what have I got on"}},
		Params: tool.Schema{Properties: map[string]tool.Property{
			"days": {Description: "How many days ahead to read."},
			"from": {Description: "The first day, as a date."},
		}},
	})
	for _, want := range []string{"days", "from", "what have I got on", "Read events"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from %q", want, got)
		}
	}
	// Sorted, since a map's order is not stable and a vector that
	// moves between restarts cannot be cached or compared.
	if strings.Index(got, "days") > strings.Index(got, "from") {
		t.Errorf("the arguments are not in a fixed order: %q", got)
	}
}
