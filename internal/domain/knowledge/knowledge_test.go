package knowledge_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/domain/knowledge"
)

// kept : The store in memory.
type kept struct {
	rows   map[string][]knowledge.Piece
	vecs   map[string][]embed.Vector
	hashes map[string]string
	puts   int
}

func newKept() *kept {
	return &kept{rows: map[string][]knowledge.Piece{}, vecs: map[string][]embed.Vector{}, hashes: map[string]string{}}
}
func key(u string, s knowledge.Source, r string) string { return u + "|" + string(s) + "|" + r }
func (k *kept) Hash(_ context.Context, u string, s knowledge.Source, r, _ string) (string, error) {
	return k.hashes[key(u, s, r)], nil
}
func (k *kept) Put(_ context.Context, u string, s knowledge.Source, r, _, h string, at time.Time, chunks []string, v []embed.Vector) error {
	k.puts++
	var ps []knowledge.Piece
	for i, c := range chunks {
		ps = append(ps, knowledge.Piece{Source: s, Ref: r, Chunk: i, Text: c, At: at})
	}
	k.rows[key(u, s, r)], k.vecs[key(u, s, r)], k.hashes[key(u, s, r)] = ps, v, h
	return nil
}
func (k *kept) Remove(_ context.Context, u string, s knowledge.Source, r string) error {
	delete(k.rows, key(u, s, r))
	delete(k.vecs, key(u, s, r))
	delete(k.hashes, key(u, s, r))
	return nil
}
func (k *kept) Refs(_ context.Context, u string, s knowledge.Source) ([]string, error) {
	var out []string
	for kk := range k.rows {
		if p := strings.SplitN(kk, "|", 3); p[0] == u && p[1] == string(s) {
			out = append(out, p[2])
		}
	}
	return out, nil
}
func (k *kept) NearestTo(_ context.Context, u string, q embed.Vector, _ string, limit int) ([]knowledge.Found, error) {
	var out []knowledge.Found
	for kk, ps := range k.rows {
		if !strings.HasPrefix(kk, u+"|") {
			continue
		}
		for i, p := range ps {
			out = append(out, knowledge.Found{Piece: p, Score: embed.Similarity(q, k.vecs[kk][i])})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func known(store *kept) *knowledge.Known {
	return &knowledge.Known{Store: store, Embed: embed.Fake{}}
}

// A source is kept in chunks, found by what it says, left alone when its
// text has not changed, and forgotten when it is empty.
func TestASourceIsKeptFoundAndForgotten(t *testing.T) {
	store := newKept()
	k := known(store)
	ctx := context.Background()
	if did, err := k.Keep(ctx, "u", knowledge.Memory, "m1", "They like their commits small and pushed only when they say."); err != nil || !did {
		t.Fatalf("kept %v, %v", did, err)
	}
	if did, _ := k.Keep(ctx, "u", knowledge.Memory, "m1", "They like their commits small and pushed only when they say."); did || store.puts != 1 {
		t.Errorf("unchanged text was embedded again")
	}
	_, _ = k.Keep(ctx, "u", knowledge.Notes, "space_1", "# Today\n[ ] ship the glance")
	found, err := k.Find(ctx, "u", "They like their commits small and pushed only when they say.", 5, 0.9)
	if err != nil || len(found) != 1 || found[0].Ref != "m1" {
		t.Fatalf("found %+v, %v", found, err)
	}
	if got := knowledge.Told(found); got != "[memory: m1]\nThey like their commits small and pushed only when they say." {
		t.Errorf("told %q", got)
	}
	_, _ = k.Keep(ctx, "u", knowledge.Memory, "m1", "  ")
	if found, _ := k.Find(ctx, "u", "commits", 5, -1); len(found) != 1 || found[0].Ref != "space_1" {
		t.Errorf("after forgetting, found %+v", found)
	}
	if _, err := k.Keep(ctx, "u", knowledge.Source("diary"), "x", "y"); err == nil {
		t.Error("an unknown source was kept")
	}
}

// Text is cut where paragraphs end, a long one where sentences do.
func TestChunks(t *testing.T) {
	got := knowledge.Chunks("one.\n\ntwo.\n\n"+strings.Repeat("A sentence here. ", 10), 60)
	if got[0] != "one.\n\ntwo." {
		t.Errorf("first chunk %q", got[0])
	}
	for _, c := range got {
		if len(c) > 60 || strings.TrimSpace(c) == "" {
			t.Errorf("chunk %q", c)
		}
	}
	if len(knowledge.Chunks("  ", 60)) != 0 {
		t.Error("nothing was chunked into something")
	}
}
