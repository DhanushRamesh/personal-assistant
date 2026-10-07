// Package rules shows the standing rules a person has.
//
// Owner, 7 October 2026: *"currently I'm having a hardcoded 25, 20,
// 15, 10, 5, 100 for battery, and like this I'll be adding many
// rules. I can't be adding rules manually like this... before that I
// should be able to view the standing rules in UI."*
//
// Until now the only way to see one was to ask for it aloud or to
// read the table. A rule is a thing that acts on its own, possibly
// every two minutes, and a person cannot be asked to trust a set of
// them they have never seen written down -- least of all before
// agreeing that more should be made for them.
//
// Read only. Making and cancelling them is what rule_add and
// rule_cancel are for, and a second way to write them is a second
// place for the grammar to be checked.
package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/DhanushRamesh/personal-assistant/internal/api/authn"
	"github.com/DhanushRamesh/personal-assistant/internal/api/httpx"
	"github.com/DhanushRamesh/personal-assistant/internal/job"
)

// DefaultLimit, MaxLimit : How many are returned.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Store : Where the rules are kept.
type Store interface {
	// Mine : This person's, newest first. An empty kind is all of
	// them, which is what this wants: a listing that hid the
	// reminders would be a listing that disagreed with the engine.
	// No states is every state.
	Mine(ctx context.Context, userID, kind string, limit int, states ...job.State) ([]job.Job, error)
}

// Handler : Serves them.
type Handler struct {
	httpx.Responder
	store Store
}

// New : Builds one.
func New(logger *slog.Logger, store Store) *Handler {
	return &Handler{Responder: httpx.Responder{Logger: logger}, store: store}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/rules", h.List)
}

// Step : One thing a rule does, as a line to read.
type Step struct {
	// Do : Which kind of step: announce, tool, write, relay, check.
	Do string `json:"do"`
	// Says : What it does, in words. The step's own fields put
	// together here rather than in the client, so that a new kind
	// of step does not need two edits to be readable.
	Says string `json:"says"`
}

// Rule : One standing rule.
type Rule struct {
	ID string `json:"id"`
	// Name : What it is called in a listing.
	Name string `json:"name"`
	// Kind : What it is in the person's own words -- rule, reminder.
	Kind string `json:"kind"`
	// Schedule : When it runs, as it was written.
	Schedule string `json:"schedule"`
	// Waits : Whether it waits for something to happen rather than
	// for a time. What tells "every 5m" from "when the battery
	// drops", which read alike in a list and behave nothing alike.
	Waits bool `json:"waits"`
	// State : active, done or cancelled.
	State string `json:"state"`
	// Steps : What it does, in order.
	Steps []Step `json:"steps"`
	// Next : When it next runs. Absent for one that waits.
	Next *time.Time `json:"next,omitempty"`
	// Last : When it last ran. Absent for one that never has.
	Last *time.Time `json:"last,omitempty"`
	// Why : What happened last run -- what went wrong, or which
	// check stopped it. Empty when it simply ran.
	Why string `json:"why,omitempty"`
	// Said : The sentence it last said, so that a rule which is
	// running and silent is told from one that is not running.
	Said string `json:"said,omitempty"`
}

// ListResponse : The body of a listing.
type ListResponse struct {
	Rules []Rule `json:"rules"`
}

// List : The person's rules, newest first.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(ctx, w, http.StatusBadRequest, "That limit could not be read.")
			return
		}
		limit = min(n, MaxLimit)
	}

	if h.store == nil {
		httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Rules: []Rule{}})
		return
	}

	// What is standing, unless the whole record is asked for. A rule
	// that was called off is not a rule somebody has; listed beside
	// the ones that are, it is a thing to read past every time, and
	// there is no limit on how many of them accumulate.
	//
	// Asked for rather than shown, as the reminders are, and for the
	// same reason: a screen that opens on a log buries the handful
	// of things actually running.
	var states []job.State
	if r.URL.Query().Get("past") != "true" {
		states = []job.State{job.Active}
	}

	held, err := h.store.Mine(ctx, authn.Of(ctx).User.ID,
		r.URL.Query().Get("kind"), limit, states...)
	if err != nil {
		h.Fail(ctx, w, "reading the rules", err)
		return
	}

	out := make([]Rule, 0, len(held))
	for _, j := range held {
		out = append(out, described(j))
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{Rules: out})
}

// described : One job as the listing shows it.
func described(j job.Job) Rule {
	out := Rule{
		ID: j.ID, Name: j.Name, Kind: j.Kind,
		Schedule: j.Schedule, Waits: job.Watches(j.Schedule),
		State: string(j.State), Why: j.Why, Said: j.Said,
		Steps: make([]Step, 0, len(j.Steps)),
	}
	for _, s := range j.Steps {
		out.Steps = append(out.Steps, Step{Do: string(s.Do), Says: says(s)})
	}
	if !j.Next.IsZero() {
		at := j.Next.UTC()
		out.Next = &at
	}
	if !j.Last.IsZero() {
		at := j.Last.UTC()
		out.Last = &at
	}
	return out
}

// says : What one step does, in a line.
//
// Built from whichever of the step's fields it uses, so that a kind
// nobody has thought of here still reads as something rather than as
// a blank. The field names are the grammar's own and are what the
// person said when they made the rule.
func says(s job.Step) string {
	switch {
	case strings.TrimSpace(s.Say) != "":
		return s.Say
	case strings.TrimSpace(s.When) != "":
		return s.When
	case strings.TrimSpace(s.Ask) != "":
		return s.Ask
	case strings.TrimSpace(s.Tool) != "":
		if with := arguments(s.Args); with != "" {
			return s.Tool + " (" + with + ")"
		}
		return s.Tool
	}
	return ""
}

// arguments : What a tool step passes, as words rather than as JSON.
//
// A page of rules is read to find out what they do, and
// {"kind": "battery.level", "limit": 6} is punctuation around two
// facts. Sorted, because a map has no order and a listing that
// shuffled itself between refreshes would be unreadable.
//
// Anything that cannot be read back as an object is passed through
// whole: a tool is free to take something other than an object, and
// showing it raw is better than showing nothing.
func arguments(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "{}" || text == "null" {
		return ""
	}
	var held map[string]any
	if err := json.Unmarshal(raw, &held); err != nil {
		return text
	}

	keys := make([]string, 0, len(held))
	for k := range held {
		// What the assistant says while the tool runs, which is
		// about the moment and not about the rule.
		if k == "saying" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s %v", k, held[k]))
	}
	return strings.Join(out, ", ")
}
