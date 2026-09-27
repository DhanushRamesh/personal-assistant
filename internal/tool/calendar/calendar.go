// Package calendar offers the diary to the model.
//
// Three tools, and the shape of them follows the two scopes rather than
// what a calendar could do in principle. The assistant has a calendar
// of its own and may do anything to it; of everything else it can see
// only that a time is taken, never by what. So there is no tool for
// changing the person's real meetings, because there is no permission
// for it and pretending otherwise would put the model in the position
// of promising something it cannot do.
package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/google"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Clock : What the tools need to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
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

// Diary : What the tools act on.
type Diary interface {
	Add(ctx context.Context, userID string, e calendar.Event) (*calendar.Event, error)
	Cancel(ctx context.Context, userID, eventID string) error
	Mine(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, error)
	Busy(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, error)
}

// All : Every calendar tool, in the order they are offered.
func All(diary Diary, clock Clock) []tool.Tool {
	return []tool.Tool{
		add(diary, clock),
		agenda(diary, clock),
		free(diary, clock),
		cancel(diary),
	}
}

// whenTrouble : What to tell the model when the diary cannot be reached.
//
// The one failure worth wording carefully. A connection that has lapsed
// is not a fault the model can retry around and not something it should
// apologise vaguely for: only the person can fix it, and only if they
// are told plainly what to do.
func whenTrouble(err error) tool.Result {
	switch {
	case err == nil:
		return tool.OK("")
	case errors.Is(err, google.ErrNotConnected):
		return tool.Failed("No Google account is connected, so there is no calendar. " +
			"Tell them to connect one on the settings screen.")
	case errors.Is(err, google.ErrNeedsReconnect):
		return tool.Failed("The Google connection has lapsed and must be granted again on " +
			"the settings screen. Tell them that, and do not say the calendar is empty.")
	default:
		return tool.Failed(err.Error())
	}
}

// add : Puts something in the diary.
func add(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "calendar_add",
		Purpose: "Put an event in the diary, on the assistant's own calendar.",
		UseWhen: "The person wants something written down for a date and time -- an appointment, a " +
			"meeting, a trip, a birthday. Anything they would look for in a calendar rather than be " +
			"interrupted about.",
		Avoid: "Not for something they want said aloud at a moment: that is reminder_set. A dentist " +
			"appointment next Tuesday is a calendar event; being told in ten minutes to take tablets " +
			"is a reminder. When they want both, set both.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"title", "starts"},
			Properties: map[string]tool.Property{
				"title": {
					Type:        "string",
					Description: "What the event is called, as it should read in a calendar.",
				},
				"starts": {
					Type: "string",
					Description: "When it starts, as 2026-09-28T15:00 in the person's own " +
						"time. No zone: the server knows theirs.",
				},
				"minutes": {
					Type: "integer",
					Description: "How long it lasts. Left out, an hour is assumed, which is " +
						"the ordinary length of an appointment.",
				},
				"all_day": {
					Type: "boolean",
					Description: "True for something that takes the whole day and has no time, " +
						"such as a birthday or a day off.",
				},
				"where": {Type: "string", Description: "The place, if they said one."},
				"notes": {Type: "string", Description: "Anything else worth keeping with it."},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Title   string `json:"title"`
				Starts  string `json:"starts"`
				Minutes int    `json:"minutes"`
				AllDay  bool   `json:"all_day"`
				Where   string `json:"where"`
				Notes   string `json:"notes"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if strings.TrimSpace(args.Title) == "" {
				return tool.Failed("An event needs a name.")
			}

			starts, err := when(args.Starts, clock.where())
			if err != nil {
				return tool.Failed(err.Error())
			}
			minutes := args.Minutes
			if minutes <= 0 {
				minutes = 60
			}
			ends := starts.Add(time.Duration(minutes) * time.Minute)
			if args.AllDay {
				ends = starts
			}

			made, err := diary.Add(ctx, in.Caller.UserID, calendar.Event{
				Title: args.Title, Where: args.Where, Notes: args.Notes,
				Starts: starts, Ends: ends, AllDay: args.AllDay,
			})
			if err != nil {
				return whenTrouble(err)
			}
			return tool.OK("Put in the diary: " + describe(*made, clock.where()))
		},
	}
}

// agenda : What the assistant has written down.
func agenda(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "calendar_list",
		Purpose: "Read what is in the diary over a stretch of days.",
		UseWhen: "They ask what is on -- today, tomorrow, this week, or before a particular date.",
		Avoid: "This shows only what the assistant put there. It cannot see the person's own " +
			"meetings, so never say the day is empty on the strength of it: say that nothing was " +
			"written down here, and use calendar_free to find out whether they are actually busy.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"days": {
					Type: "integer",
					Description: "How many days ahead to look, counting today. One is the rest " +
						"of today. Left out, seven.",
				},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Days int `json:"days"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			days := args.Days
			if days <= 0 {
				days = 7
			}
			from := clock.now()
			to := from.AddDate(0, 0, days)

			found, err := diary.Mine(ctx, in.Caller.UserID, from, to)
			if err != nil {
				return whenTrouble(err)
			}
			if len(found) == 0 {
				return tool.OK(fmt.Sprintf("Nothing is written in the diary in the next %d days. "+
					"That is only what was put there through you; it says nothing about their own "+
					"calendar.", days))
			}

			var b strings.Builder
			fmt.Fprintf(&b, "In the diary over the next %d days:", days)
			for _, e := range found {
				b.WriteString("\n- ")
				b.WriteString(describe(e, clock.where()))
				b.WriteString("  [id ")
				b.WriteString(e.ID)
				b.WriteString("]")
			}
			return tool.OK(b.String())
		},
	}
}

// free : Whether a time is taken, across every calendar they have.
func free(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "calendar_free",
		Purpose: "Say whether the person is free at a time, across all of their calendars.",
		UseWhen: "They ask whether they are free, whether something clashes, or when they are " +
			"available. Also before putting anything in the diary at a time they proposed.",
		Avoid: "This sees when they are busy and never what they are doing: the permission granted " +
			"is availability only. Do not guess at what a busy period is, and do not describe it as " +
			"a meeting -- say the time is taken and leave it there.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"from"},
			Properties: map[string]tool.Property{
				"from": {
					Type:        "string",
					Description: "Start of the window, as 2026-09-28T15:00 in the person's own time.",
				},
				"minutes": {
					Type:        "integer",
					Description: "How long a window to check. Left out, an hour.",
				},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				From    string `json:"from"`
				Minutes int    `json:"minutes"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			from, err := when(args.From, clock.where())
			if err != nil {
				return tool.Failed(err.Error())
			}
			minutes := args.Minutes
			if minutes <= 0 {
				minutes = 60
			}
			to := from.Add(time.Duration(minutes) * time.Minute)

			busy, err := diary.Busy(ctx, in.Caller.UserID, from, to)
			if err != nil {
				return whenTrouble(err)
			}

			loc := clock.where()
			window := from.In(loc).Format("3:04 pm on Monday 2 January") + " for " +
				fmt.Sprintf("%d minutes", minutes)
			if len(busy) == 0 {
				return tool.OK("Free: nothing is booked from " + window + ".")
			}

			var b strings.Builder
			b.WriteString("Busy from " + window + ". Taken:")
			for _, slot := range busy {
				fmt.Fprintf(&b, "\n- %s to %s",
					slot.Starts.In(loc).Format("3:04 pm"), slot.Ends.In(loc).Format("3:04 pm"))
			}
			b.WriteString("\n(What they are doing is not visible, only that the time is taken.)")
			return tool.OK(b.String())
		},
	}
}

// cancel : Takes something out of the diary.
func cancel(diary Diary) tool.Tool {
	return tool.Tool{
		Name:    "calendar_cancel",
		Purpose: "Remove an event the assistant put in the diary.",
		UseWhen: "They want something taken out that is listed by calendar_list.",
		Avoid: "Only events on the assistant's own calendar can be removed, which is every event it " +
			"can see. There is no way to touch the person's real meetings and no point trying.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {Type: "string", Description: "The event's identifier, from a listing."},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if strings.TrimSpace(args.ID) == "" {
				return tool.Failed("Which event? Use calendar_list to find its identifier.")
			}

			if err := diary.Cancel(ctx, in.Caller.UserID, args.ID); err != nil {
				return whenTrouble(err)
			}
			return tool.OK("Taken out of the diary.")
		},
	}
}

// when : A time written the way the tools ask for it, read in the
// person's own zone.
//
// No zone in the string on purpose. A model asked for an offset writes
// whatever it last saw, and an hour wrong in a diary is worse than a
// refusal.
func when(written string, loc *time.Location) (time.Time, error) {
	written = strings.TrimSpace(written)
	if written == "" {
		return time.Time{}, fmt.Errorf("a time is needed, as 2026-09-28T15:00")
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", time.DateOnly} {
		if at, err := time.ParseInLocation(layout, written, loc); err == nil {
			return at.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a time this understands; write it as 2026-09-28T15:00", written)
}

// describe : One event in a line, for the model to read back.
func describe(e calendar.Event, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(e.Title)
	if e.AllDay {
		b.WriteString(", all day on " + e.Starts.In(loc).Format("Monday 2 January"))
	} else {
		b.WriteString(", " + e.Starts.In(loc).Format("3:04 pm on Monday 2 January"))
		if !e.Ends.IsZero() && e.Ends.After(e.Starts) {
			b.WriteString(" until " + e.Ends.In(loc).Format("3:04 pm"))
		}
	}
	if w := strings.TrimSpace(e.Where); w != "" {
		b.WriteString(", at " + w)
	}
	return b.String()
}
