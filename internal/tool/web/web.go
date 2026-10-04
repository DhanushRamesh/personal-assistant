// Package web : Lets the assistant look something up on the web.
//
// The one domain where the answer is not the person's own. Everything
// else here reads their diary, their reminders, what their devices
// saw; this reads what strangers have published, and the description
// says so, because a snippet is somebody's claim and not a fact that
// has been checked.
package web

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/search"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// Default, Most : How many results come back.
//
// Few. Each is a title, a summary and a source, and a model handed
// ten of them reads several out. Five is enough to answer from and
// short enough to be read rather than recited.
const (
	Default = 5
	Most    = 10
)

// Tools : What the assistant can look up.
func Tools(searcher search.Searcher) []tool.Tool {
	return []tool.Tool{lookUp(searcher)}
}

// lookUp : Search the web.
func lookUp(searcher search.Searcher) tool.Tool {
	return tool.Tool{
		Name:   "web_search",
		Domain: "web",
		Lists:  true,
		Purpose: "Look something up on the web: anything that is not about them, not in their " +
			"diary, reminders, messages, tasks or what their own devices reported.",
		UseWhen: "A question whose answer lives in the world rather than in their life. " +
			"What something is, who somebody is, how something works, what happened, what is " +
			"on, what a place is like, what a thing costs, what the news says. " +
			"And anything that has changed since you were trained, which is most of what is " +
			"worth knowing: prices, scores, who holds an office, whether a place is open, " +
			"what a film is showing at. " +
			"If you find yourself about to answer from memory about the world, search instead " +
			"-- being out of date sounds exactly like being wrong, and they cannot tell which " +
			"you are.",
		Avoid: "Not for anything about them. Their day, their diary, their calls, where they " +
			"were, what they asked you to remember: those have their own tools and the web " +
			"does not know any of it. " +
			"Not for arithmetic, for what a word means, or for anything you can simply answer " +
			"-- searching to confirm what you already know spends seconds they are waiting " +
			"through. " +
			"And not for a second search to check the first: if the answer disagrees with " +
			"itself, say so.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"query"},
			Properties: map[string]tool.Property{
				"query": {
					Type: "string",
					Description: "What to search for, written as you would type it into a " +
						"search box rather than as they said it. Put in what they left out " +
						"because you both know it -- a place, a year, which of two things " +
						"they mean -- and leave out the politeness.",
				},
				"limit": {
					Type: "integer",
					Description: "How many results. Fewer is better: three is plenty for a " +
						"fact, more only for something being compared.",
					Default: Default,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "who won the match last night",
				Args: `{"query":"cricket result last night","saying":"looking it up"}`},
			{Ask: "what time does Phoenix Mall close",
				Args: `{"query":"Phoenix Marketcity Chennai opening hours","limit":3}`},
			{Ask: "is it going to rain tomorrow",
				Args: `{"query":"Chennai weather forecast tomorrow","limit":3}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if searcher == nil {
				return tool.Failed("Nothing on this server can reach the web.")
			}
			query := strings.TrimSpace(args.Query)
			if query == "" {
				return tool.Failed("A search needs something to search for.")
			}

			limit := args.Limit
			if limit <= 0 {
				limit = Default
			}
			if limit > Most {
				limit = Most
			}

			found, err := searcher.Search(ctx, query, limit)
			if err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(search.Describe(query, found))
		},
	}
}
