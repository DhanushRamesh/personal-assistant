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
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/search"
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
	// most.
	//
	// Six thousand characters is perhaps a thousand words, and which
	// thousand matters more than how many. It used to be the first
	// always, on the grounds that the part of a page which answers
	// something is near the top. That holds for a page written about
	// the question and not for a search result, which is the only
	// kind there is here: asked for the population of Chennai from
	// its Wikipedia article, the read came back with geography and
	// the assistant said the page did not show it.
	//
	// So the caller may say what it is looking for and be sent the
	// part around it. Without that it still gets the opening.
	// Thin : How few news results mean the index was the wrong one.
	//
	// Three. One or two is a search that found the edge of
	// something; three is a search that found it.
	Thin    = 3
	Pages   = 3
	Longest = 6000
)

// Tools : What the assistant can look up, and read.
func Tools(service search.Service) []tool.Tool {
	if service == nil {
		return []tool.Tool{lookUp(nil, nil), readPage(nil)}
	}
	return []tool.Tool{lookUp(service, service), readPage(service)}
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
	"If the words were right and the answer simply was not in the summaries, search again " +
	"with deep set, which reads the pages themselves. " +
	"Never offer to search more deeply and wait to be told. Owner, 18 October 2026, shown " +
	"\"I can search more deeply if you would like\": that is a turn spent asking permission " +
	"for something nobody would refuse. Do it, then answer. " +
	"Give up only when a differently worded search and a deep one have both failed, and " +
	"then say what you did find rather than what you did not. " +
	"Never describe the searching. Not the results, not the sources, not what came back, " +
	"not that the first one was incomplete: they asked about the world and a sentence about " +
	"your own machinery answers nothing and tells them to go and look themselves. " +
	"Say the thing you found as a fact about the world, then what is still missing, in that " +
	"order -- and if the nearest you have is about something adjacent, say what it is of " +
	"and let them judge it."

// lookUp : Search the web.
func lookUp(searcher search.Searcher, reader search.Reader) tool.Tool {
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
			"Never say that something in the world does not exist, has not happened, has not " +
			"been released or has not been announced without searching first: that is a " +
			"statement about the world as it was when you were trained, and you cannot tell " +
			"how long ago that was. Asked how a film was, whether a thing is out, who holds a " +
			"job, what something costs now -- search, then answer. " +
			"Where the search disagrees with what you remember, the search is right and you " +
			"are out of date. Say what it found rather than doubting it; a page you have not " +
			"heard of is not a reason to dismiss what several of them agree on. " +
			"If you find yourself about to answer from memory about the world, search instead " +
			"-- being out of date sounds exactly like being wrong, and they cannot tell which " +
			"you are. " + keepLooking,
		Avoid: "Not for anything about them. Their day, their diary, their calls, where they " +
			"were, what they asked you to remember: those have their own tools and the web " +
			"does not know any of it. " +
			"Not for arithmetic, for what a word means, or for anything that cannot have " +
			"changed since you were trained -- searching to confirm what is still true spends " +
			"seconds they are waiting through. That is a narrow list: how a word is spelt, how " +
			"a thing works, what happened in a year that is over. A film, a price, a score, a " +
			"person's job, whether something exists yet, and anything with a date on it are " +
			"all on the other side of it. " +
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
				"deep": {
					Type: "boolean",
					Description: "Read the top pages as well as searching, and bring back " +
						"what each of them actually says, so they can be compared against " +
						"one another rather than taken from one summary. " +
						"Set it yourself the moment a plain search has not answered -- a " +
						"summary that stops short, results about the wrong thing, a detail " +
						"the snippets do not carry. That is what it is for, and it is your " +
						"decision, not theirs: never offer to look more deeply and wait to " +
						"be told, just do it and answer. " +
						"Set it too when they ask you to cross-check, to look into " +
						"something properly or to be sure, and for anything where sources " +
						"are likely to disagree: a price, a review, a claim about what " +
						"something does, a date that has moved. " +
						"Say where they disagree when they do; that is the whole point of " +
						"having read several. " +
						"It costs a few seconds and reads three pages, so not for a fact " +
						"the first search already gave you.",
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
			{Ask: "cross-check that for me",
				Args: `{"query":"Jailer 2 reviews critics","deep":true,"saying":"reading a few reviews"}`},
			{Ask: "look into whether that medicine clashes with anything",
				Args: `{"query":"metformin drug interactions","deep":true,"limit":5}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
				Days  int    `json:"days"`
				News  bool   `json:"news"`
				Deep  bool   `json:"deep"`
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

			// A news search that found almost nothing is asked again
			// of the web at large, here rather than by the model.
			//
			// The news index is a narrow one and the argument that
			// chooses it cannot be described well enough to be got
			// right every time: asked who had been eliminated from
			// Bigg Boss Tamil, the model set news because an
			// elimination is plainly something that happened, and
			// the answer is on entertainment sites and fan pages
			// that no wire service indexes. It reported that the
			// search only returned the Telugu version. The same
			// query without the filter had the name, the date and
			// the prize.
			//
			// Done in the tool because a round spent discovering
			// this is a round the person waits through, and because
			// the model has already been told twice -- in the
			// argument and in keepLooking -- and did it anyway. A
			// filter that narrows to nothing should widen itself.
			if args.News && len(found) < Thin {
				wider, again := searcher.Search(ctx, search.Ask{
					Query: query, Limit: limit, Days: args.Days,
				})
				if again == nil && len(wider) > len(found) {
					found = wider
				}
			}
			if !args.Deep || reader == nil || len(found) == 0 {
				return tool.OK(search.Describe(query, found))
			}
			return tool.OK(compared(ctx, reader, query, found))
		},
	}
}

// compared : The results, and what the first few pages actually say.
//
// Owner, 18 October 2026, asked whether deep research should be its own
// tool or a background job: *"I want deep research in the chat call
// only, because I'll ask to cross-check and research there."*
//
// So it is one tool call and not a conversation: search, then read the
// top pages in a single batch, and hand the model all of it at once.
// A turn is cut at thirty seconds and a round of the model is four and
// a half of them, so the thing that cannot be afforded is rounds. The
// reading is one more HTTP call on a service that takes every URL
// together.
//
// What it buys is the only kind of cross-checking that is worth the
// name: three sources' own words side by side, rather than one
// summary and a model's memory of the rest.
func compared(ctx context.Context, reader search.Reader, query string, found []search.Result) string {
	wanted := make([]string, 0, Pages)
	for _, r := range found {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		wanted = append(wanted, r.URL)
		if len(wanted) >= Pages {
			break
		}
	}

	pages, err := reader.Read(ctx, wanted)
	if err != nil || len(pages) == 0 {
		// The search still stands. A read that failed is a thinner
		// answer, not a failed one, and saying so beats refusing.
		return search.Describe(query, found)
	}
	return search.Describe(query, found) + "\n\n" +
		"What each of these pages says, in its own words. Compare them, and say where they disagree:\n\n" +
		search.Said(pages, Longest, query)
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
				"looking_for": {
					Type: "string",
					Description: "A word or two you expect to find near the answer -- a " +
						"name, a figure's label, the thing itself. A long page is cut, " +
						"and this decides which part of it you are sent: without it you " +
						"get the opening, which is the right part only when the page is " +
						"about your question. Asked a city's population, say population.",
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what does that article actually say",
				Args: `{"urls":["https://example.com/a"],"saying":"reading it"}`},
			{Ask: "how many people live in Chennai",
				Args: `{"urls":["https://en.wikipedia.org/wiki/Chennai"],"looking_for":"population","saying":"reading the page for the figure"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				URLs       []string `json:"urls"`
				LookingFor string   `json:"looking_for"`
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
			return tool.OK(search.Said(pages, Longest, args.LookingFor))
		},
	}
}
