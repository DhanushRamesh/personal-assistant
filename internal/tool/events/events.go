// Package events : Lets the assistant read what the person's own devices
// reported.
//
// Everything else it knows, it knows because it was told. This is the one
// source that arrives unasked, and reading it is what turns a table that
// fills itself into something the person can ask about: when they got in,
// whether they have moved, how long they were out.
//
// Read on demand and never prefetched. This is the largest thing the
// person has and it grows every day; putting it into every prompt would
// cost every turn for the sake of the few that ask.
//
// Condensed rather than listed. A phone reports what it sees, and what it
// sees includes a network dropping and returning eight times over lunch.
// Handing all of that over spends the turn and gets the whole list read
// back; runs of the same thing are collapsed so that what remains is the
// shape of the day.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/event"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

const (
	// DefaultDays : How far back a question reaches when it does not say.
	//
	// Today. "What have I been doing" means today unless somebody says
	// otherwise, and a week of events answered to that question buries
	// the morning they were asking about.
	DefaultDays = 1

	// MaxDays : The furthest back one call may reach.
	MaxDays = 90

	// DefaultLimit, MaxLimit : How many events are read.
	//
	// Read before collapsing, so the limit is on rows rather than on
	// lines: a day of a flapping network is three hundred rows and four
	// lines.
	DefaultLimit = 300
	MaxLimit     = 1000

	// MaxLines : How many lines the answer may run to once collapsed.
	//
	// What is left after collapsing is the shape of a day, and a model
	// given more than this says all of it.
	MaxLines = 40

	// Together : How close two of the same thing must be to count as one
	// run.
	//
	// Nothing is collapsed across a gap longer than this, because a gap
	// is the information: joining the same network at nine and at six is
	// two facts, and eight times over lunch is one.
	Together = 2 * time.Hour
)

// Reader : Where the events are.
type Reader interface {
	// Recent : What happened, newest first by when it happened.
	Recent(ctx context.Context, userID string, since time.Time, kind string, limit int) ([]event.Event, error)
	// Kinds : Which kinds exist, and how many of each.
	Kinds(ctx context.Context, userID string) ([]event.Kind, error)
}

// Clock : What this needs to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// Tools : Everything in this package.
func Tools(reader Reader, clock Clock) []tool.Tool {
	return []tool.Tool{recent(reader, clock)}
}

// recent : What the person's devices have reported.
func recent(reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "event_recent",
		Domain:  "event",
		Lists:   true,
		Purpose: "Read what the person's own devices reported happening to them.",
		UseWhen: "Any question about what the person has actually been doing rather than what they have " +
			"told you: where they have been, when they got in or went out, whether they have moved, " +
			"what their day looked like, when something last happened. Also when you are about to " +
			"remark on their day unprompted and need to know what it was.",
		Avoid: "Not for things they asked you to remember, which are memories, and not for what is " +
			"coming, which is the calendar and the reminders. Do not answer from the conversation: " +
			"what you said earlier about their day was true when you said it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"kind": {
					Type: "string",
					Description: "Only this kind of event, as a dotted name such as network.joined. " +
						"Leave it out for everything. Call once without it first if you do not " +
						"know which kinds exist: the answer lists them.",
				},
				"days": {
					Type: "integer",
					Description: "How many days back to look. 1 is today only, 7 is the past week. " +
						"Use a larger number for when something last happened.",
					Default: DefaultDays,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what have I been up to today", Args: `{"saying":"looking at your day"}`},
			{Ask: "when did I last leave the office", Args: `{"kind":"network.left","days":30}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Kind string `json:"kind"`
				Days int    `json:"days"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if reader == nil {
				return tool.Failed("Nothing on this server is collecting what your devices see.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			days := args.Days
			if days <= 0 {
				days = DefaultDays
			}
			days = min(days, MaxDays)

			// From the start of that day in the person's own zone, not
			// from this moment minus a day. "Today" means since
			// midnight; measuring back from now would answer a question
			// about this morning with yesterday evening in it.
			where := clock.where()
			local := clock.now().In(where)
			since := time.Date(local.Year(), local.Month(), local.Day(),
				0, 0, 0, 0, where).AddDate(0, 0, -(days - 1))

			found, err := reader.Recent(ctx, in.Caller.UserID, since,
				strings.TrimSpace(args.Kind), DefaultLimit)
			if err != nil {
				return tool.Failed(err.Error())
			}

			if len(found) == 0 {
				return tool.OK(nothing(ctx, reader, in.Caller.UserID, args.Kind, days))
			}
			return tool.OK(describe(found, args.Kind, days, where))
		},
	}
}

// nothing : What to say when the window held none.
//
// Which kinds do exist, when the question found nothing, because the
// likeliest reason is a kind that was guessed at rather than a quiet day.
func nothing(ctx context.Context, reader Reader, userID, kind string, days int) string {
	over := window(days)
	if kind == "" {
		return "Their devices reported nothing " + over + "."
	}

	kinds, err := reader.Kinds(ctx, userID)
	if err != nil || len(kinds) == 0 {
		return "Nothing of kind " + kind + " was reported " + over + "."
	}
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		names = append(names, fmt.Sprintf("%s (%d)", k.Kind, k.Count))
	}
	return prompt.Text(
		"Nothing of kind "+kind+" was reported "+over+".",
		"The kinds that do exist are: "+strings.Join(names, ", ")+".",
	)
}

// window : How to say the period a question covered.
func window(days int) string {
	switch {
	case days <= 1:
		return "today"
	case days == 7:
		return "in the past week"
	default:
		return fmt.Sprintf("in the past %d days", days)
	}
}

// run : Several of the same thing, close together.
type run struct {
	kind  string
	value string
	first time.Time
	last  time.Time
	count int
}

// describe : The shape of what happened, collapsed.
func describe(found []event.Event, kind string, days int, where *time.Location) string {
	// Oldest first: a day is read forwards. The store answers newest
	// first because a listing wants that, and a narrative does not.
	sort.Slice(found, func(i, j int) bool {
		return found[i].OccurredAt.Before(found[j].OccurredAt)
	})

	var runs []run
	for _, e := range found {
		at := e.OccurredAt.In(where)
		v := value(e.Payload)
		if n := len(runs); n > 0 {
			last := &runs[n-1]
			if last.kind == e.Kind && last.value == v &&
				at.Sub(last.last) <= Together &&
				sameDay(last.last, at) {
				last.last = at
				last.count++
				continue
			}
		}
		runs = append(runs, run{kind: e.Kind, value: v, first: at, last: at, count: 1})
	}

	var b strings.Builder
	header := fmt.Sprintf("%d events %s", len(found), window(days))
	if kind != "" {
		header = fmt.Sprintf("%d of kind %s %s", len(found), kind, window(days))
	}
	if len(runs) < len(found) {
		header += fmt.Sprintf(", as %d entries once repeats are grouped", len(runs))
	}
	b.WriteString(header + ". Times are the person's own.\n")

	day := ""
	shown := 0
	for _, r := range runs {
		if shown >= MaxLines {
			b.WriteString(fmt.Sprintf("... and %d earlier entries not shown.\n", len(runs)-shown))
			break
		}
		if d := r.first.Format("Monday 2 January"); d != day {
			day = d
			b.WriteString("\n" + d + "\n")
		}
		b.WriteString("  " + line(r) + "\n")
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

// line : One entry.
func line(r run) string {
	at := r.first.Format("15:04")
	what := r.kind
	if r.value != "" {
		what += " " + r.value
	}
	if r.count == 1 {
		return at + "  " + what
	}
	return fmt.Sprintf("%s  %s  (%d times, last at %s)",
		at, what, r.count, r.last.Format("15:04"))
}

// value : The one thing a payload is mostly about.
//
// Whatever is under "value", which is what a phone writing these sends.
// Anything else is rendered as it stands: a payload nobody planned for is
// still worth showing, and guessing at which of its fields matters would
// drop the one that did.
func value(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil || len(into) == 0 {
		return ""
	}
	if v, ok := into["value"]; ok && len(into) == 1 {
		return fmt.Sprint(v)
	}

	keys := make([]string, 0, len(into))
	for k := range into {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, into[k]))
	}
	return strings.Join(parts, " ")
}

// sameDay : Whether two local moments are the same date.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
