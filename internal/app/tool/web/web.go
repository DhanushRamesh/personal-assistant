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

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/chat"
	"personal-assistant/internal/base/search"
)

// Default, Most : How many results come back.
//
// Few. Each is a title, a summary and a source, and a model handed
// ten of them reads several out. Five is enough to answer from and
// short enough to be read rather than recited.
const (
	Default = 5
	Most    = 10

	// Pages, Longest : How many pages may be read at once, and how
	// much of each reaches the model.
	//
	// Three, because reading is the dear act -- seconds each, and a
	// credit each on the service that counts them -- and because an
	// answer built from three sources is already better founded than
	// most. Six thousand characters is perhaps a thousand words: the
	// part of a page that answers something is almost always near
	// the top, and the rest spends a turn nobody has.
	Pages   = 3
	Longest = 6000
)

// Tools : What the assistant can look up, and read.
func Tools(service search.Service) []tool.Tool {
	if service == nil {
		return []tool.Tool{lookUp(nil), readPage(nil)}
	}
	return []tool.Tool{lookUp(service), readPage(service)}
}

// keepLooking : What to do with a search that did not answer, and
// how to say what did.
//
// Owner, 7 October 2026, on two answers in a row: *"it should do
// research until it finds out, always bring a positive message,
// and don't say search results."*
//
// Asked whether somebody was married it said the results were
// fragmentary and the first one was incomplete. Asked who had been
// eliminated from Big Boss it said the search had returned the
// Telugu show rather than the Tamil one. Both are true, both were
// one search, and five rounds are allowed.
//
// The Avoid line above was the cause of the first half. It said not
// to make a second search, meaning do not confirm what you already
// found, and was read as one search only -- so a thin result became
// a report of a thin result instead of a better query.
//
// The other half is that a search is machinery and they did not ask
// about it. "The search returned results for Big Boss Telugu" is a
// sentence about a tool; "Srishti Vyakaranam went from the Telugu
// version two days ago, and I have not found the Tamil one" is the
// same facts as a sentence about the world.
var keepLooking = "One search is a first attempt, not an answer. Five rounds are allowed and nobody " +
	"is counting them. " +
	"If what comes back is thin, cut off, or about the wrong thing, search again with " +
	"different words -- name the series, the season, the year, the place, the language, " +
	"whichever of those the results got wrong. If a result looks like it holds the answer " +
	"but the summary stops short, read the page. " +
	"Give up only when a differently worded search has failed too, and then say what you " +
	"did find rather than what you did not. " +
	"Never describe the searching. Not the results, not the sources, not what came back, " +
	"not that the first one was incomplete: they asked about the world and a sentence about " +
	"your own machinery answers nothing and tells them to go and look themselves. " +
	"Say the thing you found as a fact about the world, then what is still missing, in that " +
	"order -- and if the nearest you have is about something adjacent, say what it is of " +
	"and let them judge it."

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
			"you are. " + keepLooking,
		Avoid: "Not for anything about them. Their day, their diary, their calls, where they " +
			"were, what they asked you to remember: those have their own tools and the web " +
			"does not know any of it. " +
			"Not for arithmetic, for what a word means, or for anything you can simply answer " +
			"-- searching to confirm what you already know spends seconds they are waiting " +
			"through. " +
			"And not to confirm what the first search already told you: where it answered, " +
			"that is the answer, and asking again to be sure spends their seconds on nothing. " +
			"Searching again because you have not got the answer yet is a different thing " +
			"and is what you should do.",
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
				"days": {
					Type: "integer",
					Description: "Only things published in the last this many days. " +
						"Give it whenever the question is about now rather than about how " +
						"things are: today, this week, latest, currently, what is on, who is " +
						"winning, what it costs. 1 for today, 7 for this week, 30 for this " +
						"month. " +
						"Leave it out for anything that does not change -- what a place is, " +
						"how something works, who somebody was. " +
						"Without it an index ranks a page written three years ago above one " +
						"written this morning, and the answer looks current and is not.",
				},
				"news": {
					Type: "boolean",
					Description: "Search news rather than the web at large. For something " +
						"that happened rather than something that is: a result, a decision, " +
						"an announcement, an accident. Not for opening hours or prices.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "who won the match last night",
				Args: `{"query":"cricket result","days":1,"news":true,"saying":"looking it up"}`},
			{Ask: "what time does Phoenix Mall close",
				Args: `{"query":"Phoenix Marketcity Chennai opening hours","limit":3}`},
			{Ask: "is it going to rain tomorrow",
				Args: `{"query":"Chennai weather forecast tomorrow","days":1,"limit":3}`},
			{Ask: "what is trending on Tamil social media today",
				Args: `{"query":"Tamil trending today","days":1,"news":true}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
				Days  int    `json:"days"`
				News  bool   `json:"news"`
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

			found, err := searcher.Search(ctx, search.Ask{
				Query: query, Limit: limit, Days: args.Days, News: args.News,
			})
			if err != nil {
				return tool.Broke("The search could not be done.", err)
			}
			return tool.OK(search.Describe(query, found))
		},
	}
}

// readPage : Read what is actually on a page.
func readPage(reader search.Reader) tool.Tool {
	return tool.Tool{
		Name:   "web_read",
		Domain: "web",
		Purpose: "Read what is actually on a web page, when the summary from a search was not " +
			"enough to answer with.",
		UseWhen: "A search gave you a result that plainly has the answer and a summary too short " +
			"to take it from: a figure, a time, a list, a step, the wording of something. " +
			"Read the page rather than guess what the summary was abbreviating. " +
			"Also when they give you a link and ask what it says.",
		Avoid: "Not instead of searching -- you need a result before you have anything to read, " +
			"and you cannot invent a URL. " +
			"Not when the summaries already answered it: reading costs several seconds each " +
			"and they are waiting through all of them. " +
			"Not to check something you have already answered, and not on every result of a " +
			"search as a matter of course -- one or two that look right, not the page.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"urls"},
			Properties: map[string]tool.Property{
				"urls": {
					Type:  "array",
					Items: &tool.Property{Type: "string"},
					Description: "The addresses to read, exactly as a search gave them. " +
						"One or two is usually right and three is the most that will be " +
						"read; the rest are dropped.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what does that article actually say",
				Args: `{"urls":["https://example.com/a"],"saying":"reading it"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				URLs []string `json:"urls"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if reader == nil {
				return tool.Failed("Nothing on this server can reach the web.")
			}

			wanted := make([]string, 0, Pages)
			for _, u := range args.URLs {
				u = strings.TrimSpace(u)
				// Only what a search can have given: anything else is
				// the model having made a URL up, and fetching an
				// invented address is a round trip to nowhere.
				if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
					continue
				}
				wanted = append(wanted, u)
				if len(wanted) >= Pages {
					break
				}
			}
			if len(wanted) == 0 {
				return tool.Failed("Reading a page needs its address, as a search gave it.")
			}

			pages, err := reader.Read(ctx, wanted)
			if err != nil {
				return tool.Broke("That page could not be read.", err)
			}
			return tool.OK(search.Said(pages, Longest))
		},
	}
}
