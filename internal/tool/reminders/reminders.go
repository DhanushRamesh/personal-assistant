// Package reminders lets the assistant say something at a time rather than
// because it was asked.
//
// A reminder only ever says something. There is no tool here for making one
// that runs an instruction, because a task firing with nobody watching has
// nobody to catch it.
package reminders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// idPattern : The shape of a reminder identifier, so one the model invented
// is refused before it reaches the database.
const idPattern = `^rem_[0-9A-HJKMNP-TV-Z]{26}$`

// Bounds on when something may be set for.
const (
	// MaxMinutes : The longest a relative reminder may be, in minutes.
	// A year, which is past anything anybody says out loud.
	MaxMinutes = 525600
	// MinSeconds : The shortest a reminder may be, in seconds.
	//
	// Below this it would fire while the assistant is still saying that it
	// has been set.
	MinSeconds = 5
	// MaxSeconds : The longest worth saying in seconds rather than minutes.
	MaxSeconds = 86400
	// MaxAhead : The furthest ahead an absolute one may be set.
	MaxAhead = 5 * 365 * 24 * time.Hour
	// DefaultSnooze : How long "snooze it" means when no length is said.
	//
	// Ten minutes, which is what the word means on every clock radio ever
	// made. Asking how long every time would be worse than being wrong
	// occasionally, and being wrong is one more sentence to put right.
	DefaultSnooze = 10 * time.Minute
	// Slack : How far in the past a time may be and still be taken as now.
	//
	// The model works the time out from what it was told, and a second or
	// two passes while it does. Refusing those would refuse "in a minute".
	Slack = 2 * time.Minute
)

// layouts : How a written time may be spelt.
//
// Several, because a model writes the same moment several ways and
// refusing it over a missing "T" would be refusing the reminder.
var layouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// Clock : What the tools need to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
	Location *time.Location
}

// now : The moment, in UTC.
func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

// where : The person's zone.
func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// All : Every reminder tool, in the order they are offered.
//
// Any of them may be said out loud. None destroys anything: a cancelled
// reminder that was wanted after all is set again in a sentence.
func All(store remind.Store, clock Clock) []tool.Tool {
	return []tool.Tool{
		set(store, clock),
		list(store, clock),
		recent(store, clock),
		snooze(store, clock),
		cancel(store),
	}
}

// DefaultRecentHours : How far back "did I miss anything" looks by default.
//
// An hour. Somebody stepping out and coming back is asking about the time
// they were gone, not about yesterday, and a recap that reaches further
// reads out things they were there for.
const DefaultRecentHours = 1

// MaxRecentHours : The furthest back it will look. A week, past which this
// is not a recap but a history, and reminder_list with include_finished is
// the honest way to ask for that.
const MaxRecentHours = 168

// recent : What happened while somebody was away.
func recent(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "reminder_recent",
		Purpose: "Say what reminders have happened lately: which were said, and which were never said at all.",
		UseWhen: "The person asks whether they missed anything, what they missed while they were out, or " +
			"what has already gone off.",
		Avoid: "This is about what has already happened. For what is still to come, that is reminder_list. " +
			"Say which of the three each one was: being told a thing, never being told it, and it still " +
			"waiting to be told are different, and running them together tells the person they heard " +
			"something they did not.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"hours": {
					Type: "integer",
					Description: "How far back to look. Leave out for the last hour, which is what " +
						"somebody who has just walked in is asking about.",
					Default: DefaultRecentHours,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "did I miss any reminders", Args: `{}`},
			{Ask: "what did I miss this morning", Args: `{"hours":6}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Hours int `json:"hours"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			hours := args.Hours
			if hours <= 0 {
				hours = DefaultRecentHours
			}
			if hours > MaxRecentHours {
				return tool.Failed(fmt.Sprintf(
					"hours was %d, and the most allowed is %d. For anything older, list them with "+
						"include_finished instead.", hours, MaxRecentHours))
			}

			since := clock.now().Add(-time.Duration(hours) * time.Hour)

			said, err := store.LastSpoken(ctx, in.Caller.UserID, since)
			if err != nil {
				return tool.Failed(err.Error())
			}
			missed, err := store.List(ctx, in.Caller.UserID, remind.Missed)
			if err != nil {
				return tool.Failed(err.Error())
			}
			held, err := store.Waiting(ctx, in.Caller.UserID)
			if err != nil {
				return tool.Failed(err.Error())
			}

			return tool.OK(recap(said, within(missed, since), held, hours, len(said), clock))
		},
	}
}

// within : The reminders whose time fell inside the window.
func within(all []remind.Reminder, since time.Time) []remind.Reminder {
	kept := make([]remind.Reminder, 0, len(all))
	for i := range all {
		if !all[i].DueAt.Before(since) {
			kept = append(kept, all[i])
		}
	}
	return kept
}

// recap : What happened lately, as the model is shown it.
//
// The two lists are kept apart and labelled. A reminder that was spoken and
// one that was never spoken are different facts, and a recap that runs them
// together tells somebody they heard a thing they did not.
func recap(said, missed, held []remind.Reminder, hours, spokenCount int, clock Clock) string {
	window := "the last hour"
	if hours != 1 {
		window = fmt.Sprintf("the last %d hours", hours)
	}

	if len(said) == 0 && len(missed) == 0 && len(held) == 0 {
		return "Nothing has been said and nothing was missed in " + window + "."
	}

	var b strings.Builder
	if len(said) > 0 {
		b.WriteString("Said out loud in " + window + ", most recent first:")
		for i := range said {
			b.WriteString("\n- ")
			b.WriteString(spell(spokenAt(said[i]), clock.where()))
			b.WriteString(": ")
			b.WriteString(said[i].Body)
		}
		// The store returns a bounded number. Saying so is better than
		// implying the list is everything.
		if spokenCount >= remind.DefaultSpokenLimit {
			b.WriteString("\n(That is as many as this returns; there may be more.)")
		}
	}
	if len(held) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Still waiting, kept back because nobody was in the room. " +
			"These have not been said yet and will be when the person is next greeted:")
		for i := range held {
			b.WriteString("\n- ")
			b.WriteString(spell(held[i].DueAt, clock.where()))
			b.WriteString(": ")
			b.WriteString(held[i].Body)
		}
	}
	if len(missed) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Never said at all, because nothing could say them at the time:")
		for i := range missed {
			b.WriteString("\n- ")
			b.WriteString(spell(missed[i].DueAt, clock.where()))
			b.WriteString(": ")
			b.WriteString(missed[i].Body)
		}
	}
	return b.String()
}

// spokenAt : The moment a reminder was said, or its due time if unrecorded.
func spokenAt(r remind.Reminder) time.Time {
	if r.LastFiredAt != nil {
		return *r.LastFiredAt
	}
	return r.DueAt
}

// snooze : Puts off something that has just been said, or is still to come.
func snooze(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "reminder_snooze",
		Purpose: "Put a reminder off until later, whether it has just gone off or is still to come.",
		UseWhen: "The person wants one again later: snooze it, not now, in ten minutes, remind me after lunch, " +
			"push my four o'clock back.",
		Avoid: "Leave id out when they mean the one just said -- that is the usual case, and it is worked " +
			"out here rather than guessed. Give id, from a listing, only for one they named that is still " +
			"to come. Do not use this to make a new reminder about something else: that is reminder_set.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"id": {
					Type: "string",
					Description: "The reminder's identifier, from a listing. Leave it out for the one " +
						"just said aloud, which is what \"that\" almost always means.",
					Pattern: idPattern,
				},
				"seconds_from_now": {
					Type:        "integer",
					Description: "For a length of time said in seconds.",
				},
				"minutes_from_now": {
					Type:        "integer",
					Description: "For a length of time said in minutes or hours. Ten minutes is 10, an hour is 60.",
				},
				"at": {
					Type: "string",
					Description: "For a time or a date, written as 2006-01-02 15:04 in the person's own " +
						"local time.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "snooze that", Args: `{}`},
			{Ask: "remind me again in twenty minutes", Args: `{"minutes_from_now":20}`},
			{Ask: "push my four o'clock back to five",
				Args: `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV0","at":"2026-09-27 17:00"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string `json:"id"`
				Seconds int    `json:"seconds_from_now"`
				Minutes int    `json:"minutes_from_now"`
				At      string `json:"at"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			until, fail := laterBy(clock, args.Seconds, args.Minutes, args.At)
			if fail != "" {
				return tool.Failed(fail)
			}

			existing, fail := which(ctx, store, clock, in.Caller.UserID, args.ID)
			if fail != "" {
				return tool.Failed(fail)
			}

			added, err := remind.Later(ctx, store, existing, until)
			if err != nil {
				if errors.Is(err, remind.ErrNotSnoozable) {
					return tool.Failed(fmt.Sprintf(
						"%q was %s, so there is nothing to put off. Set it again instead.",
						existing.Title, existing.Status))
				}
				return tool.Failed(err.Error())
			}

			// A repeating one was not moved, and saying it was would have
			// the assistant report that the alarm had shifted when the
			// alarm is exactly where it was.
			if added != nil {
				return tool.OK(fmt.Sprintf(
					"%q repeats %s, and a repeating reminder is not moved: putting its time back "+
						"would put every one after it back too. So it is unchanged, still due at %s, "+
						"and a single extra one was added for %s. Tell the person both, briefly and "+
						"in their words.",
					existing.Title, existing.Repeats, spell(existing.DueAt, clock.where()),
					spell(until, clock.where())))
			}
			return tool.OK(fmt.Sprintf("Put off: %q will now be said at %s. Tell the person when, "+
				"in their words rather than as a date.", existing.Title, spell(until, clock.where())))
		},
	}
}

// which : The reminder being put off, or why it cannot be worked out.
//
// With no identifier it is the one just said. Several said together is
// the case that must not be answered by picking: the person is asked.
func which(ctx context.Context, store remind.Store, clock Clock, userID, id string) (*remind.Reminder, string) {
	if id != "" {
		got, err := store.Get(ctx, userID, id)
		if err != nil {
			if errors.Is(err, remind.ErrNotFound) {
				return nil, fmt.Sprintf(
					"There is no reminder with the identifier %s. List them rather than guessing.", id)
			}
			return nil, err.Error()
		}
		return got, ""
	}

	spoken, err := store.LastSpoken(ctx, userID, clock.now().Add(-remind.JustSaidWindow))
	if err != nil {
		return nil, err.Error()
	}

	switch len(spoken) {
	case 0:
		return nil, "Nothing has been said aloud in the last few minutes, so there is no " +
			"\"that\" to put off. Ask which reminder they mean, or list them and ask."
	case 1:
		return &spoken[0], ""
	default:
		return nil, "More than one was said just now, so which is meant cannot be told from " +
			"\"that\". Ask which, naming them:\n" + describe(spoken, clock)
	}
}

// laterBy : When a put-off reminder comes back.
//
// Nothing said means DefaultSnooze. "Snooze it" is a whole sentence and
// asking how long would be answering a question with a question.
func laterBy(clock Clock, seconds, minutes int, written string) (time.Time, string) {
	if seconds <= 0 && minutes <= 0 && strings.TrimSpace(written) == "" {
		return clock.now().Add(DefaultSnooze), ""
	}
	return when(clock, seconds, minutes, written)
}

// set : Arranges for something to be said later.
func set(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "reminder_set",
		Purpose: "Arrange for something to be said out loud at a time, once or repeatedly.",
		UseWhen: "The person asks to be reminded, wants a timer, or wants telling at a time or on a day.",
		Avoid: "Give exactly one of seconds_from_now, minutes_from_now or at. Use one of the first two for " +
			"anything said as a length of time, so the arithmetic is not yours to get wrong: do not " +
			"convert seconds into minutes, and never refuse a length because it is not a whole number " +
			"of minutes. Do not use this to write something down for later reference: that is " +
			"memory_remember.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"title", "say"},
			Properties: map[string]tool.Property{
				"title": {
					Type:        "string",
					Description: "A few words naming it, for a listing and for cancelling it later. Not what gets said.",
				},
				"say": {
					Type:        "string",
					Description: "Exactly what should be spoken when the time comes, as a whole sentence. It is read out with nothing around it, so make it make sense on its own.",
				},
				"seconds_from_now": {
					Type:    "integer",
					Minimum: tool.Bound(MinSeconds), Maximum: tool.Bound(MaxSeconds),
					Description: "For a length of time said in seconds. Thirty seconds is 30, ninety seconds is 90.",
				},
				"minutes_from_now": {
					Type:    "integer",
					Minimum: tool.Bound(1), Maximum: tool.Bound(MaxMinutes),
					Description: "For a length of time said in minutes or hours. Twenty minutes is 20, two hours is 120.",
				},
				"at": {
					Type: "string",
					Description: "For a time or a date, written as 2006-01-02 15:04 in the person's own local time. " +
						"You are told the current local time; work it out from that. Leave out if using minutes_from_now.",
				},
				"repeats": {
					Type: "string", Enum: repeatWords(),
					Description: "How often it comes back. Leave out for once only.",
				},
				"scope": {
					Type: "string", Enum: []string{string(remind.ScopeUser), string(remind.ScopeClient)},
					Description: "user follows the person and is almost always right. client ties it to the device this was asked on.",
					Default:     string(remind.ScopeUser),
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "set a timer for twenty minutes",
				Args: `{"title":"Timer","say":"Your twenty minute timer has finished.","minutes_from_now":20}`},
			{Ask: "set a timer for thirty seconds",
				Args: `{"title":"Timer","say":"Your thirty second timer has finished.","seconds_from_now":30}`},
			{Ask: "remind me to call the roofer at half past four",
				Args: `{"title":"Call the roofer","say":"Time to call the roofer.","at":"2026-09-26 16:30"}`},
			{Ask: "wake me at seven every weekday",
				Args: `{"title":"Wake up","say":"It is seven o'clock.","at":"2026-09-28 07:00","repeats":"weekdays"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Title   string `json:"title"`
				Say     string `json:"say"`
				Seconds int    `json:"seconds_from_now"`
				Minutes int    `json:"minutes_from_now"`
				At      string `json:"at"`
				Repeats string `json:"repeats"`
				Scope   string `json:"scope"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person, so there is nobody to remind.")
			}

			due, fail := when(clock, args.Seconds, args.Minutes, args.At)
			if fail != "" {
				return tool.Failed(fail)
			}

			scope := remind.Scope(args.Scope)
			if args.Scope == "" {
				scope = remind.ScopeUser
			}
			if scope == remind.ScopeClient && in.Caller.ClientID == "" {
				return tool.Failed("This did not come from a known device, so it cannot be tied to one. Set it without a scope.")
			}

			r, err := remind.New(in.Caller.UserID, in.Caller.ClientID, scope,
				args.Title, args.Say, due, remind.Repeat(args.Repeats))
			if err != nil {
				return tool.Failed(err.Error())
			}
			if err := store.Create(ctx, r); err != nil {
				return tool.Failed(err.Error())
			}

			return tool.OK(fmt.Sprintf("Set: %q, %s%s. Its identifier is %s. Tell the person when it will happen, "+
				"in their words rather than as a date.",
				r.Title, spell(r.DueAt, clock.where()), repeating(r.Repeats), r.ID))
		},
	}
}

// list : What is waiting to be said.
func list(store remind.Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "reminder_list",
		Purpose: "List what is waiting to be said, soonest first, with their identifiers.",
		UseWhen: "Any question about what reminders or timers exist, without exception: what they have, " +
			"what is left, whether anything is coming, what is set for a particular day. Also when you " +
			"need an identifier in order to cancel or put one off.",
		Avoid: "Never answer from the conversation or from a quoted exchange. Your own earlier answer about " +
			"reminders was true when you gave it and is not evidence of anything now: the ones it named " +
			"have since gone off, been put off or been called off. There is no question about what is " +
			"waiting that you may answer without calling this first. Do not call it twice in one turn, " +
			"though: nothing changes while you are answering.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"include_finished": {
					Type:        "boolean",
					Description: "Also list ones that have already happened, been missed or been called off.",
					Default:     false,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what timers do I have", Args: `{}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Finished bool `json:"include_finished"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			states := []remind.Status{remind.Pending}
			if args.Finished {
				states = nil
			}

			found, err := store.List(ctx, in.Caller.UserID, states...)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if len(found) == 0 {
				return tool.OK("There is nothing waiting to be said.")
			}
			return tool.OK(describe(found, clock))
		},
	}
}

// cancel : Calls one off.
func cancel(store remind.Store) tool.Tool {
	return tool.Tool{
		Name:     "reminder_cancel",
		Purpose:  "Call off something that was going to be said.",
		UseWhen:  "The person asks to cancel or stop a reminder or timer.",
		Avoid:    "Do not guess the identifier: list them first. If more than one could be the one they mean, ask which.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type: "string", Description: "The reminder's identifier, from a listing.",
					Pattern: idPattern,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "cancel that timer", Args: `{"id":"rem_01M3D477HXQ4YNQX7BNXJZZCV0"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if store == nil {
				return tool.Failed("There is nowhere to keep reminders on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			existing, err := store.Get(ctx, in.Caller.UserID, args.ID)
			if err != nil {
				if errors.Is(err, remind.ErrNotFound) {
					return tool.Failed(fmt.Sprintf(
						"There is no reminder with the identifier %s. List them rather than guessing.", args.ID))
				}
				return tool.Failed(err.Error())
			}
			if err := store.Cancel(ctx, in.Caller.UserID, args.ID); err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(fmt.Sprintf("Called off %q.", existing.Title))
		},
	}
}

// when : The moment a reminder is due, or why it cannot be worked out.
//
// Minutes are preferred to a written time wherever the person said a
// length, because the arithmetic is then the server's rather than the
// model's.
func when(clock Clock, seconds, minutes int, written string) (time.Time, string) {
	now := clock.now()
	written = strings.TrimSpace(written)

	var given int
	for _, set := range []bool{seconds > 0, minutes > 0, written != ""} {
		if set {
			given++
		}
	}

	switch {
	case given > 1:
		return time.Time{}, "Give exactly one of seconds_from_now, minutes_from_now or at. Which was meant?"

	case seconds > 0:
		switch {
		case seconds < MinSeconds:
			return time.Time{}, fmt.Sprintf(
				"seconds_from_now was %d, and the least allowed is %d: anything shorter goes off while you are still saying it is set.",
				seconds, MinSeconds)
		case seconds > MaxSeconds:
			return time.Time{}, fmt.Sprintf(
				"seconds_from_now was %d, and the most allowed is %d. Use minutes_from_now for anything longer.",
				seconds, MaxSeconds)
		}
		return now.Add(time.Duration(seconds) * time.Second), ""

	case minutes > 0:
		if minutes > MaxMinutes {
			return time.Time{}, fmt.Sprintf("minutes_from_now was %d, and the most allowed is %d.", minutes, MaxMinutes)
		}
		return now.Add(time.Duration(minutes) * time.Minute), ""

	case written != "":
		at, ok := parse(written, clock.where())
		if !ok {
			return time.Time{}, fmt.Sprintf(
				"%q is not a time this understands. Write it as 2006-01-02 15:04 in the person's local time.", written)
		}
		switch {
		case at.Before(now.Add(-Slack)):
			return time.Time{}, fmt.Sprintf(
				"%s is in the past. The current local time is %s; work it out from that.",
				spell(at, clock.where()), clock.now().In(clock.where()).Format("2006-01-02 15:04"))
		case at.After(now.Add(MaxAhead)):
			return time.Time{}, "That is further ahead than this will hold. Was the year right?"
		}
		return at, ""
	}

	return time.Time{}, "Say when: either minutes_from_now for a length of time, or at for a time of day."
}

// parse : A written time, read in the person's own zone.
func parse(written string, loc *time.Location) (time.Time, bool) {
	for _, layout := range layouts {
		if at, err := time.ParseInLocation(layout, written, loc); err == nil {
			return at.UTC(), true
		}
	}
	// A model may write the zone in. Taken at its word when it does.
	if at, err := time.Parse(time.RFC3339, written); err == nil {
		return at.UTC(), true
	}
	return time.Time{}, false
}

// describe : Reminders as the model is shown them.
func describe(found []remind.Reminder, clock Clock) string {
	var b strings.Builder
	b.WriteString("Soonest first. Each line is an identifier, when it happens, and what it says.\n")
	for i := range found {
		r := found[i]
		b.WriteString("\n")
		b.WriteString(r.ID)
		b.WriteString("  ")
		b.WriteString(spell(r.DueAt, clock.where()))
		b.WriteString(repeating(r.Repeats))
		if r.Status != remind.Pending {
			b.WriteString("  [" + string(r.Status) + "]")
		}
		b.WriteString("  ")
		b.WriteString(r.Title)
		b.WriteString(": ")
		b.WriteString(r.Body)
	}
	return b.String()
}

// spell : A moment written out in the person's own zone.
func spell(at time.Time, loc *time.Location) string {
	return at.In(loc).Format("3:04 pm on Monday 2 January 2006")
}

// repeating : How often it comes back, as a phrase, or nothing.
func repeating(r remind.Repeat) string {
	if r == remind.Once {
		return ""
	}
	return ", repeating " + string(r)
}

// repeatWords : The repeats a caller may choose.
func repeatWords() []string {
	out := make([]string, 0, len(remind.Repeats()))
	for _, r := range remind.Repeats() {
		out = append(out, string(r))
	}
	return out
}
