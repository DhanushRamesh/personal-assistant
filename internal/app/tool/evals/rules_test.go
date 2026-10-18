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
	"personal-assistant/internal/app/tool/events"
	"personal-assistant/internal/app/tool/reminders"
	"personal-assistant/internal/app/tool/rules"
	"personal-assistant/internal/base/platform/config"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment/platformai"
	eventmemory "personal-assistant/internal/base/world/event/inmemory"
	"personal-assistant/internal/base/world/routine"
	jobmemory "personal-assistant/internal/domain/job/inmemory"
	"personal-assistant/internal/domain/persona"
	remindmemory "personal-assistant/internal/domain/remind/inmemory"
)

// ruleCases : Writing a rule, and the mistakes worth measuring.
//
// Owner, 7 October 2026: *"this is an important rule, the AI must
// know how to use it well without making mistakes. Is there any way
// to check if the AI created or updated a rule properly?"*
//
// The tools refuse a rule that cannot be read, and a rule is tried
// the moment it is written so one that can never fire says so. What
// neither catches is a rule that is well formed, fires, and is not
// what was asked for. That is what this measures.
//
// Three confusions are the whole point of it:
//
//   - a rule against a reminder. "Tell me every morning" is a
//     reminder; "tell me whenever the battery drops" is a rule, and
//     the difference is whether a time or a happening sets it off.
//   - changing one against making a second. The easy mistake, and
//     how six battery rules came to exist where one would have done.
//   - putting one aside against calling it off. One comes back and
//     the other is gone for good.
//
// # What this measures, and what it does not
//
// The first move. A write refuses to run until its domain has been
// listed, so reaching for rule_list is as right as reaching for
// rule_add and both are accepted: what is being measured is whether
// the model goes to the rules at all, rather than to the events or
// the reminders, which is where every failure so far has gone.
//
// It does not measure the rule that comes out. That needs the round
// after the listing, and sending one means handing the gateway back
// the identifier it gave the first call -- which ask throws away.
// Threading it through is the next thing here; until then a rule's
// arguments are checked by job.New refusing them, by the trial that
// runs the moment it is written, and by Try on the page.
var ruleCases = []eval{
	// Making one, and the forms of the schedule.
	{Say: "tell me whenever the battery drops below fifteen",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "every morning at eight tell me what is coming up",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "check every twenty minutes whether anything is amiss",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "when I plug the phone in, check a quarter of an hour later that it is actually charging",
		Tool: []string{"rule_add", "rule_list"}},

	// Reaching them when they are not in the room.
	//
	// Almost every rule belongs where somebody is standing, and a few
	// -- the phone about to die before they go out -- are worth most
	// exactly when they are not. The rule says which, so the model has
	// to set it when asked, and the listing is given here because
	// changing a rule means reading one first.
	//
	// Measured because the words for it are not the word for it:
	// nobody says "anywhere". They say wherever I am, even when I am
	// out, on my phone.
	{Say: "make the low battery rule reach me wherever I am",
		After: "Your standing rules: \"low battery with something on\" (job_01M4BSFQ6EM7HYC9YR7WM4J3WR) every 10m; \"a call nobody returned\" (job_01M4BFQJE1JBW2G3S5XAHJ7H47) every 30m, kept 6h.",
		Tool:  []string{"rule_update"},
		Args:  []string{"anywhere", "true"}},
	{Say: "I want to hear that one even when I am not in the room",
		After: "Your standing rules: \"low battery with something on\" (job_01M4BSFQ6EM7HYC9YR7WM4J3WR) every 10m; \"a call nobody returned\" (job_01M4BFQJE1JBW2G3S5XAHJ7H47) every 30m, kept 6h.",
		Tool:  []string{"rule_update"},
		Args:  []string{"anywhere", "true"}},
	{Say: "tell me when the battery is nearly flat, and I want it on my phone when I am out",
		After: "Your standing rules: \"low battery with something on\" (job_01M4BSFQ6EM7HYC9YR7WM4J3WR) every 10m; \"a call nobody returned\" (job_01M4BFQJE1JBW2G3S5XAHJ7H47) every 30m, kept 6h.",
		Tool:  []string{"rule_add"},
		Args:  []string{"anywhere"}},

	// And the other way, so it is not simply always set. Most rules
	// are asked for without a word about where, and those belong in
	// the room.
	{Say: "remind me every half hour to stand up",
		After:   "Your standing rules: \"low battery with something on\" (job_01M4BSFQ6EM7HYC9YR7WM4J3WR) every 10m; \"a call nobody returned\" (job_01M4BFQJE1JBW2G3S5XAHJ7H47) every 30m, kept 6h.",
		Tool:    []string{"rule_add"},
		NotArgs: []string{"anywhere"}},

	// Conditions.
	{Say: "warn me about the battery, but only after ten in the morning",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "tell me about that on weekdays only",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "let me know if a missed call has not been returned, and keep it for the afternoon",
		Tool: []string{"rule_add", "rule_list"}},
	{Say: "tell me when the laptop battery is lower than it was an hour ago",
		Tool: []string{"rule_add", "rule_list"}},

	// Changing one rather than making a second. The easy mistake.
	{Say: "make the battery warning twenty-five instead of twenty",
		Tool: []string{"rule_update", "rule_list"}, Never: []string{"created"}},
	{Say: "change that rule to only run on weekends",
		Tool: []string{"rule_update", "rule_list"}, Never: []string{"created"}},
	{Say: "have it say something different when that happens",
		Tool: []string{"rule_update", "rule_list"}},

	// Putting one aside rather than calling it off.
	{Say: "give the battery rule a rest for a week",
		Tool: []string{"rule_quiet", "rule_list"}},
	{Say: "stop telling me about that until Monday",
		Tool: []string{"rule_quiet", "rule_list"}},
	{Say: "I am finished with that rule, get rid of it",
		Tool: []string{"rule_cancel", "rule_list"}},

	// A rule against a reminder. A time somebody named, once, is a
	// reminder however much it sounds like automation.
	{Say: "remind me to take the tablets at nine",
		Tool: []string{"reminder_set"}, Never: []string{"rule"}},
	{Say: "remind me to call Amma tomorrow evening",
		Tool: []string{"reminder_set"}},
	{Say: "what rules have I got", Tool: []string{"rule_list"}},
	{Say: "what runs automatically", Tool: []string{"rule_list"}},
}

// TestTheModelWritesARuleProperly : The same harness as the tool
// choice suite, over the rule tools and the two it is most likely to
// confuse them with.
func TestTheModelWritesARuleProperly(t *testing.T) {
	clock := reminders.Clock{Location: time.UTC}
	registry, err := tool.NewRegistry(slices.Concat(
		rules.All(jobmemory.New(), rules.Clock{Location: time.UTC},
			func() string { return "job_01M48E09RJW0G247RBST6712D1" },
			func(string) (tool.Tool, bool) { return tool.Tool{}, false },
			// usually, then the trier and the writer. A writer with
			// no model in it refuses rather than writes, which is
			// what this suite wants: it measures whether the model
			// picks the right tool and fills its arguments, not
			// what a second model would then do with them.
			settled{}, nil, rules.Writer{}),
		reminders.All(remindmemory.New(), clock, nil),
		events.Tools(eventmemory.New(), events.Clock{Location: time.UTC}),
	)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	registry.Add(tool.Describing(registry))

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
	var right, wrong, asked int
	for _, c := range ruleCases {
		if only != "" && !strings.Contains(strings.ToLower(c.Say), only) {
			continue
		}
		if asked > 0 {
			time.Sleep(Pace)
		}
		asked++

		channel := c.Channel
		if channel == "" {
			channel = chat.ChannelVoice
		}
		called, args, said := ask(t, env, registry, channel, c.Say, c.After, nil)

		var blurted []string
		for _, never := range c.Never {
			if strings.Contains(strings.ToLower(said), strings.ToLower(never)) {
				blurted = append(blurted, never)
			}
		}
		if len(blurted) > 0 {
			wrong++
			t.Errorf("%-60q said %v: %s", c.Say, blurted, said)
			continue
		}

		switch {
		case !among(called, c.Tool):
			wrong++
			t.Errorf("%-60q called %q, want one of %v", c.Say, called, c.Tool)
		default:
			var missing []string
			for _, want := range c.Args {
				if !strings.Contains(strings.ToLower(args), strings.ToLower(want)) {
					missing = append(missing, want)
				}
			}
			for _, never := range c.NotArgs {
				if strings.Contains(strings.ToLower(args), strings.ToLower(never)) {
					missing = append(missing, "not "+never)
				}
			}
			if len(missing) > 0 {
				wrong++
				t.Errorf("%-60q called %s without %v: %s", c.Say, called, missing, args)
				continue
			}
			right++
		}
	}
	t.Logf("%d right, %d wrong, of %d asked", right, wrong, asked)
}

// settled : A routine with one fact in it, so the listing offers
// what a rule may also stand on.
type settled struct{}

func (settled) All(_ context.Context, _ string) ([]routine.Fact, error) {
	return []routine.Fact{
		{Subject: "home", What: routine.Leaves, Hour: 10, Days: 4, Over: 6},
	}, nil
}
func (settled) Keep(context.Context, string, []routine.Fact) error { return nil }
