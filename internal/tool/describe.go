package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// DescribeName : The tool that describes the others.
const DescribeName = "tool_describe"

// mostDescribed : How many tools one call will describe.
//
// A cap rather than a rule. Asking for everything defeats the point --
// the whole reason the others are deferred is that describing all of
// them is most of the request -- and a model that wants twelve at once
// probably wants two.
const mostDescribed = 6

// oneAtLeast, mostAtOnce : mostDescribed and one, addressable, because
// a schema's bounds are pointers so that absent and zero differ.
var oneAtLeast, mostAtOnce = 1, mostDescribed

// Describing : The tool that hands over how to call the deferred ones.
//
// It closes over the registry it belongs to, which is why it is built
// rather than declared: it has to describe whatever was registered,
// including tools added after it.
//
// Channel-filtered through the registry, so this cannot be used to
// learn about a tool the caller was never allowed to reach. Describing
// one is not the same as being able to call it -- Call checks again --
// but naming one that is out of reach would still be telling somebody
// about a thing that is not theirs.
func Describing(r *Registry, address func() string) Tool {
	return Tool{
		Name:    DescribeName,
		Purpose: "Get the arguments for a tool that was named but not described.",
		UseWhen: "Something in the list of further tools looks like what is needed. " +
			"Ask for every one that might be wanted in a single call, because each call costs a round " +
			"and somebody is waiting while it happens.",
		Avoid: "This is not for the tools already described above, which can be called directly. " +
			"It does not do anything itself -- it answers how to call something else, and that call still has to be made.",
		Params: Schema{
			Properties: map[string]Property{
				"names": {
					Type:        "array",
					Description: "The names of the tools to describe, exactly as they were listed.",
					Items:       &Property{Type: "string"},
					MinItems:    &oneAtLeast,
					MaxItems:    &mostAtOnce,
				},
			},
			Required: []string{"names"},
		},
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Run: func(ctx context.Context, in Invocation) Result {
			var args struct {
				Names []string `json:"names"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return Failed("the names could not be read: " + err.Error())
			}
			if len(args.Names) == 0 {
				return Failed("no tool was named, so there is nothing to describe.")
			}
			if len(args.Names) > mostDescribed {
				return Failed(fmt.Sprintf(
					"that is %d tools; ask for at most %d at a time, choosing the ones most likely to be needed.",
					len(args.Names), mostDescribed))
			}

			var described []string
			var missing []string
			var reveal []string
			for _, name := range args.Names {
				name = strings.TrimSpace(name)
				t, ok := r.Get(name)
				if !ok || !t.Reaches(in.Caller.Channel) || name == DescribeName {
					missing = append(missing, name)
					continue
				}
				// Narrated with the live address, the same way the
				// runner narrates the tools it offers directly. A
				// schema handed over without the saying argument gets
				// called without it, and the person waits in silence.
				said := ""
				if address != nil {
					said = address()
				}
				schema, err := Narrated(t.Params, said).MarshalJSON()
				if err != nil {
					missing = append(missing, name)
					continue
				}
				described = append(described, name+": "+
					NarratedDescription(t.Description(), said)+
					" Arguments: "+string(schema))
				reveal = append(reveal, name)
			}

			if len(described) == 0 {
				return Failed("no tool by " + oneOrOther(args.Names) +
					" name. The names are listed in full above; use one of those.")
			}

			sort.Strings(described)
			content := strings.Join(described, "\n\n")
			if len(missing) > 0 {
				content += "\n\nNot found, so not described: " + strings.Join(missing, ", ") +
					". Do not try to call " + oneOrOther(missing) + "."
			}
			return Result{
				Outcome: outcomeFor(len(missing)),
				Content: content,
				Reveal:  reveal,
			}
		},
	}
}

// oneOrOther : "that" or "those", so the sentence reads.
func oneOrOther(names []string) string {
	if len(names) == 1 {
		return "that"
	}
	return "those"
}

// outcomeFor : OK when everything asked for was described, partial when
// some name was not found.
//
// Partial rather than OK, because a model that asked for three and got
// two has to notice: the third is not coming, and building an answer
// around it produces a claim about a tool that does not exist.
func outcomeFor(missing int) conversation.Outcome {
	if missing > 0 {
		return conversation.OutcomePartial
	}
	return conversation.OutcomeOK
}
