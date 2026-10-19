//go:build evals

// Package evals replays real spans through the routing, so a change to
// the word lists or to how a name is matched is measured rather than
// argued about.
package evals

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"personal-assistant/internal/app/pointing"
	"personal-assistant/internal/base/thinking/naming"
)

// Assistant : The name used while these were recorded.
const Assistant = "Jarvis"

type recorded struct {
	Note  string   `json:"note"`
	Worth []string `json:"worth"`
	Asked []struct {
		Said  string `json:"said"`
		Spans []struct {
			Text  string  `json:"text"`
			Label string  `json:"label"`
			Score float64 `json:"score"`
		} `json:"spans"`
	} `json:"asked"`
}

// fixed : A finder that replays what GLiNER found, so the measurement
// needs no service and never moves under a change to the lists.
type fixed []naming.Span

func (f fixed) Names(context.Context, string, []string) ([]naming.Span, error) {
	return []naming.Span(f), nil
}
func (f fixed) Available() bool { return true }

func load(t *testing.T) (recorded, pointing.Known) {
	t.Helper()
	raw, err := os.ReadFile("spans.json")
	if err != nil {
		t.Fatalf("spans.json: %v", err)
	}
	var got recorded
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("spans.json: %v", err)
	}

	names, err := os.ReadFile("known.txt")
	if err != nil {
		t.Fatalf("known.txt: %v", err)
	}
	var held []string
	for _, line := range strings.Split(string(names), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			held = append(held, line)
		}
	}
	record := &pointing.Record{Sources: []pointing.Names{
		pointing.NamesFunc(func(context.Context, string) ([]string, error) { return held, nil }),
	}}
	return got, record
}

// TestTheRoutingHoldsItsMeasuredNumbers : Every span that is worth a
// lookup is kept, and most of what is kept is worth one.
//
// Recall is the number that matters. A name wrongly dropped is a lookup
// that never happens; a name wrongly kept is one query that finds
// nothing. So recall must stay at 100% and precision is reported.
func TestTheRoutingHoldsItsMeasuredNumbers(t *testing.T) {
	got, record := load(t)

	worth := map[string]bool{}
	for _, w := range got.Worth {
		worth[strings.ToLower(w)] = true
	}

	ctx := pointing.WithUser(context.Background(), "usr_evals")
	var kept, keptWorth, total, kings int
	var dropped, wronglyKept []string
	his := map[string]bool{}

	for _, ask := range got.Asked {
		spans := make(fixed, 0, len(ask.Spans))
		for _, s := range ask.Spans {
			spans = append(spans, naming.Span{Text: s.Text, Label: s.Label, Score: s.Score})
		}
		p := &pointing.Pointer{Finder: spans, Known: record, Assistant: Assistant}
		points, err := p.Points(ctx, ask.Said)
		if err != nil {
			t.Fatalf("Points(%q): %v", ask.Said, err)
		}
		for _, point := range points {
			total++
			want := worth[strings.ToLower(point.Said)]
			if want {
				kings++
			}
			if point.Where == pointing.Nowhere {
				if want {
					dropped = append(dropped, point.Said)
				}
				continue
			}
			kept++
			if want {
				keptWorth++
			} else {
				wronglyKept = append(wronglyKept, point.Said)
			}
			if point.Where == pointing.His {
				his[point.Term()] = true
			}
		}
	}

	t.Logf("spans %d, worth a lookup %d", total, kings)
	t.Logf("kept %d, of which worth a lookup %d", kept, keptWorth)
	t.Logf("precision %d%%, recall %d%%", 100*keptWorth/kept, 100*keptWorth/kings)
	t.Logf("wrongly kept: %v", wronglyKept)
	t.Logf("sent to his own record: %d -- %v", len(his), keys(his))

	if len(dropped) != 0 {
		t.Errorf("dropped %d spans that were worth a lookup: %v", len(dropped), dropped)
	}
	if 100*keptWorth/kept < 80 {
		t.Errorf("precision fell to %d%%, was 85%%", 100*keptWorth/kept)
	}
}

// TestNothingWorldlyIsCalledHis : Matching by likeness must not hand a
// name the devices never reported to his own record, which would answer
// about the wrong thing entirely.
func TestNothingWorldlyIsCalledHis(t *testing.T) {
	got, record := load(t)

	// Named by his devices or his own conversation, so they belong to
	// him. The rest are the world's: a city, a piece of software, a
	// service.
	//
	// "Jaylen" is here because it is not a name at all: it is "Jailer"
	// misheard, in "Is Jaylen will be releasing tomorrow?", and the
	// record holds Jailer because he talks about the film. Matching it
	// by likeness is the correction working, not a wild match -- it
	// scores 0.667 against a bar of 0.6, and nothing else in the whole
	// set comes near: Spotify to Spectra is 0.286, Chennai to Chintada
	// 0.500, Dravid to Dhanush 0.286.
	ofHis := map[string]bool{"deepika": true, "chintada": true, "matron": true,
		"jailer": true, "jaylen": true, "home": true, "office": true, "so stone": true}

	ctx := pointing.WithUser(context.Background(), "usr_evals")
	for _, ask := range got.Asked {
		spans := make(fixed, 0, len(ask.Spans))
		for _, s := range ask.Spans {
			spans = append(spans, naming.Span{Text: s.Text, Label: s.Label, Score: s.Score})
		}
		p := &pointing.Pointer{Finder: spans, Known: record, Assistant: Assistant}
		points, _ := p.Points(ctx, ask.Said)
		for _, point := range points {
			if point.Where != pointing.His {
				continue
			}
			if !ofHis[strings.ToLower(point.Said)] {
				t.Errorf("%q was sent to his own record as %q, and his devices have never reported it",
					point.Said, point.Term())
			}
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
