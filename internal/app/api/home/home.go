// Package home : What a client's dashboard shows of the server: how the
// server itself stands, and what the day still holds.
//
// Both are read-only and both say only what the server can actually see.
// What it cannot -- the microphone's level, which speech engine the
// bridge fell back to -- is left out rather than guessed, so a dashboard
// never shows a reading nobody took.
package home

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"personal-assistant/internal/app/api/authn"
	"personal-assistant/internal/app/api/httpx"
	"personal-assistant/internal/app/api/views"
	livehub "personal-assistant/internal/app/live"
	"personal-assistant/internal/base/platform/logging"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/domain/calendar"
	"personal-assistant/internal/domain/remind"
)

// Checking : How long a dependency is given to answer before it is
// reported as not answering. A dashboard that hangs is worse than one
// that says something is down.
const Checking = 2 * time.Second

// Fetching : How long the diary is given. Several calendars are read at
// once, each a round trip to Google.
const Fetching = 8 * time.Second

// Pinger : The part of a database handle a health reading needs.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Chats : Where the last turn is read from.
type Chats interface {
	List(ctx context.Context, f chat.Filter) ([]chat.Summary, error)
}

// Latest : What the live hub last heard of a kind, such as the satellite.
type Latest interface {
	Latest(kind livehub.Kind) (livehub.Event, bool)
}

// Diary : What is written down, in every calendar.
type Diary interface {
	Everywhere(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, []string, error)
}

// Options : What the handler reads from. Only Logger and DB are
// required; each part left out is left out of the answer too.
type Options struct {
	Logger    *slog.Logger
	DB        Pinger
	Chats     Chats
	Live      Latest
	Reminders remind.Store
	Diary     Diary
	// Model : The model a client is answered with unless it chose another.
	Model string
	// StartedAt : When this process started.
	StartedAt time.Time
	// Restarts : How many times the supervisor has restarted the server
	// since it was last started by hand, when that can be read. Restarts
	// rather than uptime: "up 8 seconds" reads as healthy and can mean
	// four hundred crashes.
	Restarts func(ctx context.Context) (int, bool)
	// Location : The person's zone, for where today ends.
	Location *time.Location
	Now      func() time.Time
}

// Handler : Serves the dashboard endpoints.
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

// Mount : Registers the endpoints on r, which must already require
// authentication.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/home/server", h.Server)
	r.Get("/v1/home/today", h.Today)
}

// Satellite : The voice satellite as Home Assistant last reported it.
type Satellite struct {
	// State : idle, listening, processing, responding or unknown.
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

// Turn : The last thing asked, and how long its answer took.
type Turn struct {
	Status     string     `json:"status"`
	Channel    string     `json:"channel,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// Seconds : From starting to finishing, once it has.
	Seconds float64 `json:"seconds,omitempty"`
}

// ServerResponse : How the server stands.
type ServerResponse struct {
	StartedAt time.Time `json:"started_at"`
	// Restarts : Absent when the supervisor cannot be asked.
	Restarts *int `json:"restarts,omitempty"`
	// Database : ok or unreachable.
	Database  string     `json:"database"`
	Satellite *Satellite `json:"satellite,omitempty"`
	Model     string     `json:"model,omitempty"`
	LastTurn  *Turn      `json:"last_turn,omitempty"`
}

// Server : How the server stands: since when, how often it was restarted,
// whether its database answers, what the satellite last did, the model,
// and the last turn.
func (h *Handler) Server(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := ServerResponse{StartedAt: h.o.StartedAt.UTC(), Database: "ok", Model: h.o.Model}

	check, done := context.WithTimeout(ctx, Checking)
	defer done()
	if err := h.o.DB.Ping(check); err != nil {
		out.Database = "unreachable"
		h.Logger.WarnContext(ctx, "database does not answer for the dashboard", slog.Any("error", err))
	}
	if h.o.Restarts != nil {
		if n, ok := h.o.Restarts(check); ok {
			out.Restarts = &n
		}
	}
	if h.o.Live != nil {
		if e, ok := h.o.Live.Latest(livehub.SatelliteState); ok {
			out.Satellite = &Satellite{State: e.State, At: e.At.UTC()}
		}
	}
	if h.o.Chats != nil {
		last, err := h.o.Chats.List(check, chat.Filter{UserID: authn.Of(ctx).User.ID, Limit: 1})
		if err != nil {
			h.Logger.WarnContext(ctx, "cannot read the last turn for the dashboard", slog.Any("error", err))
		} else if len(last) > 0 && last[0].StartedAt != nil {
			s := last[0]
			t := &Turn{Status: string(s.Status), Channel: string(s.Channel), StartedAt: s.StartedAt.UTC(), FinishedAt: s.FinishedAt}
			if s.FinishedAt != nil {
				t.Seconds = s.FinishedAt.Sub(*s.StartedAt).Seconds()
			}
			out.LastTurn = t
		}
	}
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}

// Event : One thing in the diary.
type Event struct {
	Title    string    `json:"title"`
	Where    string    `json:"where,omitempty"`
	Starts   time.Time `json:"starts"`
	Ends     time.Time `json:"ends"`
	AllDay   bool      `json:"all_day,omitempty"`
	Calendar string    `json:"calendar,omitempty"`
}

// TodayResponse : What the rest of today and tomorrow hold.
type TodayResponse struct {
	// From, To : The window read: now, to the end of tomorrow in the
	// person's zone.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Diary : ok, not_connected when there is no Google account to read,
	// or unavailable when it did not answer.
	Diary  string  `json:"diary"`
	Events []Event `json:"events"`
	// Unread : Calendars that could not be read, by name; the rest were.
	Unread    []string         `json:"unread_calendars,omitempty"`
	Reminders []views.Reminder `json:"reminders"`
}

// Today : What is still to come today and tomorrow: every calendar's
// events -- one that has ended is gone, one under way stays -- and the
// reminders due by then, both in time order. The diary is bounded: one
// that does not answer in time is said to be unavailable, and the
// reminders are still answered.
func (h *Handler) Today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := authn.Of(ctx).User.ID
	now := h.o.Now().In(h.o.Location)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, h.o.Location)
	end := day.AddDate(0, 0, 2)
	out := TodayResponse{From: now.UTC(), To: end.UTC(), Diary: "not_connected", Events: []Event{}, Reminders: []views.Reminder{}}

	var wg sync.WaitGroup
	if h.o.Diary != nil {
		out.Diary = "unavailable"
		wg.Add(1)
		go func() {
			defer wg.Done()
			ask, done := context.WithTimeout(ctx, Fetching)
			defer done()
			logging.Surviving(ask, h.Logger, "reading the diary for the dashboard", func() {
				found, unread, err := h.o.Diary.Everywhere(ask, user, day, end)
				if err != nil {
					h.Logger.WarnContext(ctx, "cannot read the diary for the dashboard", slog.Any("error", err))
					return
				}
				events := make([]Event, 0, len(found))
				for _, e := range found {
					// Ended is gone; under way is still today's.
					if !e.Ends.IsZero() && !e.Ends.After(now) {
						continue
					}
					events = append(events, Event{Title: e.Title, Where: e.Where, Starts: e.Starts.UTC(), Ends: e.Ends.UTC(), AllDay: e.AllDay, Calendar: e.Calendar})
				}
				sort.SliceStable(events, func(i, j int) bool { return events[i].Starts.Before(events[j].Starts) })
				out.Events, out.Unread, out.Diary = events, unread, "ok"
			})
		}()
	}
	if h.o.Reminders != nil {
		waiting, err := h.o.Reminders.List(ctx, user, remind.Pending, remind.Held)
		if err != nil {
			h.Logger.WarnContext(ctx, "cannot read reminders for the dashboard", slog.Any("error", err))
		} else {
			due := make([]remind.Reminder, 0, len(waiting))
			for _, rem := range waiting {
				if rem.DueAt.Before(end) {
					due = append(due, rem)
				}
			}
			sort.SliceStable(due, func(i, j int) bool { return due[i].DueAt.Before(due[j].DueAt) })
			out.Reminders = views.OfReminders(due)
		}
	}
	wg.Wait()
	httpx.WriteJSON(ctx, w, http.StatusOK, out)
}
