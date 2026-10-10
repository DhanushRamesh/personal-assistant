//go:build evals

package evals

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/app/tool/calendar"
	callingtool "personal-assistant/internal/app/tool/calling"
	comingtool "personal-assistant/internal/app/tool/coming"
	"personal-assistant/internal/app/tool/conversations"
	"personal-assistant/internal/app/tool/events"
	"personal-assistant/internal/app/tool/mail"
	mapstool "personal-assistant/internal/app/tool/maps"
	"personal-assistant/internal/app/tool/memories"
	"personal-assistant/internal/app/tool/places"
	"personal-assistant/internal/app/tool/reminders"
	"personal-assistant/internal/app/tool/tasks"
	webtool "personal-assistant/internal/app/tool/web"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/embed"
	"personal-assistant/internal/base/thinking/environment/platformai"
	"personal-assistant/internal/base/world/calling"
	eventmemory "personal-assistant/internal/base/world/event/inmemory"
	"personal-assistant/internal/domain/memory"
	"personal-assistant/internal/domain/memory/inmemory"
	"personal-assistant/internal/domain/persona"
	remindmemory "personal-assistant/internal/domain/remind/inmemory"
)

// tangled : One thing said the way this person types it, and the same
// thing said in plain English.
//
// Both are asked and the two answers compared, so what is measured is
// the spelling alone. Which tool a tidy sentence reaches for is the
// subject of [cases]; this asks only whether mangling the sentence
// costs anything.
type tangled struct {
	// Said : The sentence as it is actually typed.
	Said string
	// Tidy : The same request, spelt correctly.
	Tidy string
	// Tool : The tools that would be a right first move, any one of
	// them. Empty means no tool should be called at all.
	Tool []string
	// Args : Fragments that must appear in the arguments, if any.
	//
	// Only fragments a misspelling cannot legitimately change. A
	// date or a number survives a mangled sentence; a word lifted
	// out of it may not.
	Args []string
	// Channel : How it arrived. Voice cannot reach everything.
	Channel chat.Channel
}

// mangled : What this person types, paired with what they meant.
//
// Taken from sentences actually typed rather than invented, because
// invented misspellings are regular and real ones are not: dropped
// letters, transposed pairs, adjacent keys, double dots for a comma,
// and whole words left out.
var mangled = []tangled{
	// The diary.
	{Said: "whsts on tomorow", Tidy: "what is on tomorrow",
		Tool: []string{"calendar_events"}},
	{Said: "can i asj what are te evensts on september 3rd",
		Tidy: "can I ask what the events are on the 3rd of September",
		Tool: []string{"calendar_events"}, Args: []string{"09-03"}},
	{Said: "am i fre on thrusday afternon", Tidy: "am I free on Thursday afternoon",
		Tool: []string{"calendar_free", "calendar_events"}},
	{Said: "put the dentst in for tuseday at four",
		Tidy: "put the dentist in for Tuesday at four",
		Tool: []string{"calendar_add", "calendar_events", "calendar_free"}},

	// Timers. The number is the part that must survive.
	{Said: "set a timr for twenty mins", Tidy: "set a timer for twenty minutes",
		Tool: []string{"reminder_set"}, Args: []string{"20"}},
	{Said: "set a timr for thrity secnds", Tidy: "set a timer for thirty seconds",
		Tool: []string{"reminder_set"}, Args: []string{"seconds_from_now", "30"}},
	{Said: "remind me in to hours to take the washign out",
		Tidy: "remind me in two hours to take the washing out",
		Tool: []string{"reminder_set"}, Args: []string{"120"}},
	{Said: "wgat timers do i hav", Tidy: "what timers do I have",
		Tool: []string{"reminder_list"}},

	// Memory, asked for and not asked for.
	{Said: "rememebr that the roofr quoted forty thosand",
		Tidy: "remember that the roofer quoted forty thousand",
		Tool: []string{"memory_remember"}},
	{Said: "i cant take dary, it gves me a hedache",
		Tidy: "I cannot take dairy, it gives me a headache",
		Tool: []string{"memory_remember"}},
	{Said: "wgat did i tel you about the rooof",
		Tidy: "what did I tell you about the roof",
		Tool: []string{"memory_search"}},
	{Said: "forgte wgat i told you about the rooof",
		Tidy: "forget what I told you about the roof",
		Tool: []string{"memory_search", "memory_forget"}, Channel: chat.ChannelDirect},

	// Conversations.
	{Said: "wgat conversatins have i had", Tidy: "what conversations have I had",
		Tool: []string{"conversation_list"}},
	{Said: "go bak to the rooof conversaton", Tidy: "go back to the roof conversation",
		Tool: []string{"conversation_find"}},
	{Said: "strat a new conversaton", Tidy: "start a new conversation",
		Tool: []string{"conversation_new"}},

	// What happened, and where.
	{Said: "i said what did i listen to today, check",
		Tidy: "what did I listen to today",
		Tool: []string{"event_recent", "event_kinds"}},
	{Said: "wehn did i last leav the ofice", Tidy: "when did I last leave the office",
		Tool: []string{"place_visits", "event_recent"}},
	{Said: "hav i ben to the badmintn cort this wek",
		Tidy: "have I been to the badminton court this week",
		Tool: []string{"place_visits"}},
	{Said: "i eant to no whom i called today",
		Tidy: "I want to know who I called today",
		Tool: []string{"call_people", "event_recent"}},

	// The phone. A name is the part of a sentence a misspelling
	// cannot be reasoned back from, so the point here is that it
	// still reaches the tool that looks one up.
	{Said: "cal amma", Tidy: "call Amma",
		Tool: []string{"call_place", "call_people"}},
	{Said: "ring alekya and put it on speakrs",
		Tidy: "ring Alekhya and put it on speakers",
		Tool: []string{"call_place", "call_people"}},

	// The run-on style, where the request and an aside share a line.
	{Said: "i wana no whats on my plate tomorow..meetings and al",
		Tidy: "what is on my plate tomorrow, meetings and all",
		Tool: []string{"calendar_events"}},
	{Said: "my sistrs flight lands on the 3rd of march",
		Tidy: "my sister's flight lands on the 3rd of March",
		Tool: []string{"memory_remember", "calendar_add", "calendar_events"}},

	// Reaching for none. Mangling a sentence must not turn an
	// answer into a search, which is the failure that would be
	// invisible in a suite that only checks the right tool was
	// reached for when one was wanted.
	{Said: "wgat is the capitl of australa", Tidy: "what is the capital of Australia"},
	{Said: "i had dosa for brekfast", Tidy: "I had dosa for breakfast"},
	{Said: "wgat is twelv times eigth", Tidy: "what is twelve times eight"},
	{Said: "that tok longr than i exepcted", Tidy: "that took longer than I expected"},
}

// TestASentenceSurvivesItsSpelling : Whether typing badly costs the
// right tool.
//
// Each case is asked twice, once as typed and once spelt correctly.
// A failure is a case the tidy sentence got right and the tangled one
// did not: that is the spelling costing something, which is what this
// file is for.
//
// A case both of them get wrong is reported and not failed. The tool
// descriptions are what decide that, and [cases] is where they are
// measured; failing here as well would report one fault in two places
// and make this file fail for a reason it does not own.
func TestASentenceSurvivesItsSpelling(t *testing.T) {
	registry := everything(t)

	cfg, err := config.Load("../../../../config.ini", os.LookupEnv)
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
		SystemPrompt: persona.Prompt(cfg.Assistant.Persona, cfg.Assistant.Name, persona.Spoken),
		Timeout:      90 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}

	only := strings.ToLower(os.Getenv("EVAL_ONLY"))

	var held, cost, neither, gained int
	var asked int
	for _, c := range mangled {
		if only != "" && !strings.Contains(strings.ToLower(c.Said), only) {
			continue
		}

		channel := c.Channel
		if channel == "" {
			channel = chat.ChannelVoice
		}

		if asked > 0 {
			time.Sleep(Pace)
		}
		asked++
		tangledCalled, tangledArgs, _ := ask(t, env, registry, channel, c.Said, "")

		time.Sleep(Pace)
		tidyCalled, tidyArgs, _ := ask(t, env, registry, channel, c.Tidy, "")

		tangledOK := right(c, tangledCalled, tangledArgs)
		tidyOK := right(c, tidyCalled, tidyArgs)

		switch {
		case tangledOK && tidyOK:
			held++
		case tidyOK && !tangledOK:
			cost++
			t.Errorf("%-45q called %s %s\n%-45q called %s %s  <- the spelling cost it",
				c.Tidy, shown(tidyCalled), tidyArgs,
				c.Said, shown(tangledCalled), tangledArgs)
		case tangledOK && !tidyOK:
			gained++
			t.Logf("%q read correctly while %q did not, calling %s",
				c.Said, c.Tidy, shown(tidyCalled))
		default:
			neither++
			t.Logf("neither reading of %q is right: tidy called %s, tangled called %s, want %v",
				c.Tidy, shown(tidyCalled), shown(tangledCalled), c.Tool)
		}
	}

	t.Logf("%d of %d survived their spelling; %d cost by it, %d wrong either way, %d only right when mangled",
		held, held+cost+neither+gained, cost, neither, gained)
}

// right : Whether one answer is a right first move for this case.
func right(c tangled, called, args string) bool {
	if !among(called, c.Tool) {
		return false
	}
	for _, want := range c.Args {
		if !strings.Contains(strings.ToLower(args), strings.ToLower(want)) {
			return false
		}
	}
	return true
}

// shown : A tool name as it reads in a report, including no tool.
func shown(called string) string {
	if called == "" {
		return "nothing"
	}
	return called
}

// everything : The registry the runner builds, including the phone.
func everything(t *testing.T) *tool.Registry {
	t.Helper()

	recall := &memory.Recall{Store: inmemory.New(), Embedder: embed.Fake{}}
	registry, err := tool.NewRegistry(slices.Concat(
		conversations.All(nil),
		memories.All(recall),
		reminders.All(remindmemory.New(), reminders.Clock{Location: time.UTC}, nil),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
		calendar.All(nil, calendar.Clock{Location: time.UTC}),
		tasks.All(nil, tasks.Clock{Location: time.UTC}),
		mail.All(nil, mail.Clock{Location: time.UTC}),
		places.Tools(nil, places.Clock{Location: time.UTC}),
		comingtool.Tools(nothingComing{}, comingtool.Clock{Location: time.UTC}),
		webtool.Tools(quietWeb{}),
		mapstool.Tools(flatEarth{}, eventmemory.New(), mapstool.Clock{Location: time.UTC}),
		callingtool.Tools(eventmemory.New(), noPhone{}, nothingSaid{}, noPhone{},
			time.Now),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

// noPhone : A phone that takes every instruction and does nothing.
// Which tool is reached for is decided before anything runs.
type noPhone struct{}

func (noPhone) Ring(context.Context, string, calling.Call) error { return nil }
func (noPhone) Change(context.Context, string, calling.During, time.Duration) error {
	return nil
}

// nothingSaid : A transcript with nothing in it.
type nothingSaid struct{}

func (nothingSaid) SaidSince(context.Context, string, time.Time, int) ([]conversation.Message, error) {
	return nil, nil
}
