package pointing

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"personal-assistant/internal/base/world/heard"
)

// Names : One source of names the server already holds.
//
// Narrow on purpose: this package needs the words and nothing else, so
// the stores stay where they are and the wiring decides which to use.
type Names interface {
	// Named : Everything this source calls something, in any order.
	Named(ctx context.Context, userID string) ([]string, error)
}

// NamesFunc : A Names made from a function.
type NamesFunc func(ctx context.Context, userID string) ([]string, error)

// Named : Calls the function.
func (f NamesFunc) Named(ctx context.Context, userID string) ([]string, error) {
	return f(ctx, userID)
}

// Fresh : How long a read of the sources is reused.
//
// The nightly list changes once a day and a place is kept by hand, so
// an hour is well inside how fast any of it moves. Read every time, this
// would put three queries in front of the first word of every answer.
const Fresh = time.Hour

// Record : What the server already holds, as a lookup.
//
// Safe for concurrent use. A read that fails leaves the previous answer
// standing rather than emptying it: a name wrongly called worldly is
// looked for on the web, which is the wrong place for his own life.
type Record struct {
	// Sources : Where the names are read from. Any number.
	Sources []Names
	// Fresh : How long a read is reused. Zero selects [Fresh].
	Fresh time.Duration
	// Now : The clock, for tests. Zero value uses time.Now.
	Now func() time.Time

	mu    sync.Mutex
	held  map[string]map[string]string // userID -> lowercase -> as spelt
	taken map[string]time.Time
}

// Spelling : How the record spells this name, and whether it holds it.
//
// Matched by likeness, not letter for letter, because what was said has
// been through speech recognition and the record has not: the phone
// reported "Alekhya" off the contact and the microphone heard "alekya".
//
// Stricter than [heard.Best], which the tools use to match a title the
// person knows exists. That accepts one name anywhere inside another,
// and [heard.Normalise] has already thrown the spaces away, so "Tada"
// matched "Alekhya Chintada" and sent a question about a town to his
// contacts. Here a name may only begin another -- "realme" for "realme
// Buds Air8" -- which keeps every case that was working.
//
// Two names equally near is no match: that is a guess between two of his
// people, and guessing wrong is worse than asking the web.
func (r *Record) Spelling(ctx context.Context, said string) (string, bool) {
	want := heard.Normalise(said)
	if want == "" {
		return "", false
	}
	held := r.names(ctx, userFrom(ctx))
	if len(held) == 0 {
		return "", false
	}

	names := make([]string, 0, len(held))
	for _, spelt := range held {
		names = append(names, spelt)
	}
	sort.Strings(names) // so an ambiguous pair is refused the same way twice

	for _, name := range names {
		if heard.Normalise(name) == want {
			return name, true
		}
	}
	for _, name := range names {
		if begins(heard.Normalise(name), want) {
			return name, true
		}
	}

	best, top, next := "", 0.0, 0.0
	for _, name := range names {
		switch score := heard.Likeness(want, heard.Normalise(name)); {
		case score > top:
			next, top, best = top, score, name
		case score > next:
			next = score
		}
	}
	if top < heard.Alike || top-next < heard.Clearer {
		return "", false
	}
	return best, true
}

// begins : Whether either of two normalised names starts the other, with
// both long enough that a syllable cannot do it.
func begins(a, b string) bool {
	if len(a) < heard.Shortest || len(b) < heard.Shortest {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// Hold : How many names are held for somebody, after a read. For logs
// and for the test that the sources reached it.
func (r *Record) Hold(ctx context.Context, userID string) int {
	return len(r.names(withUser(ctx, userID), userID))
}

// names : The held list for somebody, read again when it is stale.
func (r *Record) names(ctx context.Context, userID string) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.held == nil {
		r.held, r.taken = map[string]map[string]string{}, map[string]time.Time{}
	}
	fresh := r.Fresh
	if fresh <= 0 {
		fresh = Fresh
	}
	if at, ok := r.taken[userID]; ok && r.now().Sub(at) < fresh {
		return r.held[userID]
	}

	got := map[string]string{}
	for _, source := range r.Sources {
		named, err := source.Named(ctx, userID)
		if err != nil {
			// Keep what is held. An empty list would send his own
			// people to the web.
			return r.held[userID]
		}
		for _, name := range named {
			for _, part := range strings.Split(name, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				key := strings.ToLower(part)
				if _, seen := got[key]; !seen {
					got[key] = part
				}
			}
		}
	}
	r.held[userID], r.taken[userID] = got, r.now()
	return got
}

func (r *Record) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// userKey : How the person being answered for travels to Spelling,
// which the [Known] interface gives no room to pass.
type userKey struct{}

// WithUser : Marks a context as being about one person, so a [Record]
// reached through [Known] knows whose names to hold.
func WithUser(ctx context.Context, userID string) context.Context {
	return withUser(ctx, userID)
}

func withUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userKey{}, userID)
}

func userFrom(ctx context.Context) string {
	id, _ := ctx.Value(userKey{}).(string)
	return id
}
