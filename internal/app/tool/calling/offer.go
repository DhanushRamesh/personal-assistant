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

// offer : Says a call is about to be put to the person, so that their
// answer counts.
//
// Owner, on the missed-call announcement: *"it says you got a missed
// call right -- along with that I want, do you want me to call sir?"*
//
// Which a rule could not do. A rule may only reach for tools that do
// not write, so it cannot call call_place, and an announcement that
// merely says "shall I ring him?" leaves nothing on the server
// knowing the question was asked -- so the yes would be met with the
// same question a second time.
//
// This is the asking half of call_place with the dialling removed.
// It changes nothing in the world, which is why a rule may use it:
// all it does is record that a question is going out, so that the
// answer to it is an answer to something.
//
// The name has to be chosen before it can be offered, so this is
// given to the write step rather than run ahead of it -- the step
// that decides who is outstanding is the one that knows.
func offer(reader Reader, asking *Asking, now func() time.Time) tool.Tool {
	return tool.Tool{
		Name:    "call_offer",
		Domain:  "calling",
		Purpose: "Say that you are about to offer to ring somebody, so their answer counts.",
		UseWhen: "Just before you offer a call they did not ask for -- telling them about a " +
			"missed call and asking whether to ring back. Call this with the name, then " +
			"put the question to them in your own words.",
		Avoid: "Not needed when they asked for the call themselves; call_place does the asking " +
			"then. This dials nothing and changes nothing: it only means that if they say " +
			"yes, ringing them needs no second question.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"who": {
					Type: "string",
					Description: "Who you are about to offer to ring, by name, as the " +
						"phone's contacts have it.",
				},
			},
			Required: []string{"who"},
		},
		Examples: []tool.Example{
			{Ask: "[a rule found a missed call nobody returned]",
				Args: `{"who":"Kliffy","saying":""}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			var args struct {
				Who string `json:"who"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("Those arguments could not be read.")
			}
			said := calling.Tidy(args.Who)
			if said == "" {
				return tool.Failed("Nobody was named, so there is nobody to offer to ring.")
			}
			if calling.Digits(said) {
				return tool.Failed("Only a name can be rung, never a number.")
			}

			// Resolved against the people actually called, so the
			// name held is the one the phone will find.
			who := said
			if found, err := Known(ctx, reader, in.Caller.UserID, when(now)); err == nil {
				if name, how := Match(said, found); how != Stranger {
					who = name
				}
			}
			asking.Put(in.Caller.UserID, who, said, false, false)
			return tool.OK(fmt.Sprintf("Ready. Now ask them, in your own words, whether to "+
				"ring %s, as a question they can answer yes or no. Nothing has been "+
				"dialled and nothing will be unless they say yes. If they do, ring them "+
				"with call_place and the name -- they will not be asked twice.", who))
		},
	}
}
