//go:build evals

package evals

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/embed/tei"
)

// TestWhereAQuestionRanksTheTools : The order for one question, so a
// wrong tool chosen in the room can be read back here.
//
//	make evals-nearest ARGS='-run TestWhereAQuestionRanks' ASK='from and to?'
//
// Written for one answer. He asked "from and to?" of a phone call and
// was read his diary, and "from" and "to" are the diary tool's own
// argument names.
func TestWhereAQuestionRanksTheTools(t *testing.T) {
	ask := os.Getenv("ASK")
	if ask == "" {
		ask = "from and to?"
	}

	where := TEI
	if u := os.Getenv("EVAL_TEI"); u != "" {
		where = u
	}
	e := tei.New(tei.Config{URL: where, Timeout: 30 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for _, how := range []written{withArguments, questioned} {
		names, text := reachable(t, how)

		var vectors []embed.Vector
		for i := 0; i < len(text); i += 32 {
			end := min(i+32, len(text))
			part, err := e.Documents(ctx, text[i:end])
			if err != nil {
				t.Skipf("no embedding server at %s: %v", where, err)
			}
			vectors = append(vectors, part...)
		}

		q, err := e.Query(ctx, ask)
		if err != nil {
			t.Skipf("no embedding server: %v", err)
		}

		type scored struct {
			name  string
			score float64
		}
		all := make([]scored, 0, len(names))
		for i := range names {
			all = append(all, scored{names[i], embed.Similarity(q, vectors[i])})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })

		t.Logf("\n%q ranked by %s, nearest first:", ask, how)
		for i := 0; i < 8 && i < len(all); i++ {
			t.Logf("  %2d  %.3f  %s", i+1, all[i].score, all[i].name)
		}
		for i, s := range all {
			if s.name == "event_recent" {
				t.Logf("  ... event_recent, the one it wanted, is %d of %d at %.3f", i+1, len(all), s.score)
				break
			}
		}
	}
}
