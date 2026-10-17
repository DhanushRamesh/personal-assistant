// Package inmemory : The knowledge store in memory, for tests.
package inmemory

import (
	"context"
	"sort"
	"sync"
	"time"

	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/domain/knowledge"
)

type key struct {
	user   string
	source knowledge.Source
	ref    string
}

type kept struct {
	hash    string
	pieces  []knowledge.Piece
	vectors []embed.Vector
}

// Store : What is known, in memory.
type Store struct {
	mu   sync.Mutex
	all  map[key]kept
	Puts int
}

// New : An empty store.
func New() *Store { return &Store{all: map[key]kept{}} }

// Hash : See knowledge.Store.
func (s *Store) Hash(_ context.Context, u string, src knowledge.Source, ref, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.all[key{u, src, ref}].hash, nil
}

// Put : See knowledge.Store.
func (s *Store) Put(_ context.Context, u string, src knowledge.Source, ref, _, hash string, at time.Time, chunks []string, vectors []embed.Vector) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Puts++
	k := kept{hash: hash, vectors: vectors}
	for i, c := range chunks {
		k.pieces = append(k.pieces, knowledge.Piece{Source: src, Ref: ref, Chunk: i, Text: c, At: at})
	}
	s.all[key{u, src, ref}] = k
	return nil
}

// Remove : See knowledge.Store.
func (s *Store) Remove(_ context.Context, u string, src knowledge.Source, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.all, key{u, src, ref})
	return nil
}

// Refs : See knowledge.Store.
func (s *Store) Refs(_ context.Context, u string, src knowledge.Source) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.all {
		if k.user == u && k.source == src {
			out = append(out, k.ref)
		}
	}
	sort.Strings(out)
	return out, nil
}

// NearestTo : See knowledge.Store.
func (s *Store) NearestTo(_ context.Context, u string, q embed.Vector, _ string, limit int) ([]knowledge.Found, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []knowledge.Found
	for k, v := range s.all {
		if k.user != u {
			continue
		}
		for i, p := range v.pieces {
			out = append(out, knowledge.Found{Piece: p, Score: embed.Similarity(q, v.vectors[i])})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var _ knowledge.Store = (*Store)(nil)
