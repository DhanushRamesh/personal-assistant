//go:build evals

// Package evals asks a real model whether the tool descriptions work.
//
// Everything else in the suite checks that the machinery runs. This checks
// the only thing that decides whether the assistant is any good: given what
// somebody said, does the model reach for the right tool and fill in the
// right arguments. That cannot be asserted against a stub, because a stub has
// no opinion about a description.
//
// Behind a build tag because it costs real calls to a real service:
//
//	make evals
//
// A failure here is usually a description to fix rather than code. The point
// is that a change to a description becomes measurable instead of arguable.
package evals

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/coming"
	"github.com/DhanushRamesh/personal-assistant/internal/config"
	"github.com/DhanushRamesh/personal-assistant/internal/embed"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/environment/platformai"
	eventmemory "github.com/DhanushRamesh/personal-assistant/internal/event/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/memory/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	remindmemory "github.com/DhanushRamesh/personal-assistant/internal/remind/inmemory"
	"github.com/DhanushRamesh/personal-assistant/internal/search"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/calendar"
	comingtool "github.com/DhanushRamesh/personal-assistant/internal/tool/coming"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/events"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/mail"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/memories"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/places"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/reminders"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/tasks"
	webtool "github.com/DhanushRamesh/personal-assistant/internal/tool/web"
)

// eval : One thing a person might say, and what should happen.
type eval struct {
	// Say : What the person said.
	Say string
	// Tool : The tools that would be a right first move, any one of them.
	//
	// Several, because more than one can be right: asked to delete something
	// by name, looking the name up and listing everything both lead to the
	// identifier the delete needs. Empty means no tool should be called at
	// all, which is as much a decision as choosing one.
	Tool []string
	// Args : Fragments that must appear in the arguments, if any.
	Args []string
	// Channel : How it arrived. Voice cannot reach everything.
	Channel chat.Channel
	// After : What the assistant said just before, when the case only
	// makes sense as an answer to something.
	//
	// "Ok got it" with nothing in front of it means nothing, and a
	// model that called a tool for it would be guessing. Three cases
	// about answering a warning failed here for exactly that reason
	// before the harness could carry a turn of history: the model was
	// right and the test was not.
	After string
	// Never : Fragments the reply must not contain, whatever it does.
	//
	// The rest of this measures which tool is chosen, which is almost
	// always the thing that decides whether an answer is any good. It
	// is not always: asked to add some numbers the model chose no tool,
	// correctly, and then opened with "I have no tool that calculates a
	// sum, sir." Nothing about the choice was wrong and the answer was
	// still poor, so there has to be a way to say what must not be said.
	Never []string
}

// Pace : How long to wait between questions.
//
// Chosen against the limiter rather than for speed. The suite is
// eighty-odd model calls and each already takes seconds; a short
// pause between them costs under a minute on the whole run and
// turns "you have made too many requests continuously" from a
// regular event into none.
const Pace = 1500 * time.Millisecond

// cases : What the assistant is expected to do.
var cases = []eval{
	// Reaching for the right one.
	{Say: "what conversations have I had", Tool: []string{"conversation_list"}},
	{Say: "what have we been talking about lately", Tool: []string{"conversation_list"}},
	{Say: "show me the ones I put away", Tool: []string{"conversation_list"}, Args: []string{"archived"}},
	{Say: "go back to the roof conversation", Tool: []string{"conversation_find"}, Args: []string{"roof"}},
	{Say: "switch to the one about cricket", Tool: []string{"conversation_find"}, Args: []string{"cricket"}},
	{Say: "start a new conversation", Tool: []string{"conversation_new"}},
	{Say: "let's start fresh, call it Kitchen Plans", Tool: []string{"conversation_new"}, Args: []string{"Kitchen"}},
	{Say: "call this one Roof Quotes", Tool: []string{"conversation_rename"}, Args: []string{"current", "Roof Quotes"}},
	{Say: "I'm done with this conversation, put it away", Tool: []string{"conversation_archive"}, Args: []string{"current", "true"}},

	// Archiving, not deleting. The words differ and so should the tool: one
	// of them cannot be undone.
	{Say: "I've finished with this one", Tool: []string{"conversation_archive"}},

	// Either way of finding the identifier is right, and the point is that it
	// looks one up rather than inventing one.
	{Say: "delete the Roof Quotes conversation",
		Tool: []string{"conversation_find", "conversation_list"}, Channel: chat.ChannelDirect},

	// Remembering when asked.
	{Say: "remember that the roofer quoted forty thousand", Tool: []string{"memory_remember"}, Args: []string{"roof"}},
	{Say: "keep a note that my birthday is the 22nd of October", Tool: []string{"memory_remember"}, Args: []string{"birthday"}},
	{Say: "from now on always answer me briefly", Tool: []string{"memory_remember"}, Args: []string{"always"}},

	// And when not asked. Having to say "remember this" is the thing being
	// designed away: a constraint, a decision and its reason, and a figure
	// agreed are all worth keeping whether or not anybody said so.
	{Say: "I cannot take dairy, it gives me a headache", Tool: []string{"memory_remember"}},
	{Say: "we settled on MySQL in the end, Postgres would have been another thing to run",
		Tool: []string{"memory_remember"}},
	{Say: "the plumber and I agreed twelve thousand for the bathroom", Tool: []string{"memory_remember"}},
	// Three right first moves, now that the calendar is registered. A date
	// was named, and the standing rule is to read the diary before
	// answering anything with a date in it, so looking is as right as
	// writing it down -- what would be wrong is neither.
	{Say: "my sister's flight lands on the 3rd of March",
		Tool: []string{"memory_remember", "calendar_add", "calendar_events"}},

	// Looking something up on purpose, when recall has offered nothing.
	{Say: "what did I tell you about the roof", Tool: []string{"memory_search"}, Args: []string{"roof"}},
	{Say: "do you remember anything about my shopping list", Tool: []string{"memory_search"}, Args: []string{"shopping"}},

	// Changing and forgetting both need the identifier, and the point is
	// that it looks one up rather than inventing one.
	{Say: "the roofer actually said fifty thousand, update that",
		Tool: []string{"memory_search", "memory_update"}},
	{Say: "forget what I told you about the roof",
		Tool: []string{"memory_search", "memory_forget"}, Channel: chat.ChannelDirect},

	// Reminders. A length of time goes to minutes_from_now so the
	// arithmetic is the server's, and a time of day goes to at.
	{Say: "set a timer for twenty minutes", Tool: []string{"reminder_set"}, Args: []string{"20"}},
	// Seconds are seconds. Refusing them for not being whole minutes was a
	// limitation of the tool, and the model repeated it faithfully.
	{Say: "set a timer for thirty seconds", Tool: []string{"reminder_set"}, Args: []string{"seconds_from_now", "30"}},
	{Say: "time me for ninety seconds", Tool: []string{"reminder_set"}, Args: []string{"seconds_from_now"}},
	{Say: "remind me in two hours to take the washing out", Tool: []string{"reminder_set"}, Args: []string{"120"}},
	{Say: "wake me at seven every weekday", Tool: []string{"reminder_set"}, Args: []string{"weekdays"}},
	{Say: "what timers do I have", Tool: []string{"reminder_list"}},
	{Say: "cancel my timer", Tool: []string{"reminder_list"}},

	// A note for later is remembered, not announced. The two are easy to
	// confuse and only one of them speaks at you.
	{Say: "note down that the roofer wants paying by Friday", Tool: []string{"memory_remember"}},

	// The diary, which is where dates and times live. These are the cases
	// the rule exists for: a plan, an arrangement, a question about a day.
	{Say: "what is on tomorrow", Tool: []string{"calendar_events"}},
	{Say: "am I free on Thursday afternoon", Tool: []string{"calendar_free", "calendar_events"}},
	{Say: "put the dentist in for Tuesday at four",
		Tool: []string{"calendar_add", "calendar_events", "calendar_free"}},
	{Say: "I am thinking of going to Chennai next weekend", Tool: []string{"calendar_events"}},
	{Say: "is anything happening on the 26th", Tool: []string{"calendar_events"}},

	// Something placed in the future, which is now offered for the diary
	// whatever kind of thing it is. Reading the diary, looking the name
	// up and writing it down are all right first moves; saying nothing
	// is not.
	{Say: "I am having dinner with Alekhya tomorrow",
		Tool: []string{"calendar_events", "calendar_add", "memory_search", "conversation_find"}},
	{Say: "the plumber is coming on Thursday morning",
		Tool: []string{"calendar_events", "calendar_add", "calendar_free"}},
	{Say: "I have to submit the form before Friday",
		Tool: []string{"calendar_events", "calendar_add", "task_add", "task_lists", "task_list"}},

	// What is playing, which is the case that failed live: the kind is
	// held back unless it is named, so a call without one returns no
	// music at all and the model answered from what it had said
	// earlier in the conversation.
	{Say: "what am I listening to right now",
		Tool: []string{"event_recent"}, Args: []string{"music"}},
	{Say: "what was I listening to this afternoon",
		Tool: []string{"event_recent"}, Args: []string{"music"}},

	// What the devices reported. The first of these is the one that
	// failed: asked who they had spoken to, the assistant said it had no
	// tool that reads call logs, because the description listed places
	// and networks and never people. The tool had the calls.
	{Say: "with whom did I talk today", Tool: []string{"event_recent"}},
	{Say: "who called me this morning", Tool: []string{"event_recent"}},
	{Say: "what have I been up to today", Tool: []string{"event_recent"}},
	{Say: "have I been out at all today", Tool: []string{"place_visits", "event_recent"}},

	// How something of theirs stands now. The devices report a reading
	// whenever it changes, so the newest one is the present value --
	// but the description was written about the past, and asked what
	// his battery was the assistant said it had no tool for it. It had
	// twenty-two readings from that morning.
	//
	// The same failure as the call log above, one kind later. Which is
	// the point: the description cannot list what arrives next, so it
	// has to say that, and say never to answer from it that a tool is
	// missing.
	{Say: "how much battery do I have left", Tool: []string{"event_recent"}},
	// event_kinds is allowed here and this is a gap in the harness
	// rather than generosity. In production it is prefetched, so its
	// answer is already in the prompt and calling it is a wasted
	// round; here nothing prefetches, so reaching for the listing
	// first is exactly what its own description asks for. Until the
	// suite runs the prefetch too, both are right.
	{Say: "do you know what my battery is right now",
		Tool: []string{"event_recent", "event_kinds"}},
	{Say: "is my phone charged", Tool: []string{"event_recent"}},

	// The world rather than their life. Being out of date sounds
	// exactly like being wrong, and they cannot tell which it is.
	{Say: "who won the cricket last night", Tool: []string{"web_search"}},
	{Say: "what time does Phoenix Marketcity close today", Tool: []string{"web_search"}},
	{Say: "is it going to rain in Chennai tomorrow", Tool: []string{"web_search"}},

	// The boundary, both ways. The test is not whether it knows the
	// answer but whether the answer can have changed: a capital does
	// not, a chief minister does, and being out of date on the second
	// sounds exactly like being wrong.
	{Say: "what is the capital of France", Tool: nil},
	{Say: "what does the word laconic mean", Tool: nil},
	{Say: "who is the chief minister of Tamil Nadu", Tool: []string{"web_search"}},
	{Say: "how much is a litre of petrol", Tool: []string{"web_search"}},

	// And the over-reach it could buy. Everything above is about the
	// world; everything here is about him, and the web knows none of
	// it.
	{Say: "what did I do yesterday", Tool: []string{"event_recent", "place_visits", "event_kinds"}},
	{Say: "what is eighty plus twenty plus thirty",
		Never: []string{"no tool", "not have a tool", "tools available"}},

	// Answering a warning the assistant gave about the diary. The
	// second word, nearer the time, is right for somebody who forgot
	// and wrong for somebody who answered the first one.
	{Say: "ok got it, thanks", Tool: []string{"coming_settled"},
		After: "Attending Vignesh's wedding is in 3 days, at Tuesday, 6:30 pm. " +
			"A present to buy and something to wear."},
	{Say: "cancel that, I don't need telling again", Tool: []string{"coming_settled"},
		After: "Attending Vignesh's wedding is in 3 days, at Tuesday, 6:30 pm. " +
			"A present to buy and something to wear."},
	// A whole acknowledgement, not half of one. "I have already
	// bought the present" was tried here and dropped: the warning
	// named two things to do, so settling the lot on one of them
	// would silence the shirt as well. Reaching for the diary there
	// is not obviously wrong, and a case whose right answer is
	// arguable measures nothing.
	{Say: "I have sorted all that out already", Tool: []string{"coming_settled"},
		After: "Attending Vignesh's wedding is in 3 days, at Tuesday, 6:30 pm. " +
			"A present to buy and something to wear."},

	// Half of it done. The warning stays and asks only for the rest,
	// so what the tool is given must be the shirt and not the
	// present.
	{Say: "I have already bought the present", Tool: []string{"coming_settled"},
		Args: []string{"wear"},
		After: "Attending Vignesh's wedding is in 3 days, at Tuesday, 6:30 pm. " +
			"A present to buy and something to wear."},

	// And the over-reach it could buy. Dropping a warning is not
	// taking the thing out of the diary, and it is not a reminder
	// they set themselves.
	{Say: "take the wedding out of my diary",
		Tool: []string{"calendar_events", "calendar_cancel"}},
	{Say: "cancel my 4pm water reminder",
		Tool: []string{"reminder_list", "reminder_cancel"}},

	// Things that are simply answered. No tool is right, and so is
	// saying nothing about tools: asked to add some numbers it opened
	// with "I have no tool that calculates a sum, sir", then added them
	// up and got it wrong. Asked about its own battery it recited the
	// whole toolbox before declining. The choice was right both times.
	{Say: "Just add up these things. Okay? What is 80 plus 20 plus 30 plus 70? " +
		"Plus... 10 plus 90 plus 50 plus 50 plus fifty fifty plus plus 50 plus 25.",
		Never: []string{"no tool", "not have a tool", "tools available"}},
	{Say: "add up 15, 40 and 7 for me",
		Never: []string{"no tool", "not have a tool", "tools available"}},
	{Say: "what does laconic mean",
		Never: []string{"no tool", "not have a tool", "tools available"}},

	// Lists. Every write needs an identifier that only a read can give,
	// so reading first is as right an answer as writing.
	{Say: "what is on my to-do list", Tool: []string{"task_list", "task_lists"}},
	{Say: "add milk to the shopping list",
		Tool: []string{"task_add", "task_lists", "task_list"}},
	{Say: "is bread on the shopping list",
		Tool: []string{"task_list", "task_find", "task_lists"}},
	{Say: "I have bought the milk",
		Tool: []string{"task_done", "task_list", "task_find", "task_lists"}},
	{Say: "start a new list called Work", Tool: []string{"task_list_add", "task_lists"}},
	{Say: "move the paint to the DIY list",
		Tool: []string{"task_move", "task_find", "task_list", "task_lists"}},

	// The line between the three things that sound alike. A task has no
	// time, a reminder is said aloud at one, and an event takes up time
	// in the diary and is attended.
	{Say: "I need to buy paint at some point",
		Tool: []string{"task_add", "task_lists", "task_list"}},
	{Say: "remind me to call the plumber at four", Tool: []string{"reminder_set"}},
	{Say: "I have the dentist on Tuesday at four",
		Tool: []string{"calendar_add", "calendar_events", "calendar_free"}},

	// Mail. Which one is right turns on what is being asked for: a count,
	// a glance, a sender, an exchange, or the whole of one message.
	{Say: "has anything come in", Tool: []string{"mail_inbox", "mail_unread"}},
	{Say: "how many unread do I have", Tool: []string{"mail_unread"}},
	{Say: "anything from Amazon", Tool: []string{"mail_search"}, Args: []string{"amazon"}},
	{Say: "what did Alekhya say about the flat",
		Tool: []string{"mail_thread", "mail_search"}, Args: []string{"alekhya"}},
	{Say: "is there anything I have not replied to",
		Tool: []string{"mail_thread"}, Args: []string{"unanswered"}},
	{Say: "when did I last email Giridharan",
		Tool: []string{"mail_search"}, Args: []string{"giridharan"}},

	// Where they have been is its own tool, and the raw readings are not
	// it. The events tool says who called and what connected; this says
	// where they were and for how long.
	{Say: "where was I on Tuesday", Tool: []string{"place_visits"}},
	{Say: "how long was I at the office today", Tool: []string{"place_visits"}},
	{Say: "have I been to the badminton court this week", Tool: []string{"place_visits"}},
	{Say: "when did I last leave the office", Tool: []string{"place_visits", "event_recent"}},

	// Reaching for none. A model that calls a tool at every question is as
	// wrong as one that never does.
	{Say: "what is the capital of Australia"},
	{Say: "how are you"},
	{Say: "thank you"},
	{Say: "what is twelve times eight"},

	// Nor is every passing fact a thing to write down. The test is whether
	// it should still be believed next month, and everything said is
	// already searchable on its own, so none of these needs a memory.
	{Say: "I had dosa for breakfast"},
	{Say: "it is raining here today"},
	{Say: "I am a bit tired this evening"},
	{Say: "that took longer than I expected"},
}

func TestTheModelReachesForTheRightTool(t *testing.T) {
	// A store, so a tool that runs has somewhere to run against. What is
	// measured is the choice, which is made before anything runs, but a
	// registry that cannot build measures nothing.
	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}

	clock := reminders.Clock{Location: time.UTC}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), clock),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
		calendar.All(nil, calendar.Clock{Location: time.UTC}),
		tasks.All(nil, tasks.Clock{Location: time.UTC}),
		mail.All(nil, mail.Clock{Location: time.UTC}),
		places.Tools(nil, places.Clock{Location: time.UTC}),
		comingtool.Tools(nothingComing{}, comingtool.Clock{Location: time.UTC}),
		webtool.Tools(quietWeb{}),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	cfg, err := config.Load("../../../config.ini", os.LookupEnv)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.Provider.Name != config.ProviderPlatformAI {
		t.Skip("no real environment configured, so there is nothing to ask")
	}

	env, err := platformai.New(platformai.Config{
		ClientID: cfg.PlatformAI.ClientID, ClientSecret: cfg.PlatformAI.ClientSecret,
		RefreshToken: cfg.PlatformAI.RefreshToken, PortalID: cfg.PlatformAI.PortalID,
		TokenURL: cfg.PlatformAI.TokenURL, ChatURL: cfg.PlatformAI.ChatURL,
		Scope: cfg.PlatformAI.Scope, RedirectURI: cfg.PlatformAI.RedirectURI,
		Vendor: cfg.PlatformAI.Vendor, Model: cfg.PlatformAI.Model,
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name),
		Timeout:      90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	// EVAL_ONLY=text runs just the cases whose sentence contains it, so a
	// single wording can be measured both ways without paying for all of
	// them. Empty runs everything, which is what make evals does.
	only := strings.ToLower(os.Getenv("EVAL_ONLY"))

	var right, wrong int
	var asked int
	for _, c := range cases {
		if only != "" && !strings.Contains(strings.ToLower(c.Say), only) {
			continue
		}

		// Eighty-three questions as fast as the machine can send them
		// is what the gateway means by "too many requests
		// continuously", and a suite that trips the limiter measures
		// the limiter. The server retries now, but a retry inside a
		// test is a slower test for no information: the pause is
		// cheaper than the refusal.
		if asked > 0 {
			time.Sleep(Pace)
		}
		asked++
		channel := c.Channel
		if channel == "" {
			channel = chat.ChannelVoice
		}

		called, args, said := ask(t, env, registry, channel, c.Say, c.After)

		var blurted []string
		for _, never := range c.Never {
			if strings.Contains(strings.ToLower(said), strings.ToLower(never)) {
				blurted = append(blurted, never)
			}
		}
		if len(blurted) > 0 {
			wrong++
			t.Errorf("%-45q said %v: %s", c.Say, blurted, said)
			continue
		}

		switch {
		case len(c.Tool) == 0 && called != "":
			wrong++
			t.Errorf("%-45q called %s, want it answered without a tool", c.Say, called)
		case len(c.Tool) > 0 && called == "":
			wrong++
			t.Errorf("%-45q called nothing, want one of %v", c.Say, c.Tool)
		case !among(called, c.Tool):
			wrong++
			t.Errorf("%-45q called %s, want one of %v", c.Say, called, c.Tool)
		default:
			var missing []string
			for _, want := range c.Args {
				if !strings.Contains(strings.ToLower(args), strings.ToLower(want)) {
					missing = append(missing, want)
				}
			}
			if len(missing) > 0 {
				wrong++
				t.Errorf("%-45q called %s with %s, missing %v", c.Say, called, args, missing)
			} else {
				right++
			}
		}
	}

	t.Logf("%d of %d right", right, right+wrong)
}

// ask : What the model does with one thing somebody said.
//
// One call, not the whole loop: what is being measured is the choice, and the
// choice is made before anything runs.
func ask(t *testing.T, env *platformai.Environment, registry *tool.Registry, channel chat.Channel, say, after string) (string, string, string) {
	t.Helper()

	reachable := registry.For(channel)
	specs := make([]environment.ToolSpec, 0, len(reachable))
	for _, x := range reachable {
		schema, err := json.Marshal(x.Params)
		if err != nil {
			t.Fatalf("%s has an unusable schema: %v", x.Name, err)
		}
		specs = append(specs, environment.ToolSpec{
			Name: x.Name, Description: x.Description(), Parameters: schema,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// With EVAL_LOOK_FIRST=1, the question carries what the runner
	// now puts after it, so the suite measures the prompt production
	// actually sends rather than the bare question.
	if os.Getenv("EVAL_LOOK_FIRST") == "1" {
		say = prompt.Block(say, lookFirstDraft)
	}
	req := environment.Request{Prompt: say, Tools: specs}
	if after != "" {
		req.History = []environment.Turn{{Role: environment.RoleAssistant, Text: after}}
	}
	stream, err := env.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var final *environment.Message
	for msg := range stream {
		if msg.Kind.Terminal() {
			m := msg
			final = &m
		}
	}
	if final == nil {
		t.Fatalf("%q got no answer at all", say)
	}
	if final.Kind == environment.KindError {
		t.Fatalf("%q failed: %s", say, final.Detail)
	}
	if len(final.ToolCalls) == 0 {
		return "", "", final.Text
	}
	return final.ToolCalls[0].Name, final.ToolCalls[0].Arguments, final.Text
}

// among : Whether the tool called is one of those that would be right.
func among(called string, right []string) bool {
	for _, name := range right {
		if name == called {
			return true
		}
	}
	return len(right) == 0 && called == ""
}

// nothingComing : A look-ahead with nothing outstanding.
//
// What is measured here is which tool the model reaches for, and
// that is decided before anything runs.
type nothingComing struct{}

func (nothingComing) Held(context.Context, string, time.Time) ([]coming.Judged, error) {
	return nil, nil
}
func (nothingComing) Settle(context.Context, string, string, time.Time) error { return nil }
func (nothingComing) Trim(context.Context, string, string, string, time.Time) error {
	return nil
}

// quietWeb : A web that answers nothing. What is measured here is
// which tool the model reaches for, which is decided before
// anything runs.
type quietWeb struct{}

func (quietWeb) Where() string { return "test" }
func (quietWeb) Search(context.Context, search.Ask) ([]search.Result, error) {
	return nil, nil
}
