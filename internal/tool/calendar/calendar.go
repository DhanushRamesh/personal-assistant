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
	"strconv"
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
	Update(ctx context.Context, userID, eventID string, a calendar.Amend) (*calendar.Event, error)
	One(ctx context.Context, userID, eventID string) (*calendar.Event, error)
	Cancel(ctx context.Context, userID, eventID string) error
	Mine(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, error)
	Busy(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, error)
}

// All : Every calendar tool, in the order they are offered.
func All(diary Diary, clock Clock) []tool.Tool {
	return []tool.Tool{
		add(diary, clock),
		amend(diary, clock),
		agenda(diary, clock),
		free(diary, clock),
		cancel(diary, clock),
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

			day := starts.AddDate(0, 0, 1)
			before := len(onTheDay(ctx, diary, in.Caller.UserID, starts, day))

			made, err := diary.Add(ctx, in.Caller.UserID, calendar.Event{
				Title: args.Title, Where: args.Where, Notes: args.Notes,
				Starts: starts, Ends: ends, AllDay: args.AllDay,
			})
			if err != nil {
				return whenTrouble(err)
			}

			// Read the day back from Google rather than trusting what
			// the insert returned. An event that was accepted and is
			// not there is the one failure nobody can hear.
			after := onTheDay(ctx, diary, in.Caller.UserID, starts, day)
			if !among(after, made.ID) {
				return tool.Unverified("Putting "+strconv.Quote(args.Title)+" in the diary",
					"reading that day back does not show it")
			}
			return tool.OK(fmt.Sprintf("Put in the diary and read back: %s. That day had %d %s "+
				"before and has %d now. Tell them what was added and when.",
				describe(*made, clock.where()), before, thing(before), len(after)))
		},
	}
}

// amend : Changes an event already in the diary.
//
// Here because without it the only way to rename something was to
// cancel it and add another, which loses the identifier and leaves two
// events behind when the cancel fails. That happened on 27 September
// 2026 and left a duplicate pair an hour before this was written.
func amend(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "calendar_update",
		Purpose: "Change an event already in the diary: its name, when it is, where, or the notes.",
		UseWhen: "They want an existing event altered rather than replaced -- rename it, move it, " +
			"make it longer, add a place.",
		Avoid: "Give only what is changing; anything left out keeps what it had. Never cancel an " +
			"event and add another in its place: that gives it a new identifier, and if the cancel " +
			"fails you have left them with two. Use this.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Description: "The event's identifier, from calendar_list. Never guessed.",
				},
				"title":   {Type: "string", Description: "A new name for it."},
				"starts":  {Type: "string", Description: "A new start, as 2026-09-28T15:00 in their own time."},
				"minutes": {Type: "integer", Description: "A new length in minutes, counted from the start."},
				"where":   {Type: "string", Description: "A new place. An empty string clears it."},
				"notes":   {Type: "string", Description: "New notes. An empty string clears them."},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string  `json:"id"`
				Title   *string `json:"title"`
				Starts  string  `json:"starts"`
				Minutes int     `json:"minutes"`
				Where   *string `json:"where"`
				Notes   *string `json:"notes"`
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

			// Read it first, both to have something to compare against
			// and so a wrong identifier is caught before anything is
			// written.
			before, err := diary.One(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				return whenTrouble(err)
			}
			if before == nil {
				return tool.Failed("There is no event with that identifier. List the diary and use " +
					"the identifier exactly as it came back, rather than guessing.")
			}
			// Copied, not held by pointer. A Diary that hands back a
			// pointer into its own storage would otherwise have the
			// write mutate what we are comparing against, and the diff
			// would quietly show that nothing changed.
			was := *before

			change := calendar.Amend{Title: args.Title, Where: args.Where, Notes: args.Notes}
			if strings.TrimSpace(args.Starts) != "" {
				starts, err := when(args.Starts, clock.where())
				if err != nil {
					return tool.Failed(err.Error())
				}
				change.Starts = &starts

				// Moving the start alone keeps the length it had, which
				// is what somebody means by "move it to four".
				length := was.Ends.Sub(was.Starts)
				if args.Minutes > 0 {
					length = time.Duration(args.Minutes) * time.Minute
				}
				if length <= 0 {
					length = time.Hour
				}
				ends := starts.Add(length)
				change.Ends = &ends
			} else if args.Minutes > 0 {
				ends := was.Starts.Add(time.Duration(args.Minutes) * time.Minute)
				change.Ends = &ends
			}

			if change.Empty() {
				return tool.Failed("Nothing was given to change. Say what should be different.")
			}
			if _, err := diary.Update(ctx, in.Caller.UserID, args.ID, change); err != nil {
				return whenTrouble(err)
			}

			after, err := diary.One(ctx, in.Caller.UserID, args.ID)
			if err != nil || after == nil {
				return tool.Unverified("Changing that event", "it can no longer be read back")
			}

			loc := clock.where()
			return tool.Changed("Changed the event",
				tool.Change{What: "the name", From: was.Title, To: after.Title},
				tool.Change{What: "when it is",
					From: was.Starts.In(loc).Format("3:04 pm on Monday 2 January"),
					To:   after.Starts.In(loc).Format("3:04 pm on Monday 2 January")},
				tool.Change{What: "the place", From: was.Where, To: after.Where},
				tool.Change{What: "the notes", From: was.Notes, To: after.Notes},
			)
		},
	}
}

// agenda : What the assistant has written down.
func agenda(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:     "calendar_list",
		Prefetch: true,
		Purpose:  "Read what is in the diary, over the days ahead or across a particular stretch of dates.",
		UseWhen: "Any question about what is in the diary -- today, tomorrow, this week, a named " +
			"day, a range of dates, or a day already past. Call it every time, including when the " +
			"answer seems obvious: a date in the past is still a question about what is stored, and " +
			"the only way to know what is stored is to look.",
		Avoid: "Do not answer from what was said earlier in the conversation, and do not reason that " +
			"a date must be empty because it is in the past or because nothing was mentioned. Both are " +
			"claims about the diary, and a claim about the diary needs this tool to have just run.\n\n" +
			"What comes back is only what the assistant put there; it cannot see the person's own " +
			"meetings. So never call a day empty on the strength of it -- say nothing was written down " +
			"here, and use calendar_free for whether they are actually busy.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"from": {
					Type: "string",
					Description: "First day to look at, as 2026-09-03, or a moment as " +
						"2026-09-03T15:00. Given on its own it means that one day.",
				},
				"to": {
					Type: "string",
					Description: "Last day to look at, as 2026-09-05. The whole of that day is " +
						"included. Ignored without 'from'.",
				},
				"days": {
					Type: "integer",
					Description: "Instead of dates: how many days ahead to look, counting today. " +
						"One is the rest of today. Used only when 'from' is absent; left out, seven.",
				},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				From string `json:"from"`
				To   string `json:"to"`
				Days int    `json:"days"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			from, to, said, err := window(args.From, args.To, args.Days, clock)
			if err != nil {
				return tool.Failed(err.Error())
			}

			found, err := diary.Mine(ctx, in.Caller.UserID, from, to)
			if err != nil {
				return whenTrouble(err)
			}
			if len(found) == 0 {
				return tool.OK("Nothing is written in the diary " + said + ". That is only what " +
					"was put there through you; it says nothing about their own calendar.")
			}

			var b strings.Builder
			b.WriteString("In the diary " + said + ":")
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

// window : The stretch of time to read, and how to say which it was.
//
// Dates win over a count of days when both are given, because somebody
// who named a date meant it. A date on its own is that whole day, since
// "what is on the third" is a question about a day and not about the
// instant it begins.
//
// The description comes back with the times because the answer has to
// name the window it looked at. "Nothing in the diary" is a different
// statement about tomorrow than about the whole of last month, and a
// model given only the events cannot tell the person which it checked.
func window(from, to string, days int, clock Clock) (time.Time, time.Time, string, error) {
	loc := clock.where()

	if strings.TrimSpace(from) == "" {
		if days <= 0 {
			days = 7
		}
		start := clock.now()
		return start, start.AddDate(0, 0, days),
			fmt.Sprintf("over the next %d days", days), nil
	}

	start, err := when(from, loc)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}

	// A bare date means the whole of that day, so the end is the moment
	// the next one begins.
	end := start.AddDate(0, 0, 1)
	said := "on " + start.In(loc).Format("Monday 2 January")

	if strings.TrimSpace(to) != "" {
		last, err := when(to, loc)
		if err != nil {
			return time.Time{}, time.Time{}, "", err
		}
		if last.Before(start) {
			return time.Time{}, time.Time{}, "",
				fmt.Errorf("that range ends before it starts")
		}
		end = last.AddDate(0, 0, 1)
		said = "between " + start.In(loc).Format("Monday 2 January") +
			" and " + last.In(loc).Format("Monday 2 January")
	}

	return start, end, said, nil
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
func cancel(diary Diary, clock Clock) tool.Tool {
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

			// A wide window either side, so the count means something
			// whenever the event actually was.
			from := clock.now().AddDate(0, 0, -365)
			to := clock.now().AddDate(0, 0, 365)
			before := onTheDay(ctx, diary, in.Caller.UserID, from, to)

			if err := diary.Cancel(ctx, in.Caller.UserID, args.ID); err != nil {
				return whenTrouble(err)
			}

			after := onTheDay(ctx, diary, in.Caller.UserID, from, to)
			if among(after, args.ID) {
				return tool.Unverified("Taking that out of the diary", "it is still there")
			}
			return tool.Removed("Taken out of the diary", len(before), len(after), "event")
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

// onTheDay : What is in the diary across a window, or nothing when it
// cannot be read. A failure to count is not a failure to write, so it
// costs the count and not the operation.
func onTheDay(ctx context.Context, diary Diary, userID string, from, to time.Time) []calendar.Event {
	found, err := diary.Mine(ctx, userID, from, to)
	if err != nil {
		return nil
	}
	return found
}

// among : Whether an event with this identifier is in the list.
func among(events []calendar.Event, id string) bool {
	for _, e := range events {
		if e.ID == id {
			return true
		}
	}
	return false
}

// thing : "event" or "events", for a count read aloud.
func thing(n int) string {
	if n == 1 {
		return "event"
	}
	return "events"
}
