// Package places : Lets the assistant answer where the person has been.
//
// It could be worked out from the raw events, and that is exactly what
// went wrong. Asked "do you know where I went today", the assistant
// called for every event of the day, was handed forty-two entries of
// which forty were a laptop's wifi reconnecting, and answered: "You
// left home at ten twenty-two and came back at ten fifty, sir. Since
// then you have been at home, moving between the Dhanush and
// Dhanush_EXT networks."
//
// The first half was right and the second half was a network adapter.
// The assistant had no way to ask the question that was asked, so it
// assembled an answer out of what it could reach. This is that
// question, asked properly: one list of places and the times, with
// nothing in it that is not somewhere they were.
package places

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/event"

	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// DefaultDays : How far back a question reaches when it does not say.
//
// Today. "Where did I go" means today unless somebody says otherwise,
// and a week of places read aloud is not an answer to it.
const DefaultDays = 1

// MostDays : The furthest back one call will look.
const MostDays = 90

// Reader : Where the events are read from.
type Reader interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Clock : Now, and where the person is, so a day means their day.
type Clock struct {
	Now      func() time.Time
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// Tools : Everything in this package.
func Tools(reader Reader, clock Clock) []tool.Tool {
	return []tool.Tool{visits(reader, clock)}
}

// visit : One stretch of being somewhere.
type visit struct {
	where    string
	from, to time.Time
	// open : Still there, as far as anything knows.
	open bool
	// unreported : Open for so long that the departure was missed
	// rather than not yet made. Said as such, because an invented
	// leaving time is worse than admitting there is none.
	unreported bool
}

// visits : Where the person has been.
func visits(reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "place_visits",
		Domain:  "event",
		Lists:   true,
		Purpose: "Read where the person has been, when they arrived and left, and how long they stayed.",
		UseWhen: "Any question about where they went, where they are, how long they were somewhere, " +
			"when they got in or out, or whether they have been somewhere lately. Also before " +
			"remarking on their day unprompted.",
		Avoid: "Not for what they said about where they were going, which is the conversation, and " +
			"not for what is planned, which is the calendar. Do not work this out from event_recent: " +
			"that carries every network a laptop joined and answering from it reads out wifi names.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"days": {
					Type: "integer",
					Description: "How many days back to look, ending today. 1 is today only, " +
						"7 is the past week. Use a large number to ask when they were last somewhere.",
					Default: DefaultDays,
				},
				"place": {
					Type: "string",
					Description: "Only somewhere whose name contains this, such as office. " +
						"Leave it out for everywhere.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "do you know where I went today?", Args: `{"days": 1}`},
			{Ask: "how long was I at the office this week?", Args: `{"days": 7, "place": "office"}`},
			{Ask: "when did I last go to the badminton court?", Args: `{"days": 90, "place": "badminton"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Days  int    `json:"days"`
				Place string `json:"place"`
			}
			if len(in.Args) > 0 {
				if err := json.Unmarshal(in.Args, &args); err != nil {
					return tool.Failed("those arguments could not be read: " + err.Error())
				}
			}
			days := args.Days
			if days <= 0 {
				days = DefaultDays
			}
			if days > MostDays {
				days = MostDays
			}

			now := clock.now()
			where := clock.where()
			// From the start of their day, not from this time of day
			// so many days ago. "Today" means since midnight.
			midnight := time.Date(now.In(where).Year(), now.In(where).Month(), now.In(where).Day(),
				0, 0, 0, 0, where)
			since := midnight.AddDate(0, 0, -(days - 1))

			// A crossing can precede the window by a long way: somebody
			// who has been at home since last night entered it
			// yesterday. Read further back than asked and trim after.
			found, err := reader.Recent(ctx, in.Caller.UserID, event.Query{
				Prefix: "place.", Since: since.Add(-7 * 24 * time.Hour)})
			if err != nil {
				return tool.Failed("their movements could not be read: " + err.Error())
			}

			been := gather(found, now)
			been = within(been, since, args.Place)
			if len(been) == 0 {
				return tool.OK(nothing(days, args.Place, where, since))
			}
			return tool.Reference(describe(been, where, days))
		},
	}
}

// gather : Every stretch of being somewhere, from both the places they
// drew and the ones worked out from their positions.
//
// A stay inside a geofence carries that geofence's name and the same
// hours, so it would otherwise be listed twice. The crossing wins where
// they overlap: it is the record of the boundary actually being
// crossed, where the stay is an inference from scattered readings.
func gather(found []event.Event, now time.Time) []visit {
	var crossings, stays []event.Event
	for _, e := range found {
		switch e.Kind {
		case event.Entered, event.Exited:
			crossings = append(crossings, e)
		case event.Stayed:
			stays = append(stays, e)
		}
	}

	out := make([]visit, 0, len(crossings)+len(stays))
	for _, f := range event.Fences(crossings, now) {
		still := !f.To.Before(now)
		out = append(out, visit{
			where: f.Name, from: f.From, to: f.To,
			open:       still,
			unreported: still && now.Sub(f.From) > event.Lingering,
		})
	}

	for _, e := range stays {
		var into struct {
			Value   string `json:"value"`
			Minutes int    `json:"minutes"`
		}
		if err := json.Unmarshal(e.Payload, &into); err != nil || into.Value == "" {
			continue
		}
		s := visit{
			where: into.Value,
			from:  e.OccurredAt,
			to:    e.OccurredAt.Add(time.Duration(into.Minutes) * time.Minute),
		}
		if !covered(s, out) {
			out = append(out, s)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].from.Before(out[j].from) })
	return out
}

// covered : Whether this stretch is already there under the same name.
func covered(s visit, already []visit) bool {
	for _, v := range already {
		if !strings.EqualFold(v.where, s.where) {
			continue
		}
		if s.from.Before(v.to) && v.from.Before(s.to) {
			return true
		}
	}
	return false
}

// within : The stretches that touch the window, and match the name
// asked for.
func within(been []visit, since time.Time, place string) []visit {
	place = strings.ToLower(strings.TrimSpace(place))
	out := make([]visit, 0, len(been))
	for _, v := range been {
		if v.to.Before(since) {
			continue
		}
		if place != "" && !strings.Contains(strings.ToLower(v.where), place) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// describe : The visits, as somebody would read them out.
func describe(been []visit, where *time.Location, days int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s they were somewhere, over %s. Times are their own.\n",
		len(been), noun("place", len(been)), window(days))

	day := ""
	for _, v := range been {
		if d := v.from.In(where).Format("Monday 2 January"); d != day {
			day = d
			b.WriteString("\n" + d + "\n")
		}
		fmt.Fprintf(&b, "  %s\n", line(v, where))
	}
	return strings.TrimSpace(b.String())
}

// line : One visit.
func line(v visit, where *time.Location) string {
	from := v.from.In(where).Format("15:04")
	if v.unreported {
		return fmt.Sprintf("%s, from %s, with no leaving reported since -- do not say they are "+
			"still there, say the departure was never recorded", v.where, from)
	}
	if v.open {
		return fmt.Sprintf("%s, from %s and still there", v.where, from)
	}
	return fmt.Sprintf("%s, %s to %s (%s)",
		v.where, from, v.to.In(where).Format("15:04"), spoken(v.to.Sub(v.from)))
}

// nothing : What to say when they went nowhere worth recording.
//
// Which is a real answer and not a failure: somebody who stayed in all
// day went nowhere, and a day with no readings at all is a phone that
// was off. The two are different and the difference is said, because
// "you did not go anywhere" about a day nobody watched is a claim that
// was never checked.
func nothing(days int, place string, where *time.Location, since time.Time) string {
	if place != "" {
		return fmt.Sprintf("Nothing matching %q over %s. Say they have not been there, "+
			"and only about that stretch.", place, window(days))
	}
	return fmt.Sprintf("Nowhere recorded over %s, since %s. That means no arrival or departure "+
		"was reported, which is what a day at home looks like and also what a phone that was "+
		"off looks like. Do not say where they were; say nothing was recorded.",
		window(days), since.In(where).Format("Monday 2 January"))
}

// window : The stretch asked about, in words.
func window(days int) string {
	switch {
	case days <= 1:
		return "today"
	case days == 7:
		return "the past week"
	default:
		return fmt.Sprintf("the past %d days", days)
	}
}

// spoken : How long, roughly, in the words somebody would use.
func spoken(d time.Duration) string {
	minutes := int(d.Round(time.Minute).Minutes())
	switch {
	case minutes < 1:
		return "a moment"
	case minutes < 60:
		return fmt.Sprintf("%d %s", minutes, noun("minute", minutes))
	case minutes%60 == 0:
		return fmt.Sprintf("%d %s", minutes/60, noun("hour", minutes/60))
	default:
		return fmt.Sprintf("%d %s %d %s", minutes/60, noun("hour", minutes/60),
			minutes%60, noun("minute", minutes%60))
	}
}

// noun : A noun for a count, by the ordinary rule.
func noun(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
