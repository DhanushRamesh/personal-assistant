// Package situations looks for what several readings together mean
// when no single one of them means anything.
//
// Somebody trying twice and not getting through. A charger that has
// been in an hour and moved nothing. A device that reports every few
// minutes and has stopped. None of those is an event; each is a
// shape across several, and the device that sent them cannot see it.
//
// A tool rather than a watcher. The looking is the part that must
// stay deterministic -- it was written carefully, it is cheap, and
// handing it to a model would be reintroducing the judging that was
// deliberately taken out of the unasked remarks. What moved onto the
// engine is when to look and what to do about it; what to look for
// is still Go.
package situations

import (
	"context"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/chat"
	"personal-assistant/internal/base/event"
	"personal-assistant/internal/base/prompt"
	"personal-assistant/internal/domain/situation"
)

// Events : Where the readings are.
type Events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Tools : The situation tool, as the registry takes it.
func Tools(events Events, checks []situation.Check, now func() time.Time) []tool.Tool {
	return []tool.Tool{find(events, checks, now)}
}

// find : What several readings together mean.
func find(events Events, checks []situation.Check, clock func() time.Time) tool.Tool {
	return tool.Tool{
		Name:    "situation_find",
		Domain:  "situation",
		Lists:   true,
		Purpose: "Look for what several recent readings together mean, and say so if they mean anything.",
		UseWhen: "A standing rule asks this on a schedule. Answer any question about whether " +
			"anything is amiss, or whether anything needs their attention, from it.",
		Avoid: "Not for what one reading says -- that is event_recent. This answers only the " +
			"handful of shapes it knows how to see, so finding nothing here does not mean " +
			"nothing happened.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "is anything wrong", Args: `{"saying":"having a look"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if events == nil || len(checks) == 0 {
				return tool.OK(prompt.Silence)
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			now := clock()
			evs, err := events.Recent(ctx, in.Caller.UserID, event.Query{
				Since: now.Add(-situation.Behind),
			})
			if err != nil {
				return tool.Broke("That could not be worked out.", err)
			}

			// The first that finds something, in the order they
			// were given. One thing said rather than a list: these
			// are interruptions, and two at once is one too many.
			for _, check := range checks {
				if found, ok := check(evs, now); ok {
					return tool.OK(found.Says)
				}
			}
			// The word the engine reads as the end of a sequence,
			// so a rule that finds nothing costs no model call.
			return tool.OK(prompt.Silence)
		},
	}
}
