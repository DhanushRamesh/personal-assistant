// Package inmemory keeps events in memory, for tests and for a server
// running without a database behind it.
package inmemory

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/base/world/event"
)

// Store : Events, in memory.
type Store struct {
	mu sync.Mutex
	// byUser : Everything stored, in the order it was stored.
	byUser map[string][]*event.Event
	// keys : Which deduplication keys each person has already sent.
	keys map[string]map[string]bool
}

// New : An empty store.
func New() *Store {
	return &Store{
		byUser: make(map[string][]*event.Event),
		keys:   make(map[string]map[string]bool),
	}
}

// Record : Stores what is not stored already, and says which keys were
// already known.
func (s *Store) Record(_ context.Context, userID string, events []*event.Event) (stored, seen []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.keys[userID] == nil {
		s.keys[userID] = make(map[string]bool)
	}
	for _, e := range events {
		if s.keys[userID][e.DedupeKey] {
			seen = append(seen, e.DedupeKey)
			continue
		}
		s.keys[userID][e.DedupeKey] = true
		s.byUser[userID] = append(s.byUser[userID], e)
		stored = append(stored, e.DedupeKey)
	}
	return stored, seen, nil
}

// All : Everything stored for one person, in the order it was stored.
func (s *Store) All(userID string) []*event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*event.Event(nil), s.byUser[userID]...)
}

// Recent : What happened, newest first by when it happened.
func (s *Store) Recent(_ context.Context, userID string, q event.Query) ([]event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []event.Event
	for _, e := range s.byUser[userID] {
		switch {
		case !q.Since.IsZero() && e.OccurredAt.Before(q.Since):
			continue
		case !q.Until.IsZero() && !e.OccurredAt.Before(q.Until):
			continue
		case q.Kind != "" && e.Kind != q.Kind:
			continue
		case len(q.Kinds) > 0 && !slices.Contains(q.Kinds, e.Kind):
			continue
		case len(q.From) > 0 && !slices.Contains(q.From, e.Source) && !slices.Contains(q.From, e.Source+"/"+e.Device):
			continue
		case q.Late > 0 && e.ReceivedAt.Sub(e.OccurredAt) <= q.Late:
			continue
		case q.Prefix != "" && !strings.HasPrefix(e.Kind, q.Prefix):
			continue
		case q.Contains != "" && !strings.Contains(
			strings.ToLower(string(e.Payload)), strings.ToLower(q.Contains)):
			continue
		case slices.Contains(q.Omit, e.Kind):
			continue
		case slices.Contains(q.NotFrom, e.Source):
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// Amend : Rewrites what an event already recorded says, matched on its
// dedupe key.
func (s *Store) Amend(_ context.Context, userID string, events []*event.Event) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	by := make(map[string]*event.Event, len(events))
	for _, e := range events {
		if e != nil && e.DedupeKey != "" {
			by[e.DedupeKey] = e
		}
	}
	var changed int64
	for _, held := range s.byUser[userID] {
		if e, ok := by[held.DedupeKey]; ok {
			held.Payload = e.Payload
			held.ReceivedAt = e.ReceivedAt
			changed++
		}
	}
	return changed, nil
}

// Forget : Drops every reading of one kind older than a moment, for
// everybody.
func (s *Store) Forget(_ context.Context, kind string, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var gone int64
	for userID, events := range s.byUser {
		kept := events[:0]
		for _, e := range events {
			if e.Kind == kind && e.OccurredAt.Before(before) {
				gone++
				continue
			}
			kept = append(kept, e)
		}
		s.byUser[userID] = kept
	}
	return gone, nil
}

// Kinds : Which kinds exist, with how many of each.
func (s *Store) Kinds(_ context.Context, userID string) ([]event.Kind, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	by := map[string]*event.Kind{}
	for _, e := range s.byUser[userID] {
		k, ok := by[e.Kind]
		if !ok {
			by[e.Kind] = &event.Kind{
				Kind: e.Kind, Count: 1,
				First: e.OccurredAt, Last: e.OccurredAt,
			}
			continue
		}
		k.Count++
		if e.OccurredAt.Before(k.First) {
			k.First = e.OccurredAt
		}
		if e.OccurredAt.After(k.Last) {
			k.Last = e.OccurredAt
		}
	}

	out := make([]event.Kind, 0, len(by))
	for _, k := range by {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

// Sources : What reported this person's events, each source and device
// with how many, most first.
func (s *Store) Sources(_ context.Context, userID string) ([]event.Source, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	by := map[[2]string]int64{}
	for _, e := range s.byUser[userID] {
		by[[2]string{e.Source, e.Device}]++
	}
	out := make([]event.Source, 0, len(by))
	for k, n := range by {
		out = append(out, event.Source{Source: k[0], Device: k[1], Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Source+out[i].Device < out[j].Source+out[j].Device
	})
	return out, nil
}

// Tally : How many of a kind arrived in the window.
//
// The same answer the database gives, worked out the obvious way,
// so that what a count means is settled here where it can be
// tested rather than in a query nothing exercises.
func (s *Store) Tally(_ context.Context, userID string, t event.Tally) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	per := map[string]int{}
	n := 0
	for _, e := range s.byUser[userID] {
		if e.Kind != t.Kind || e.OccurredAt.Before(t.Since) {
			continue
		}
		had, value := valued(e.Payload)
		if t.Match != "" && (!had || !strings.EqualFold(value, t.Match)) {
			continue
		}
		if !t.Group {
			n++
			continue
		}
		// Nothing to group it by, so it is in no group.
		if !had {
			continue
		}
		per[strings.ToLower(value)]++
	}
	if !t.Group {
		return n, nil
	}
	for _, c := range per {
		n = max(n, c)
	}
	return n, nil
}

// valued : A payload's value field, and whether it had one.
func valued(payload []byte) (bool, string) {
	var into struct {
		Value *string `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil || into.Value == nil {
		return false, ""
	}
	return true, *into.Value
}

// Ledger : Each reporter's each action since a moment, by when they
// were received: how many, first and last, how many carried "unknown"
// or no value, and how many repeated the report before them within
// event.Twice.
func (s *Store) Ledger(_ context.Context, userID string, since time.Time) ([]event.Line, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type key struct{ source, device, kind string }
	group := map[key][]*event.Event{}
	for _, e := range s.byUser[userID] {
		if e.ReceivedAt.Before(since) {
			continue
		}
		k := key{e.Source, e.Device, e.Kind}
		group[k] = append(group[k], e)
	}

	out := make([]event.Line, 0, len(group))
	for k, evs := range group {
		sort.Slice(evs, func(i, j int) bool { return evs[i].OccurredAt.Before(evs[j].OccurredAt) })
		l := event.Line{Source: k.source, Device: k.device, Kind: k.kind, Count: int64(len(evs))}
		got := make([]time.Time, 0, len(evs))
		values := map[string]bool{}
		for i, e := range evs {
			got = append(got, e.ReceivedAt)
			if has, value := anyValue(e.Payload); has {
				values[strings.ToLower(value)] = true
			}
			if l.First.IsZero() || e.ReceivedAt.Before(l.First) {
				l.First = e.ReceivedAt
			}
			if e.ReceivedAt.After(l.Last) {
				l.Last = e.ReceivedAt
			}
			switch has, value := anyValue(e.Payload); {
			case !has:
				l.Valueless++
			case strings.EqualFold(value, event.Unknown):
				l.Unknown++
			}
			if i > 0 {
				prev := evs[i-1]
				if e.OccurredAt.Sub(prev.OccurredAt) < event.Twice && event.Same(e.Payload, prev.Payload) {
					l.Repeats++
				}
			}
		}
		slices.SortFunc(got, func(a, b time.Time) int { return a.Compare(b) })
		for i := 1; i < len(got); i++ {
			l.Longest = max(l.Longest, got[i].Sub(got[i-1]))
		}
		l.Distinct = int64(len(values))
		if len(values) == 1 {
			for v := range values {
				l.Only = v
			}
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Source+a.Device != b.Source+b.Device {
			return a.Source+a.Device < b.Source+b.Device
		}
		return a.Count > b.Count
	})
	return out, nil
}

// Values : How many of each of the given kinds carried each value since a
// moment, per reporter, values folded to one case. Readings with no value
// are left out.
func (s *Store) Values(_ context.Context, userID string, since time.Time, kinds []string) ([]event.Valued, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type key struct{ source, device, kind, value string }
	by := map[key]int64{}
	for _, e := range s.byUser[userID] {
		if e.ReceivedAt.Before(since) || !slices.Contains(kinds, e.Kind) {
			continue
		}
		has, value := anyValue(e.Payload)
		if !has {
			continue
		}
		by[key{e.Source, e.Device, e.Kind, strings.ToLower(value)}]++
	}
	out := make([]event.Valued, 0, len(by))
	for k, n := range by {
		out = append(out, event.Valued{Source: k.source, Device: k.device, Kind: k.kind, Value: k.value, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Source+a.Device+a.Kind != b.Source+b.Device+b.Kind {
			return a.Source+a.Device+a.Kind < b.Source+b.Device+b.Kind
		}
		return a.Value < b.Value
	})
	return out, nil
}

// anyValue : A payload's value field as text, whatever JSON type it was,
// and whether it had one -- as the database reads it with JSON_UNQUOTE.
func anyValue(payload []byte) (bool, string) {
	var into struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(payload, &into); err != nil || len(into.Value) == 0 || string(into.Value) == "null" {
		return false, ""
	}
	var text string
	if json.Unmarshal(into.Value, &text) == nil {
		return true, text
	}
	return true, string(into.Value)
}
