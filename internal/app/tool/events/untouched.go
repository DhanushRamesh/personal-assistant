package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/event"
)

// Evening, Noon : The window a night is looked for in, in the person's
// own zone.
//
// From six in the evening to two the following afternoon. Wide enough
// for somebody who goes to bed at three and gets up at nine, and it has
// to be: a window that assumed a sensible bedtime would cut his night
// in half on the nights that matter.
const (
	Evening = 18
	Noon    = 14
)

// untouched : When the phone lay untouched, which is the nearest thing
// to when they slept.
//
// Written because the model could not do it. Asked how long he had
// slept, with every screen.locked and screen.unlocked of the night
// available to it, it answered "roughly two to three hours" -- reasoned
// from a habit he had once mentioned rather than from the evidence --
// and described the rest as "fragmentary activity through the night".
// The fragments were a five-and-three-quarter-hour hole in the middle
// of them.
//
// A model will not reliably find the longest gap in two hundred
// timestamps, and no wording makes it. So the gap is found here and
// handed over as a fact, which is the division this server already
// keeps everywhere else.
func untouched(reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "event_untouched",
		Domain: "event",
		Purpose: "The longest stretch a night in which the person's phone was not touched at " +
			"all, which is the nearest thing there is to when they slept.",
		UseWhen: "Any question about their sleep: how long they slept, when they went to bed, " +
			"when they woke, whether they got enough. Nothing records sleep itself, and this " +
			"is what stands in for it -- a phone lying still all night. Prefer it over reading " +
			"the night's events yourself: the answer is a gap between two timestamps among " +
			"hundreds, and finding it is this tool's job, not yours.",
		Avoid: "Not for what they did during the day, which is event_recent. This answers one " +
			"question about one night.",
		WhenUnasked: "Their sleep is theirs to raise. Say nothing about it unless they ask, or " +
			"unless something about the day genuinely turns on it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"night": {
					Type: "string",
					Description: "The evening the night began, as YYYY-MM-DD. Last night is the " +
						"default, so leave it out unless they named a different one. " +
						"The night of the 13th into the 14th is \"2026-10-13\".",
				},
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Night string `json:"night"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if reader == nil {
				return tool.Failed("Nothing on this server is collecting what your devices see.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			where := clock.where()
			began, err := nightOf(args.Night, clock)
			if err != nil {
				return tool.Failed("That is not a date I can read. Give it as YYYY-MM-DD.")
			}
			from := time.Date(began.Year(), began.Month(), began.Day(), Evening, 0, 0, 0, where)
			to := time.Date(began.Year(), began.Month(), began.Day(), Noon, 0, 0, 0, where).AddDate(0, 0, 1)

			kinds := make([]string, 0, len(event.Touches))
			for kind := range event.Touches {
				kinds = append(kinds, kind)
			}
			sort.Strings(kinds)

			found, err := reader.Recent(ctx, in.Caller.UserID, event.Query{
				Since: from.UTC(), Until: to.UTC(), Kinds: kinds, Limit: 5000,
			})
			if err != nil {
				return tool.Broke("What their devices reported that night could not be read.", err)
			}
			touched := make([]time.Time, 0, len(found))
			for _, e := range found {
				touched = append(touched, e.OccurredAt)
			}
			quiet, ok := event.Quiet(touched, from.UTC(), to.UTC())
			if !ok {
				return tool.OK(fmt.Sprintf(
					"Nothing was reported from their phone on the night of %s, so there is "+
						"nothing to say about when they slept. Say that, rather than guessing "+
						"from anything they have mentioned before.",
					began.Format("Monday 2 January")))
			}
			return tool.OK(saying(quiet, began, where))
		},
	}
}

// saying : The quiet stretch as a sentence, said as what was measured.
//
// The phone was untouched; the person was probably asleep and may have
// been reading in the next room. The assistant may draw the obvious
// conclusion out loud -- it is obvious -- but it is not to report it as
// a measurement, so the measurement is what this hands over.
func saying(q event.Untouched, night time.Time, where *time.Location) string {
	var b strings.Builder
	from, to := q.From.In(where), q.To.In(where)
	fmt.Fprintf(&b, "On the night of %s their phone lay untouched from %s to %s, which is %s.",
		night.Format("Monday 2 January"),
		from.Format("3:04 pm"), to.Format("3:04 pm"),
		span(q.Long().Seconds()))
	if q.Clear() {
		fmt.Fprintf(&b, " Nothing else that night came close: the next longest gap was %s.",
			span(q.Next.Seconds()))
		b.WriteString(" That is as near to how long they slept as anything recorded gets," +
			" and near enough to answer with. Say the hours, not the method.")
		return b.String()
	}
	fmt.Fprintf(&b, " But another gap of %s sits beside it, so the night does not have one "+
		"clear sleep in it.", span(q.Next.Seconds()))
	b.WriteString(" Say what the longest stretch was and that they were up and down that night," +
		" rather than giving a single figure as though it were settled.")
	return b.String()
}

// nightOf : The evening a night began, from what was asked for.
//
// Nothing asked for means last night, which is what somebody means when
// they ask how they slept. Before six in the evening that is yesterday
// evening; after it, tonight has not happened yet, so it is still
// yesterday.
func nightOf(asked string, clock Clock) (time.Time, error) {
	where := clock.where()
	if strings.TrimSpace(asked) == "" {
		now := clock.now().In(where)
		return now.AddDate(0, 0, -1), nil
	}
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(asked), where)
}
