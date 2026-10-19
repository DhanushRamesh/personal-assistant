// Package usage : What every model call cost, for the client's
// Settings › Usage.
//
// Read-only. Every call is kept where every call passes, in the
// environment; this only adds them up and lists them. Tokens only: no
// prices are kept, since the gateway charges nobody here by the token.
package usage

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/domain/usage"
)

// Titles : Where the conversations' titles are read, all at once.
type Titles interface {
	ConversationTitles(ctx context.Context, userID string, ids []string) (map[string]string, error)
}

// Options : What the handler reads from. Only Store is needed for an
// answer; without it every figure is zero.
type Options struct {
	Logger *slog.Logger
	Store  usage.Store
	// Titles : Optional. Without it no call carries a conversation title.
	Titles Titles
	// Location : The person's zone, for where today began.
	Location *time.Location
	Now      func() time.Time
}

// Handler : Serves /v1/usage.
type Handler struct {
	httpx.Responder
	o Options
}

// New : Builds the handler.
func New(o Options) *Handler {
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Handler{Responder: httpx.Responder{Logger: o.Logger}, o: o}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/usage", h.Usage)
}

// Labels : What each feature is called on the page.
//
// Here and not beside the feature ids, which are what is stored and
// must never change: a label is words for a person and may.
var Labels = map[environment.Feature]string{
	environment.FeatureChat:           "Chat answer",
	environment.FeatureTitle:          "Conversation title",
	environment.FeatureCondense:       "Condensing",
	environment.FeatureProfile:        "Profile",
	environment.FeatureVocabulary:     "Vocabulary",
	environment.FeatureGreeting:       "Greeting",
	environment.FeatureRuleWriter:     "Writing a rule",
	environment.FeatureComing:         "What is coming",
	environment.FeatureNoticing:       "Noticing",
	environment.FeatureNoticingWriter: "Noticing, written",
	environment.FeatureRuleRemark:     "Rule remark",
	environment.FeatureTerminal:       "Terminal",
	environment.FeatureNote:           "Notes",
	environment.FeatureGlance:         "What am I seeing",
	environment.FeatureOther:          "Other",
}

// Label : What a feature is called, its id where it has no label.
func Label(f environment.Feature) string {
	if l, ok := Labels[f]; ok {
		return l
	}
	return string(f)
}

// Totals : One window's figures.
type Totals struct {
	Calls           int `json:"calls"`
	Input           int `json:"input"`
	Output          int `json:"output"`
	ChatCalls       int `json:"chat_calls"`
	BackgroundCalls int `json:"background_calls"`
}

// Windows : The three windows a page shows side by side.
type Windows struct {
	Today Totals `json:"today"`
	Week  Totals `json:"week"`
	Month Totals `json:"month"`
}

// Feature : One feature's calls in the window asked for.
type Feature struct {
	Feature string `json:"feature"`
	Label   string `json:"label"`
	Calls   int    `json:"calls"`
	Input   int    `json:"input"`
	Output  int    `json:"output"`
}

// Model : One model's calls in the window asked for.
type Model struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Calls  int    `json:"calls"`
	Input  int    `json:"input"`
	Output int    `json:"output"`
}

// Call : One call, as the list shows it.
type Call struct {
	ID                string    `json:"id"`
	At                time.Time `json:"at"`
	Feature           string    `json:"feature"`
	Label             string    `json:"label"`
	Detail            string    `json:"detail,omitempty"`
	Vendor            string    `json:"vendor"`
	Model             string    `json:"model"`
	Input             int       `json:"input"`
	Output            int       `json:"output"`
	DurationMS        int64     `json:"duration_ms"`
	Ended             string    `json:"ended"`
	ChatID            string    `json:"chat_id,omitempty"`
	ConversationID    string    `json:"conversation_id,omitempty"`
	ConversationTitle string    `json:"conversation_title,omitempty"`
}

// Kinds : How many calls of each kind are in the window, for the
// filter's chips.
type Kinds struct {
	All        int `json:"all"`
	Chat       int `json:"chat"`
	Background int `json:"background"`
	Failed     int `json:"failed"`
}

// Response : The body of GET /v1/usage.
type Response struct {
	Totals    Windows   `json:"totals"`
	ByFeature []Feature `json:"by_feature"`
	ByModel   []Model   `json:"by_model"`
	Calls     []Call    `json:"calls"`
	Kinds     Kinds     `json:"kinds"`
	Insights  Insights  `json:"insights"`
}

// windows : The days a window may be asked for.
var windows = map[string]int{"1": 1, "7": 7, "30": 30}

// Usage : GET /v1/usage?days=1|7|30&kind=all|chat|background|failed&limit=N
//
// The totals are always all three windows. Everything else -- the
// groups, the list and the counts for the chips -- is for the window
// days names, today when it names none.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := authn.Of(ctx).User.ID
	q := r.URL.Query()

	days := 1
	if raw := q.Get("days"); raw != "" {
		d, ok := windows[raw]
		if !ok {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "days must be 1, 7 or 30.")
			return
		}
		days = d
	}
	kind := usage.KindAll
	if raw := q.Get("kind"); raw != "" {
		kind = usage.Kind(raw)
		if !kind.Valid() {
			httpx.WriteError(ctx, w, http.StatusBadRequest,
				"kind must be all, chat, background or failed.")
			return
		}
	}
	limit := usage.DefaultLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "limit must be a whole number above nought.")
			return
		}
		limit = usage.Clamp(n)
	}

	out := Response{ByFeature: []Feature{}, ByModel: []Model{}, Calls: []Call{}}
	if h.o.Store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, out)
		return
	}

	now := h.o.Now()
	local := now.In(h.o.Location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, h.o.Location)
	week, month := now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)
	since := map[int]time.Time{1: today, 7: week, 30: month}[days]

	var window usage.Totals
	for _, win := range []struct {
		from time.Time
		into *Totals
		days int
	}{{today, &out.Totals.Today, 1}, {week, &out.Totals.Week, 7}, {month, &out.Totals.Month, 30}} {
		t, err := h.o.Store.Totals(ctx, userID, win.from)
		if err != nil {
			h.Fail(ctx, w, "adding up model calls", err)
			return
		}
		*win.into = Totals{Calls: t.Calls, Input: t.Input, Output: t.Output,
			ChatCalls: t.Chat, BackgroundCalls: t.Background}
		if win.days == days {
			window = t
		}
	}
	out.Kinds = Kinds{All: window.Calls, Chat: window.Chat, Background: window.Background, Failed: window.Failed}

	every, err := h.o.Store.Every(ctx, userID, since)
	if err != nil {
		h.Fail(ctx, w, "reading model calls", err)
		return
	}
	out.Insights = insightsOf(every, since, days, h.o.Location)
	// The window as long, just before: yesterday for today. Totals count
	// from a moment to now, so it is the longer count less this one.
	before := since.AddDate(0, 0, -days)
	if days != 1 {
		before = since.Add(-time.Duration(days) * 24 * time.Hour)
	}
	if t, err := h.o.Store.Totals(ctx, userID, before); err == nil {
		out.Insights.Before = Totals{Calls: t.Calls - window.Calls, Input: t.Input - window.Input, Output: t.Output - window.Output,
			ChatCalls: t.Chat - window.Chat, BackgroundCalls: t.Background - window.Background}
	}

	features, err := h.o.Store.ByFeature(ctx, userID, since)
	if err != nil {
		h.Fail(ctx, w, "model calls by feature", err)
		return
	}
	for _, f := range features {
		out.ByFeature = append(out.ByFeature, Feature{Feature: string(f.Feature), Label: Label(f.Feature),
			Calls: f.Calls, Input: f.Input, Output: f.Output})
	}

	models, err := h.o.Store.ByModel(ctx, userID, since)
	if err != nil {
		h.Fail(ctx, w, "model calls by model", err)
		return
	}
	for _, m := range models {
		out.ByModel = append(out.ByModel, Model{Vendor: m.Vendor, Model: m.Model,
			Calls: m.Calls, Input: m.Input, Output: m.Output})
	}

	calls, err := h.o.Store.List(ctx, usage.Query{UserID: userID, Since: since, Kind: kind, Limit: limit})
	if err != nil {
		h.Fail(ctx, w, "listing model calls", err)
		return
	}
	titles := h.titles(ctx, userID, calls)
	for _, c := range calls {
		out.Calls = append(out.Calls, Call{
			ID: c.ID, At: c.At, Feature: string(c.Feature), Label: Label(c.Feature),
			Detail: c.Detail, Vendor: c.Vendor, Model: c.Model,
			Input: c.Input, Output: c.Output, DurationMS: c.Took.Milliseconds(),
			Ended: string(c.Ended), ChatID: c.ChatID, ConversationID: c.ConversationID,
			ConversationTitle: titles[c.ConversationID],
		})
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

// titles : The titles of the conversations the calls belong to, read
// in one go. A failure leaves them untitled rather than failing the
// page: the figures are what it is for.
func (h *Handler) titles(ctx context.Context, userID string, calls []usage.Call) map[string]string {
	if h.o.Titles == nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, c := range calls {
		if c.ConversationID != "" && !seen[c.ConversationID] {
			seen[c.ConversationID] = true
			ids = append(ids, c.ConversationID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	got, err := h.o.Titles.ConversationTitles(ctx, userID, ids)
	if err != nil {
		h.Logger.WarnContext(ctx, "cannot read the conversations' titles", slog.Any("error", err))
		return nil
	}
	return got
}
