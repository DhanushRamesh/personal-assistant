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

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/base/world/routine"
	"personal-assistant/internal/domain/job"
)

// DefaultLimit, MaxLimit : How many are returned.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// Trier : Something that can run a rule without saying anything.
type Trier interface {
	Try(ctx context.Context, j job.Job) job.Trial
}

// TriedResponse : What a rule did when it was tried.
type TriedResponse struct {
	// Said : What it would have said, empty when it would have
	// said nothing.
	Said string `json:"said,omitempty"`
	// Stopped : Whether a step ended the sequence early.
	Stopped bool `json:"stopped"`
	// Why : Which step stopped it, or what went wrong.
	Why string `json:"why,omitempty"`
	// Steps : What each step did, as far as it got.
	Steps []Step `json:"steps"`
}

// Try : Runs a rule now and says what it did, saying nothing aloud.
//
// A rule could be made and then nothing done but wait for the
// world to set it off. That is how a rule that was wrong stayed
// wrong for a day, and why a rule written tonight could not be
// shown to work at all.
//
// Nothing is written down and nothing is said. The same steps run
// through the same code against the same readings, because a
// trial down a different path would be testing the path.
func (h *Handler) Try(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if h.store == nil || h.trier == nil {
		httpx.WriteError(ctx, w, http.StatusNotImplemented,
			"This server cannot try rules.")
		return
	}

	id := chi.URLParam(r, "id")
	held, err := h.store.Mine(ctx, authn.Of(ctx).User.ID, "", MaxLimit)
	if err != nil {
		h.Fail(ctx, w, "reading the rules", err)
		return
	}
	var want job.Job
	for _, j := range held {
		if j.ID == id {
			want = j
			break
		}
	}
	if want.ID == "" {
		httpx.WriteError(ctx, w, http.StatusNotFound, "There is no rule with that identifier.")
		return
	}

	tried := h.trier.Try(ctx, want)
	out := TriedResponse{Said: tried.Said, Stopped: tried.Stopped, Why: tried.Why}
	// The conditions are answered before any step, so a trial that
	// stopped on them has one more thing to say than it has acts.
	// It is shown first, where it happened.
	out.Steps = make([]Step, 0, len(tried.Acts)+1)
	for i, did := range tried.Did {
		if i >= len(tried.Acts) {
			break
		}
		out.Steps = append(out.Steps, Step{
			Do: tried.Acts[i].Do, Says: says(tried.Acts[i]), Did: did,
		})
	}
	if len(tried.Did) > len(tried.Acts) {
		out.Steps = append(out.Steps, Step{
			Do: "when", Says: want.When.Describe(), Did: tried.Did[len(tried.Acts)],
		})
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

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
	store   Store
	routine routine.Store
	trier   Trier
}

// New : Builds one.
func New(logger *slog.Logger, store Store, usually routine.Store, trier Trier) *Handler {
	return &Handler{
		Responder: httpx.Responder{Logger: logger},
		store:     store, routine: usually, trier: trier,
	}
}

// Mount : Registers the endpoint on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/rules", h.List)
	r.Post("/v1/rules/{id}/try", h.Try)
}

// Step : One thing a rule does, as a line to read.
type Step struct {
	// Do : Which kind of step: announce, tool, write, relay, check.
	Do string `json:"do"`
	// Says : What it does, in words. The step's own fields put
	// together here rather than in the client, so that a new kind
	// of step does not need two edits to be readable.
	Says string `json:"says"`
	// Did : What came of it when the rule was tried -- what a tool
	// found, what a check decided, what would have been said.
	// Empty in a listing, which is a rule at rest.
	Did string `json:"did,omitempty"`
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
	// Next : When it next runs. Absent for a waiting rule, unless
	// it has been set off and is waiting out a delay, which is the
	// one time a waiting rule has a time at all.
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

// Usual : One thing the person usually does, as a condition.
//
// Shown beside the rules and not on a page of its own, because what
// it is for is writing one: these are the sources a rule may stand
// on, and a condition nobody can see is a condition nobody writes.
type Usual struct {
	// Source : What to write in a rule -- usually.home.leaves.
	Source string `json:"source"`
	// Subject, What : The place, by their own name for it, and
	// which end of a visit.
	Subject string `json:"subject"`
	What    string `json:"what"`
	// Hour : The hour, 0 to 23, in their own zone.
	Hour int `json:"hour"`
	// Said : The hour as somebody would say it.
	Said string `json:"said"`
	// Days, Over : How many days agreed, out of how many it was
	// seen at all. Both, because together they are what makes it
	// arguable.
	Days int `json:"days"`
	Over int `json:"over"`
	// At : When it was worked out.
	At time.Time `json:"at"`
}

// ListResponse : The body of a listing.
type ListResponse struct {
	Rules []Rule `json:"rules"`
	// Usually : What a rule may condition on besides a device
	// reading. Sent with the rules rather than fetched separately:
	// it is the vocabulary for the thing on the page.
	Usually []Usual `json:"usually"`
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
	httpx.WriteJSON(ctx, w, http.StatusOK, ListResponse{
		Rules: out, Usually: h.usually(ctx, authn.Of(ctx).User.ID),
	})
}

// usually : What they usually do, as conditions.
//
// A failure costs the vocabulary and not the listing. Somebody
// looking at their rules should see them whether or not the
// routine could be read.
func (h *Handler) usually(ctx context.Context, userID string) []Usual {
	if h.routine == nil {
		return []Usual{}
	}
	held, err := h.routine.All(ctx, userID)
	if err != nil {
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, "cannot read what they usually do",
				slog.Any("error", err))
		}
		return []Usual{}
	}
	out := make([]Usual, 0, len(held))
	for _, f := range held {
		out = append(out, Usual{
			Source: f.Name(), Subject: f.Subject, What: string(f.What),
			Hour: f.Hour, Said: f.Said(), Days: f.Days, Over: f.Over, At: f.At,
		})
	}
	return out
}

// Only : What joins a rule's trigger to the conditions that gate it.
//
// A comma and a plain word, because this is read aloud as often as
// it is read on a screen.
const Only = ", only if "

// described : One job as the listing shows it.
func described(j job.Job) Rule {
	out := Rule{
		ID: j.ID, Name: j.Name, Kind: j.Kind,
		Schedule: j.Schedule, Waits: job.Watches(j.Schedule),
		State: string(j.State), Why: j.Why, Said: j.Said,
		Steps: make([]Step, 0, len(j.Perform)+1),
	}
	// The conditions, where there are any, shown as the first
	// thing the rule does -- which is what they now are.
	//
	// And again on the summary line, which is the only part read
	// at a glance. Owner, 10 October 2026, looking at the list for
	// a rule he had asked for the day before: *"I can't see any
	// rule listed for the screen unlock after 5 hrs and in night."*
	// It was the fourth row. What it said was "when
	// screen.unlocked arrives", because the line is built from the
	// wake hooks alone -- so a rule that fires on perhaps one
	// unlock in ten read as firing on all of them, and the half he
	// would recognise it by was a tap away.
	//
	// Only here, never in the stored column: that string is parsed
	// back for rules written in the older shape, and a suffix on
	// it would be read as part of the trigger.
	if when := j.When.Describe(); when != "nothing to check" {
		out.Steps = append(out.Steps, Step{Do: "when", Says: when})
		out.Schedule = strings.TrimSpace(j.Schedule + Only + when)
	}
	for _, a := range j.Perform {
		out.Steps = append(out.Steps, Step{Do: a.Do, Says: says(a)})
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
func says(a job.Act) string {
	switch {
	case strings.TrimSpace(a.Text) != "":
		return a.Text
	case strings.TrimSpace(a.Ask) != "":
		return a.Ask
	case strings.TrimSpace(a.Use) != "":
		if with := arguments(a.With); with != "" {
			return a.Use + " (" + with + ")"
		}
		return a.Use
	}
	return a.Do
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
