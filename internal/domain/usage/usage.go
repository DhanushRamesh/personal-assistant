// Package usage : Every call made to a model, with what it cost.
//
// Owner, 17 October 2026: *"whatever AI call is made .. we will be
// getting input output token usage .. mark it and show in settings
// page"*. The environment records each attempt where every call
// passes; this keeps them and reads them back as totals, as groups and
// as a list. Tokens only: no prices are kept.
package usage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"personal-assistant/internal/base/thinking/environment"
)

// Prefix : What a kept call's identifier starts with.
const Prefix = "mcl_"

// NewID : A new identifier for a kept call.
func NewID() string { return Prefix + ulid.Make().String() }

// Keep : How long a call is kept.
//
// Ninety days: long enough to see a month against the month before
// it, and a few thousand rows a day is nothing to carry that long.
const Keep = 90 * 24 * time.Hour

// Call : One kept call.
type Call struct {
	// ID : Its identifier, a Prefix and a ULID.
	ID string
	environment.Call
}

// Kind : Which calls a reading is about.
type Kind string

const (
	// KindAll : Every call.
	KindAll Kind = "all"
	// KindChat : The calls made while answering somebody, and the
	// housekeeping that follows from a chat directly: naming and
	// condensing its conversation.
	KindChat Kind = "chat"
	// KindBackground : Everything else -- what the server asks of a
	// model on its own account.
	KindBackground Kind = "background"
	// KindFailed : The calls that failed, whoever made them.
	KindFailed Kind = "failed"
)

// Kinds : Every kind, in the order a filter shows them.
func Kinds() []Kind { return []Kind{KindAll, KindChat, KindBackground, KindFailed} }

// Valid : Whether k is one of Kinds.
func (k Kind) Valid() bool {
	for _, known := range Kinds() {
		if k == known {
			return true
		}
	}
	return false
}

// chatFeatures : The features that count as chat. Everything else is
// background.
var chatFeatures = []environment.Feature{
	environment.FeatureChat, environment.FeatureTitle, environment.FeatureCondense,
}

// ChatFeatures : The features counted as chat, for a store to filter by.
func ChatFeatures() []string {
	out := make([]string, 0, len(chatFeatures))
	for _, f := range chatFeatures {
		out = append(out, string(f))
	}
	return out
}

// IsChat : Whether a feature counts as chat.
func IsChat(f environment.Feature) bool {
	for _, c := range chatFeatures {
		if f == c {
			return true
		}
	}
	return false
}

// Matches : Whether c is one of the calls of kind k.
func (k Kind) Matches(c Call) bool {
	switch k {
	case KindChat:
		return IsChat(c.Feature)
	case KindBackground:
		return !IsChat(c.Feature)
	case KindFailed:
		return c.Ended == environment.EndedFailed
	default:
		return true
	}
}

// Totals : What a window of calls added up to.
type Totals struct {
	Calls  int
	Input  int
	Output int
	// Chat, Background : How many of Calls were each.
	Chat       int
	Background int
	// Failed : How many of Calls failed.
	Failed int
}

// Add : Counts one more call in.
func (t *Totals) Add(c Call) {
	t.Calls++
	t.Input += c.Input
	t.Output += c.Output
	if IsChat(c.Feature) {
		t.Chat++
	} else {
		t.Background++
	}
	if c.Ended == environment.EndedFailed {
		t.Failed++
	}
}

// ByFeature : A window's calls from one feature.
type ByFeature struct {
	Feature environment.Feature
	Calls   int
	Input   int
	Output  int
}

// ByModel : A window's calls to one model.
type ByModel struct {
	Vendor string
	Model  string
	Calls  int
	Input  int
	Output int
}

// Query : Which calls to list.
type Query struct {
	UserID string
	// Since : The earliest moment, inclusive.
	Since time.Time
	// Kind : Which of them. Empty is all.
	Kind Kind
	// Limit : How many at most. Out of range is clamped by Clamp.
	Limit int
}

// DefaultLimit, MaxLimit : How many calls a listing returns unless
// asked, and the most it will.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Clamp : A limit as a listing will honour it.
func Clamp(limit int) int {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	}
	return limit
}

// Store : Where calls are kept.
type Store interface {
	// Add : Keeps a call.
	Add(ctx context.Context, c Call) error
	// Totals : What somebody's calls since a moment added up to.
	Totals(ctx context.Context, userID string, since time.Time) (Totals, error)
	// ByFeature : Their calls since a moment, by feature, the most
	// input first.
	ByFeature(ctx context.Context, userID string, since time.Time) ([]ByFeature, error)
	// ByModel : The same by model.
	ByModel(ctx context.Context, userID string, since time.Time) ([]ByModel, error)
	// List : Their calls, newest first.
	List(ctx context.Context, q Query) ([]Call, error)
	// Every : All their calls since a moment, oldest first, with no
	// limit -- what the usage page's charts are worked out from.
	Every(ctx context.Context, userID string, since time.Time) ([]Call, error)
	// Forget : Drops every call older than a moment, saying how many.
	Forget(ctx context.Context, before time.Time) (int64, error)
}

// Recorder : Keeps what the environment records, as an
// environment.Recorder.
//
// A failure to keep one is logged and not returned: the call it is
// about has happened, and its answer is not to be lost over the
// receipt.
type Recorder struct {
	Store  Store
	Logger *slog.Logger
}

// Record : Keeps one call.
func (r Recorder) Record(ctx context.Context, c environment.Call) {
	if r.Store == nil {
		return
	}
	if err := r.Store.Add(ctx, Call{ID: NewID(), Call: c}); err != nil && r.Logger != nil {
		r.Logger.WarnContext(ctx, "cannot keep what a model call cost",
			slog.String("feature", string(c.Feature)), slog.Any("error", err))
	}
}

var _ environment.Recorder = Recorder{}

// Throttled : f, said at most once every [every] for each person.
//
// A chat is several calls in a few seconds and a screen showing them
// need not read them again for each one. The last of a burst is never
// lost: a call inside the quiet spell is said when it ends.
func Throttled(every time.Duration, f func(userID string)) func(userID string) {
	if f == nil {
		return nil
	}
	var (
		mu      sync.Mutex
		last    = map[string]time.Time{}
		pending = map[string]bool{}
	)
	return func(userID string) {
		mu.Lock()
		if pending[userID] {
			mu.Unlock()
			return
		}
		wait := every - time.Since(last[userID])
		if wait <= 0 {
			last[userID] = time.Now()
			mu.Unlock()
			f(userID)
			return
		}
		pending[userID] = true
		mu.Unlock()
		time.AfterFunc(wait, func() {
			mu.Lock()
			pending[userID] = false
			last[userID] = time.Now()
			mu.Unlock()
			f(userID)
		})
	}
}
