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
	"encoding/json"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/situation"
)

// Events : Where the readings are.
type Events interface {
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Named : One check, and what to ask for it by.
//
// A name, because a rule has to be able to ask for one of these
// rather than for whichever fires first. Asking for all of them
// was the whole of what this tool could do, and it meant the
// check listed first silently suppressed the ones after it: a
// phone could be dead for an hour and go unmentioned because
// somebody had rung twice.
type Named struct {
	// Name : What a rule asks for it by. Lower case, one word.
	Name string
	// Look : The check itself.
	Look situation.Check
}

// Tools : The situation tool, as the registry takes it.
func Tools(events Events, checks []Named, now func() time.Time) []tool.Tool {
	return []tool.Tool{find(events, checks, now)}
}

// wanted : The checks to run, in order, for what was asked.
//
// Everything when nothing was named, which is what somebody asking
// "is anything wrong" means and what the model gets. One when a
// rule names it, which is what a rule about that one thing means.
func wanted(checks []Named, name string) []Named {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return checks
	}
	for _, c := range checks {
		if c.Name == name {
			return []Named{c}
		}
	}
	return nil
}

// names : What may be asked for, for the description and for the
// message when something else is.
func names(checks []Named) string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return strings.Join(out, ", ")
}

// find : What several readings together mean.
func find(events Events, checks []Named, clock func() time.Time) tool.Tool {
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
		Params: tool.Schema{Properties: map[string]tool.Property{
			"look": {
				Type: "string",
				Description: "One shape to look for, by name: " + names(checks) +
					". Leave it out to look for all of them, which is what a person " +
					"asking whether anything is wrong means.",
			},
		}},
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

			var asked struct {
				Look string `json:"look"`
			}
			if len(in.Args) > 0 {
				if err := json.Unmarshal(in.Args, &asked); err != nil {
					return tool.Broke("That could not be worked out.", err)
				}
			}
			look := wanted(checks, asked.Look)
			if look == nil {
				return tool.Failed("There is nothing of that name to look for. " +
					"There is " + names(checks) + ".")
			}

			now := clock()
			evs, err := events.Recent(ctx, in.Caller.UserID, event.Query{
				Since: now.Add(-situation.Behind),
			})
			if err != nil {
				return tool.Broke("That could not be worked out.", err)
			}

			// The first that finds something, among those asked
			// for. One thing said rather than a list: these are
			// interruptions, and two at once is one too many --
			// which is exactly why a rule about one of them has to
			// be able to say which, or the first in the list
			// speaks for all of them for as long as it has
			// anything to say.
			for _, check := range look {
				if found, ok := check.Look(evs, now); ok {
					return tool.OK(found.Says)
				}
			}
			// The word the engine reads as the end of a sequence,
			// so a rule that finds nothing costs no model call.
			return tool.OK(prompt.Silence)
		},
	}
}
