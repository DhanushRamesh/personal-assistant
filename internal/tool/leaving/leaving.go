// Package leaving works out when to set off for something in the
// diary.
//
// The half the diary cannot say on its own: it knows the hour and
// not the road. What moved onto the engine is when to look; the
// working out -- how far it is, how long that takes, whether it is
// time yet -- is unchanged and still here.
package leaving

import (
	"context"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/leaving"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Due : Whether it is time to set off for anything.
//
// The watcher is built once and held, not made per call. Its
// memory of what it has already said is a map in the process, as
// it has always been, and that memory is the only thing stopping
// a warning from repeating: the sentence counts down -- "starts
// twenty five minutes from now", then twenty, then fifteen -- so
// it is a different sentence every look and nothing comparing
// words could tell them apart.
//
// Lost on a restart, which is how it already was. Making it
// survive one is a keyed store and a separate decision.
func Due(watch *leaving.Watcher) tool.Tool {
	return tool.Tool{
		Name:    "leaving_due",
		Domain:  "leaving",
		Lists:   true,
		Purpose: "Work out whether it is time to set off for something, and give the words for it.",
		UseWhen: "A standing rule asks this on a schedule. The answer is the sentence to say, " +
			"already written, including how long the journey takes.",
		Avoid: "Not for how far somewhere is -- that is the map tools -- and not for what is in " +
			"the diary, which is calendar_events. This answers only whether it is time to " +
			"leave for something at this minute.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "should I be leaving for anything", Args: `{"saying":"checking the roads"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if watch == nil {
				return tool.OK(prompt.Silence)
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			said, err := watch.Look(ctx, in.Caller.UserID)
			if err != nil {
				return tool.Failed(err.Error())
			}
			if strings.TrimSpace(said) == "" {
				return tool.OK(prompt.Silence)
			}
			return tool.OK(said)
		},
	}
}
