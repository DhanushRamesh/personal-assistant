package calling

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/calling"
)

// Changer : What changes something about a call in progress.
type Changer interface {
	// Change : Writes down that the phone should do this, not
	// before `after` has passed.
	//
	// Zero for a change asked for on its own, where the call is
	// already up. A few seconds for one asked for in the same
	// breath as the call, where the dialler has to get there
	// first -- see [calling.Connecting].
	Change(ctx context.Context, userID string, d calling.During, after time.Duration) error
}

// during : The two mid-call tools.
//
// Not confirmed, unlike placing a call, and the reason is the room
// rather than the risk. Both are instantly undone, and both are asked
// for while somebody else is on the line -- where "shall I put that on
// speaker, sir?" said out loud is worse than simply being wrong.
//
// They are also silent by design: the answer is a thing that happened,
// not a thing to announce into somebody else's call.
func during(change Changer) []tool.Tool {
	return []tool.Tool{
		{
			Name:    "call_speaker",
			Domain:  "calling",
			Writes:  true,
			Purpose: "Put the call that is in progress on loudspeaker, or take it off.",
			UseWhen: "They ask for speaker, loudspeaker, speakerphone, or to be taken off it, " +
				"while a call is going on.",
			Avoid: "Not for the phone's media or ringer volume. This is the earpiece against " +
				"the loudspeaker during a call, and it does nothing when no call is up.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params: tool.Schema{
				Properties: map[string]tool.Property{
					"on": {
						Type: "boolean",
						Description: "True for loudspeaker, false to go back to the " +
							"earpiece.",
						Default: true,
					},
				},
			},
			Examples: []tool.Example{
				{Ask: "put it on speaker", Args: `{"on":true,"saying":"on speaker"}`},
				{Ask: "take it off speaker", Args: `{"on":false,"saying":"off speaker"}`},
			},
			Run: changing(change, calling.Speaker, func(on bool) string {
				if on {
					return "The call is on loudspeaker."
				}
				return "The call is back on the earpiece."
			}),
		},
		{
			Name:    "call_mute",
			Domain:  "calling",
			Writes:  true,
			Purpose: "Mute or unmute the person's own microphone on the call in progress.",
			UseWhen: "They ask to be muted, to mute themselves, or to be unmuted or come " +
				"back, while a call is going on.",
			Avoid: "This is their own microphone on the call, so the other side stops hearing " +
				"them. Not for silencing the phone, the ringer, or anything playing.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params: tool.Schema{
				Properties: map[string]tool.Property{
					"on": {
						Type: "boolean",
						Description: "True to mute them, false to let them be heard " +
							"again. True means muted, which is the microphone off.",
						Default: true,
					},
				},
			},
			Examples: []tool.Example{
				{Ask: "mute me", Args: `{"on":true,"saying":"muted"}`},
				{Ask: "unmute", Args: `{"on":false,"saying":"you are back"}`},
			},
			// Muting is the microphone off, which is the one place
			// the words and the flag run opposite. Inverted here, at
			// the edge, so nothing downstream has to remember it.
			Run: changing(change, calling.Mic, func(on bool) string {
				if on {
					return "They are muted. The other side cannot hear them now."
				}
				return "They are unmuted and can be heard again."
			}, inverted),
		},
	}
}

// inverted : That the argument means the opposite of the switch.
const inverted = true

// changing : One mid-call change, as a tool's Run.
func changing(change Changer, what calling.What, said func(bool) string,
	flip ...bool) tool.Run {
	return func(ctx context.Context, in tool.Invocation) tool.Result {
		if in.Caller.UserID == "" {
			return tool.Failed("This request did not come from a known person.")
		}
		// Absent means true. Both of these are asked for far more
		// often than they are undone, and a model that leaves the
		// argument out meant the obvious thing.
		args := struct {
			On *bool `json:"on"`
		}{}
		if err := json.Unmarshal(in.Args, &args); err != nil {
			return tool.Failed("Those arguments could not be read.")
		}
		on := true
		if args.On != nil {
			on = *args.On
		}

		want := on
		if len(flip) > 0 && flip[0] {
			want = !on
		}
		// No delay: this was asked for on its own, so the call is
		// already up.
		if err := change.Change(ctx, in.Caller.UserID,
			calling.During{What: what, On: want}, 0); err != nil {
			return tool.Broke("That could not be asked of the phone.", err)
		}
		return tool.OK(fmt.Sprintf("%s Say nothing about it unless they ask: this was "+
			"wanted in the middle of a call with somebody else, and anything said here "+
			"is said into that call.", said(on)))
	}
}
