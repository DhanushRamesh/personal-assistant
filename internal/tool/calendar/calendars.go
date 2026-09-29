package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/calendar"
	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// calendars : The calendars themselves, as opposed to what is in them.
//
// A separate tool because a calendar and an event are different things and
// the model was reading them as one. Asked to list the calendars it had, it
// searched the conversations instead -- reasonably, since nothing offered
// to list calendars and the tool that reads events is called calendar_events.
func calendars(diary Diary) tool.Tool {
	return tool.Tool{
		Name:    "calendar_calendars",
		Domain:  "calendar",
		Lists:   true,
		Purpose: "List the person's calendars themselves -- their names and which one the assistant writes to. Not what is in them.",
		UseWhen: "They ask what calendars they have, what a calendar is called, or which one you keep things in. " +
			"Also before renaming one, so the name you are changing is one that exists.",
		Avoid: "This is not for events. What is in a calendar comes from calendar_events, and whether they are busy from calendar_free. " +
			"Listing calendars says nothing at all about what is written in them.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "what calendars do I have", Args: `{"saying":"listing your calendars"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			held, err := diary.Calendars(ctx, in.Caller.UserID)
			if err != nil {
				return whenTrouble(err)
			}
			if len(held) == 0 {
				return tool.OK("There are no calendars on this account.")
			}
			return tool.OK(describeCalendars(held))
		},
	}
}

// describeCalendars : The calendars as the model should read them.
//
// The count leads, so the number of calendars is something the model was
// told rather than something it counts off a list that may have been cut
// short. What may be done with each is stated for the same reason: read
// access looks like write access until something is refused.
func describeCalendars(held []calendar.Owned) string {
	var b strings.Builder
	fmt.Fprintf(&b, "There are %d calendars in total. ", len(held))
	b.WriteString("Only the one marked as yours can be written to; the rest you can read but not change.\n")
	for _, c := range held {
		b.WriteString("\n")
		b.WriteString(c.Name)
		if c.Mine {
			b.WriteString("  -- this is the one you write to")
		} else {
			b.WriteString("  -- the person's own, read only to you (" + c.Role + ")")
		}
	}
	return b.String()
}

// rename : Changes what the assistant's own calendar is called.
func rename(diary Diary) tool.Tool {
	return tool.Tool{
		Name:    "calendar_rename",
		Domain:  "calendar",
		Writes:  true,
		Purpose: "Rename the calendar the assistant writes to.",
		UseWhen: "They ask for your calendar to be called something else.",
		Avoid: "You can only rename your own calendar. Their other calendars, including their main one, " +
			"cannot be renamed from here: say so rather than trying. Renaming changes the calendar's name " +
			"and nothing in it, so do not use it to change an event.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"name"},
			Properties: map[string]tool.Property{
				"name": {
					Type:        "string",
					Description: "What the calendar should be called from now on.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "rename your calendar to Personal Assistant", Args: `{"name":"Personal Assistant","saying":"renaming my calendar"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(in.Args, &args)
			if strings.TrimSpace(args.Name) == "" {
				return tool.Failed("A calendar needs a name. Ask what it should be called.")
			}

			// What it was called, read before the change so the answer can
			// say what moved rather than only what it is now.
			was := ""
			if before, err := diary.Calendars(ctx, in.Caller.UserID); err == nil {
				for _, c := range before {
					if c.Mine {
						was = c.Name
						break
					}
				}
			}

			if _, err := diary.Rename(ctx, in.Caller.UserID, args.Name); err != nil {
				return whenTrouble(err)
			}

			// Read back rather than trusting the write, as with every other
			// change: the listing is what anybody else would see.
			after, err := diary.Calendars(ctx, in.Caller.UserID)
			if err != nil {
				return tool.Unverified("Renaming the calendar", "it cannot be read back")
			}
			now := ""
			for _, c := range after {
				if c.Mine {
					now = c.Name
					break
				}
			}
			if now == "" {
				return tool.Unverified("Renaming the calendar", "it is no longer in the list")
			}
			return tool.Changed("Renamed the calendar",
				tool.Change{What: "the name", From: was, To: now})
		},
	}
}

// whichCalendar : What to say when a spoken name did not settle on one
// calendar.
//
// The names are fetched here rather than left to the model, which has
// been seen to refuse without calling anything at all.
func whichCalendar(ctx context.Context, diary Diary, userID, said string, several bool) tool.Result {
	held, err := diary.Calendars(ctx, userID)
	if err != nil {
		return tool.Failed("The calendars could not be read, so which one was meant is unknown.")
	}
	names := make([]string, 0, len(held))
	for _, c := range held {
		names = append(names, c.Name)
	}
	if several {
		return tool.WhichOfThese("calendar", said, names)
	}
	return tool.WhichOne("calendar", said, names)
}

// missingEvent : What to say when an identifier names no event.
//
// The diary is read here rather than the model being sent to read it. A
// model told to look something up before answering has been seen to skip
// the looking and answer that the thing does not exist, which about a
// diary is a claim it cannot support.
func missingEvent(ctx context.Context, diary Diary, clock Clock, userID, said string) tool.Result {
	from, to, _, err := window("", "", 30, clock)
	if err != nil {
		return tool.Failed("There is no event with that identifier.")
	}
	found, err := diary.Mine(ctx, userID, from, to)
	if err != nil || len(found) == 0 {
		return tool.Failed("There is no event with that identifier, and nothing is in the diary " +
			"over the next thirty days.")
	}

	names := make([]string, 0, len(found))
	for _, e := range found {
		names = append(names, e.Title+" ["+e.ID+"]")
	}
	return tool.WhichOne("event", said, names)
}

// eventPattern : The shape of a Google event identifier.
//
// Lower-case letters and digits, and long enough not to be a word. It is
// here because a model without one to hand will write something that reads
// like an identifier instead: a call was once made with the literal
// "<identifier_for_the_existing_event>", which Google refused for the
// wrong reason. Refusing it here says what is actually wrong.
// Go's regexp refuses a repeat count above 1000, and a pattern it
// cannot compile is skipped silently rather than failing loudly -- so
// {5,1024} meant this was never checked at all. Google's identifiers
// are well under a hundred characters.
const eventPattern = `^[a-z0-9_]{5,256}$`
