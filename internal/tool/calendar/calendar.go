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
	"github.com/DhanushRamesh/personal-assistant/internal/heard"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
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
	Theirs(ctx context.Context, userID, which string, from, to time.Time) (calendar.Owned, []calendar.Event, error)
	Everywhere(ctx context.Context, userID string, from, to time.Time) ([]calendar.Event, []string, error)
	Calendars(ctx context.Context, userID string) ([]calendar.Owned, error)
	Rename(ctx context.Context, userID, name string) (*calendar.Owned, error)
}

// All : Every calendar tool, in the order they are offered.
func All(diary Diary, clock Clock) []tool.Tool {
	return []tool.Tool{
		add(diary, clock),
		amend(diary, clock),
		agenda(diary, clock),
		free(diary, clock),
		cancel(diary, clock),
		calendars(diary),
		rename(diary),
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
		Domain:  "calendar",
		Writes:  true,
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
				"ends": {
					Type: "string",
					Description: "The last day it covers, as 2026-06-29, for something that runs " +
						"over several days. Use it with all_day for a stay or a trip: one event " +
						"across the days, never one event per day. Left out, it is a single day.",
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
				Ends    string `json:"ends"`
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

			// A stay over several days is one event across them, not one
			// event per day. Without this the only way to say "the 26th to
			// the 29th" was four separate entries, which is what the model
			// did, and then could not update them as one thing.
			if last := strings.TrimSpace(args.Ends); last != "" {
				until, err := when(last, clock.where())
				if err != nil {
					return tool.Failed(err.Error())
				}
				if until.Before(starts) {
					return tool.Failed("That ends before it starts. Give the later day as 'ends'.")
				}
				ends = until
			}

			// The whole span is read back, not only the first day, or a
			// multi-day event looks missing from every day but one.
			day := ends.AddDate(0, 0, 1)
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
			return tool.Added("Put in the diary", made.Title, describe(*made, clock.where()),
				before, len(after), "event")
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
		Domain:  "calendar",
		Writes:  true,
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
					Type:    "string",
					Pattern: eventPattern,
					Description: "The event's identifier, exactly as calendar_events gave it. " +
						"Never guessed, never a placeholder, and never a description of the event.",
				},
				"title":   {Type: "string", Description: "A new name for it."},
				"starts":  {Type: "string", Description: "A new start, as 2026-09-28T15:00 in their own time."},
				"minutes": {Type: "integer", Description: "A new length in minutes, counted from the start."},
				"ends": {Type: "string",
					Description: "A new last day, as 2026-06-29, for something that runs over " +
						"several days. This is how a one-day stay becomes a stay from the 26th " +
						"to the 29th: change the one event, rather than adding one per day."},
				"where": {Type: "string", Description: "A new place. An empty string clears it."},
				"notes": {Type: "string", Description: "New notes. An empty string clears them."},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string  `json:"id"`
				Title   *string `json:"title"`
				Starts  string  `json:"starts"`
				Minutes int     `json:"minutes"`
				Ends    string  `json:"ends"`
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
				return tool.Failed("Which event? Use calendar_events to find its identifier.")
			}

			// Read it first, both to have something to compare against
			// and so a wrong identifier is caught before anything is
			// written.
			before, err := diary.One(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				return whenTrouble(err)
			}
			if before == nil {
				return missingEvent(ctx, diary, clock, in.Caller.UserID, args.ID)
			}
			// Copied, not held by pointer. A Diary that hands back a
			// pointer into its own storage would otherwise have the
			// write mutate what we are comparing against, and the diff
			// would quietly show that nothing changed.
			was := *before

			change := calendar.Amend{Title: args.Title, Where: args.Where, Notes: args.Notes}
			// Carried from the event itself, because Google stores a
			// whole-day event as dates and a timed one as timestamps,
			// and it refuses a patch that sends the wrong one. Two
			// attempts to move a birthday failed this way on 28
			// September 2026 and were reported as the date being in
			// the past.
			change.AllDay = &was.AllDay
			if strings.TrimSpace(args.Starts) != "" {
				starts, err := when(args.Starts, clock.where())
				if err != nil {
					return tool.Failed(err.Error())
				}
				change.Starts = &starts

				// Moving the start alone keeps the length it had, which
				// is what somebody means by "move it to four".
				length := was.Ends.Sub(was.Starts)
				if args.Minutes > 0 && !was.AllDay {
					length = time.Duration(args.Minutes) * time.Minute
				}
				// A whole-day event has no length in minutes, and a
				// one-day one is a span of nothing: an hour's default
				// would turn a birthday into a morning.
				if length <= 0 && !was.AllDay {
					length = time.Hour
				}
				ends := starts.Add(length)
				change.Ends = &ends
			} else if args.Minutes > 0 && !was.AllDay {
				ends := was.Starts.Add(time.Duration(args.Minutes) * time.Minute)
				change.Ends = &ends
			}

			// A named last day wins over any length worked out above: it
			// is the one way to stretch an event across days rather than
			// leaving a row of one-day copies behind.
			if last := strings.TrimSpace(args.Ends); last != "" {
				until, err := when(last, clock.where())
				if err != nil {
					return tool.Failed(err.Error())
				}
				from := was.Starts
				if change.Starts != nil {
					from = *change.Starts
				}
				if until.Before(from) {
					return tool.Failed("That would end before it starts.")
				}
				change.Ends = &until
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
				tool.Change{What: "the time",
					From: was.Starts.In(loc).Format("3:04 pm on Monday 2 January"),
					To:   after.Starts.In(loc).Format("3:04 pm on Monday 2 January")},
				tool.Change{What: "the place", From: was.Where, To: after.Where},
				tool.Change{What: "the note", From: was.Notes, To: after.Notes},
			)
		},
	}
}

// agenda : What the assistant has written down.
func agenda(diary Diary, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "calendar_events",
		Domain: "calendar",
		Lists:  true,
		Purpose: prompt.Text(
			"Read events, over the days ahead or across a stretch of dates.",
			"Reads every calendar the person has -- their own, the assistant's, holidays, birthdays -- unless one is named in 'calendar', which narrows it to that one.",
			"All of them can be read; only the assistant's own can be changed.",
		),
		UseWhen: prompt.Block(
			prompt.Text(
				"Anything to do with a date or a time, whether it is asked about, mentioned in passing, or only discussed.",
				"The calendar is where dates and times live: a day, a month, a season, a birthday, an anniversary, a trip, a meeting, a deadline, next week, the weekend, before they leave.",
				"Read it before you answer, every time, even when nobody asked what is in the diary.",
				"What they said may already be written down, may clash with something, or may be the thing to offer to write down; none of that can be known without looking.",
			),
			prompt.Text(
				"Also any question about what is in the diary -- today, tomorrow, this week, a named day, a range of dates, or a day already past.",
				"Call it every time, including when the answer seems obvious: a date in the past is still a question about what is stored, and the only way to know what is stored is to look.",
			),
			prompt.Text(
				"Including when they name a calendar you do not recognise.",
				"The name reached you through speech and is more likely mangled than wrong, so pass it anyway and let it be matched.",
				"Never answer that a calendar does not exist without having looked: that is a claim about what they have, and it needs a tool to have just run.",
			),
			prompt.Text(
				"There is no limit on how far back or forward this reaches.",
				"'from' and 'to' take any dates, years apart if that is what was asked for, so never say a stretch of time is out of reach: pick the widest range that answers what they asked and read it.",
			),
		),
		Avoid: prompt.Block(
			prompt.Text(
				"Do not answer from what was said earlier in the conversation, and do not reason that a date must be empty because it is in the past or because nothing was mentioned.",
				"Both are claims about the diary, and a claim about the diary needs this tool to have just run.",
			),
			prompt.Text(
				"Do not narrow it without being asked to.",
				"Called without 'calendar' it reads everything they have, which is what a question about a day wants; naming one hides the rest, so name one only when they asked about that calendar in particular.",
			),
			prompt.Text(
				"Do not offer to check their own calendar: it has already been read.",
				"If some calendar would not open the answer says so by name, and that, not silence, is what to pass on.",
			),
		),
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
				// 'from' and 'to' take any dates at all, in either
				// direction and however far apart. Said once here because
				// the model has claimed the opposite: asked for everything
				// rather than a month, it answered that it could not reach
				// indefinitely into the past and future, which is not true
				// of anything in this schema.

				"calendar": {
					Type: "string",
					Description: prompt.Text(
						"Which single calendar to read, when they asked about one in particular.",
						"Left out, every calendar they have is read together, which is almost always what is wanted.",
						"Pass what the person called it, even if it sounds wrong: names spoken aloud arrive mangled and are matched by likeness, so \"rjdanesh22rjmail.com\" finds rjdhanush22@gmail.com and \"javas\" finds Jarvis.",
						"Their own main one answers to \"main\".",
						"Reading any of theirs is allowed; changing them is not.",
					),
				},
			},
		},
		Examples: []tool.Example{
			// A question nobody would call a diary question, which is
			// the point: a birthday is a date, the date is written
			// down, and answering from memory answers about what you
			// were told rather than about what exists. A year, because
			// a birthday is not in the next seven days.
			{Ask: "when is Alekhya's birthday", Args: `{"from":"2026-09-28","to":"2027-09-28","saying":"looking for that"}`},
			{Ask: "is anyone's birthday coming up", Args: `{"days":60,"saying":"having a look"}`},
			{Ask: "what have I got on", Args: `{"days":7,"saying":"looking at your diary"}`},
			{Ask: "what is on my own calendar this week", Args: `{"days":7,"calendar":"main","saying":"reading your own calendar"}`},
			{Ask: "read the events in RJDanajtvali.jml.com", Args: `{"days":30,"calendar":"RJDanajtvali.jml.com","saying":"reading that calendar"}`},
			{Ask: "what is in my gmail calendar in August", Args: `{"from":"2026-08-01","to":"2026-08-31","calendar":"gmail","saying":"reading August on your calendar"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				From     string `json:"from"`
				To       string `json:"to"`
				Days     int    `json:"days"`
				Calendar string `json:"calendar"`
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

			// Named, it is one of the person's own and read only. Left
			// out, it is the assistant's, which is the only one it can
			// change and the only one whose identifiers are worth having.
			if strings.TrimSpace(args.Calendar) != "" {
				on, found, err := diary.Theirs(ctx, in.Caller.UserID, args.Calendar, from, to)
				if errors.Is(err, calendar.ErrNoSuchCalendar) || errors.Is(err, calendar.ErrWhichCalendar) {
					return whichCalendar(ctx, diary, in.Caller.UserID, args.Calendar,
						errors.Is(err, calendar.ErrWhichCalendar))
				}
				if err != nil {
					return whenTrouble(err)
				}
				if len(found) == 0 {
					return tool.OK("There is nothing on " + on.Name + " " + said + ".")
				}

				var b strings.Builder
				fmt.Fprintf(&b, "There are %d entries on %s %s. ", len(found), on.Name, said)
				if on.Mine {
					// Naming your own calendar has to reach the same place
					// as not naming one. It did not: asked to read
					// "Personal Assistant" the assistant was handed its own
					// events with no identifiers and told it could not
					// change them, so it added more instead of mending what
					// was there.
					b.WriteString("This is your own calendar, the one you write to, so these can " +
						"be changed and removed.")
				} else {
					b.WriteString("This is the person's own calendar: you can read it and cannot " +
						"change it. Read the titles back exactly as they are written here, " +
						"misspellings and all: tidying one hides the fact that you are looking at " +
						"a different thing from the one they meant.")
				}
				if !heard.Exactly(on.Name, args.Calendar) {
					b.WriteString(" " + tool.BySound("calendar", args.Calendar, on.Name))
				}
				for _, e := range found {
					b.WriteString("\n- ")
					b.WriteString(describe(e, clock.where()))
					if on.Mine {
						b.WriteString("  [id " + e.ID + "]")
					}
				}
				return tool.OK(b.String())
			}

			// Everything they have, which is what a question about a
			// date means. Naming a calendar narrows it; naming none
			// used to narrow it to the assistant's own, which answered
			// "anything on the 2nd of October" with silence while the
			// holiday calendar had Gandhi Jayanti on it.
			found, missed, err := diary.Everywhere(ctx, in.Caller.UserID, from, to)
			if err != nil {
				return whenTrouble(err)
			}

			var b strings.Builder
			if len(found) == 0 {
				b.WriteString("Nothing is written " + said + ", on any of their calendars.")
			} else {
				fmt.Fprintf(&b, "There are %d entries %s, across all their calendars. ", len(found), said)
				b.WriteString("The calendar each one sits on is named after it, and it matters: " +
					"a holiday or a birthday is not something they arranged. Only entries marked " +
					"with an identifier can be changed or removed.")
				for _, e := range found {
					b.WriteString("\n- ")
					b.WriteString(describe(e, clock.where()))
					if e.Calendar != "" {
						b.WriteString("  (" + e.Calendar + ")")
					}
					if e.Mine {
						b.WriteString("  [id " + e.ID + "]")
					}
				}
			}
			// Said plainly, because a day called empty on the strength
			// of a calendar that would not open is a wrong answer
			// rather than a missing one.
			if len(missed) > 0 {
				b.WriteString("\n\nThese would not open, so this is not the whole picture: " +
					strings.Join(missed, ", ") + ".")
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
		Domain:  "calendar",
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
		Domain:  "calendar",
		Writes:  true,
		Purpose: "Remove an event the assistant put in the diary.",
		UseWhen: "They want something taken out that is listed by calendar_events. " +
			"Read the listing in this same turn first and take the identifier from it. " +
			"If you do not have one in front of you, you do not have one: list, then remove.",
		Avoid: "Only events on the assistant's own calendar can be removed, which is every event it " +
			"can see. There is no way to touch the person's real meetings and no point trying.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"ids"},
			Properties: map[string]tool.Property{
				"ids": {
					Type: "array", MinItems: tool.Bound(1), MaxItems: tool.Bound(25),
					Description: "Which events to remove. Give every one you mean in a single " +
						"call, not one call each: they are counted and read back together, so " +
						"the person is told once how many went rather than once per event.",
					Items: &tool.Property{Type: "string", Pattern: eventPattern,
						Description: "An event identifier, exactly as a listing gave it."},
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "cancel that event",
				Args: `{"ids":["3uhvjlv8681uhvr1kq69flvjt4"],"saying":"cancelling that event"}`},
			{Ask: "delete the four duplicate stays",
				Args: `{"ids":["v3fcn5cg5e5g03q5ivq2n98i60","if83k9l3falej69kl5tvt4sb34",` +
					`"4bk8kf1r6m4s13155skat7qlvg","hnpu9lqtprtpq469du2frd3vo0"],` +
					`"saying":"removing the four duplicate stays"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				IDs []string `json:"ids"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if diary == nil {
				return tool.Failed("There is no calendar on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			if len(args.IDs) == 0 {
				return tool.Failed("Which events? Use calendar_events to find their identifiers.")
			}

			// A wide window either side, so the count means something
			// whenever the events actually were.
			from := clock.now().AddDate(0, 0, -365)
			to := clock.now().AddDate(0, 0, 365)
			before := onTheDay(ctx, diary, in.Caller.UserID, from, to)

			// Every one is attempted. Stopping at the first failure would
			// leave the person with some of them gone and no account of
			// which, which is worse than finishing and saying so.
			var refused []string
			for _, id := range args.IDs {
				if err := diary.Cancel(ctx, in.Caller.UserID, id); err != nil {
					refused = append(refused, id)
				}
			}

			after := onTheDay(ctx, diary, in.Caller.UserID, from, to)

			// Read back rather than trusting the deletes. Anything still
			// there did not go, whatever its call reported.
			var stayed []string
			for _, id := range args.IDs {
				if among(after, id) {
					stayed = append(stayed, id)
				}
			}
			if len(stayed) == len(args.IDs) {
				return tool.Unverified("Taking those out of the diary", "they are all still there")
			}
			if len(stayed) > 0 {
				return tool.Partly("Taken out of the diary",
					len(args.IDs)-len(stayed), len(args.IDs), stayed,
					len(before), len(after), "event")
			}
			_ = refused
			return tool.RemovedMany("Taken out of the diary",
				len(args.IDs), len(before), len(after), "event")
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
	switch {
	case e.AllDay && spansDays(e):
		// A stay from the 26th to the 29th used to read as the 26th
		// alone, because only the start was said.
		b.WriteString(", from " + e.Starts.In(loc).Format("Monday 2 January") +
			" to " + e.Ends.In(loc).Format("Monday 2 January"))

	case e.AllDay && e.Kind == birthday:
		// No "all day". A birthday is a day, and saying so tells nobody
		// anything: "Meganadham's birthday today, all day".
		b.WriteString(", on " + e.Starts.In(loc).Format("Monday 2 January"))

	case e.AllDay:
		b.WriteString(", all day on " + e.Starts.In(loc).Format("Monday 2 January"))

	default:
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

// birthday : What Google calls a birthday, and the yearly things it
// files with them. Its own word, not one of ours, so it means the same
// whatever language the title is written in.
const birthday = "birthday"

// spansDays : Whether a whole-day event covers more than the one day.
//
// Ends is the inclusive last day for these, so equal dates are one day.
func spansDays(e calendar.Event) bool {
	return !e.Ends.IsZero() && e.Ends.After(e.Starts) &&
		e.Ends.YearDay() != e.Starts.YearDay()
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
