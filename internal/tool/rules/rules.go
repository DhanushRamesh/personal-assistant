// Package rules lets the assistant be given a standing instruction
// rather than asked for one thing now.
//
// A rule is a time or a happening, and a sequence of things to do
// when it comes round. Everything these tools accept is written
// out in the schedule and condition forms the engine already
// reads, so what the person said is what is stored and what is
// read back to them.
package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/job"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/routine"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// idPattern : The shape of a rule identifier, so one the model
// invented is refused before it reaches the database.
const idPattern = `^job_[0-9A-HJKMNP-TV-Z]{26}$`

// Most : How many tools one rule may look things up with.
//
// A rule is a sequence, not a program. Three readings is more than
// any sentence has needed and small enough that a rule firing
// every few minutes cannot become the busiest thing on the server.
const Most = 3

// Kind : What these rows are called when they are listed back.
const Kind = "rule"

// Store : Where rules are kept.
type Store interface {
	Mine(ctx context.Context, userID, kind string, limit int, states ...job.State) ([]job.Job, error)
	Keep(ctx context.Context, j job.Job) error
	// Change : Replaces one that exists, and forgets what it last
	// said.
	Change(ctx context.Context, j job.Job) error
	Stop(ctx context.Context, userID, id string) (bool, error)
}

// Lookup : Finds a tool by name, for checking what a rule may run.
type Lookup func(name string) (tool.Tool, bool)

// Clock : What these tools need to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour
	// means. Nil is UTC.
	Location *time.Location
}

func (c Clock) now() time.Time {
	if c.Now == nil {
		return time.Now().UTC()
	}
	return c.Now().UTC()
}

func (c Clock) where() *time.Location {
	if c.Location == nil {
		return time.UTC
	}
	return c.Location
}

// WhenUnasked : What the model is told about the rules it is handed
// before anybody asked for them.
var WhenUnasked = prompt.Text(
	"Reference, for one kind of question only.",
	"Say nothing about any of it unless they ask what standing rules they have, or about one of these in particular.",
	"A rule is a thing they set up once and expect to forget about.",
	"Listing them back unasked is the assistant talking about its own filing.",
)

// howToSchedule : The schedule forms, written for the model.
var howToSchedule = prompt.Lines(
	"When it runs. One of these four, written exactly like this:",
	`  "every 30m" -- over and over, at that spacing`,
	`  "at 08:00" -- that hour, every day, in their own zone`,
	`  "once 2026-10-07 06:40" -- one time, then finished`,
	`  "when battery.level < 20" -- no clock at all; it runs the moment a reading from their device crosses that line`,
	`  "when power.plugged arrives" -- any reading of that kind at all, whatever it says`,
	"Use arrives for a thing that happens rather than a level that moves. A phone cannot always say what it was plugged into, and what matters is that it was.",
	`A trigger may wait before it runs: "when power.plugged is true then 15m".`,
	"For a question that cannot be answered at the moment it is asked -- whether the phone is really charging is only known later, by whether the level moved.",
)

// most : [Most], addressable, for a schema bound.
var most = Most

// howToLookUp : What a lookup is for, written for the model.
var howToLookUp = prompt.Text(
	"Things to look up first, so that ask has something to work from.",
	"Reading tools only, at most three, and leave it out entirely unless ask needs them.",
	"Give each one its arguments: a lookup runs once a minute with nobody watching,",
	"and the tool's defaults are usually everything rather than the one thing the rule is about.",
)

// howToArgue : Why the arguments matter, written for the model.
//
// A rule asking for the battery every minute was given
// event_recent with no arguments, got a week of everything, and
// read out a reading from the day before. The tool could have
// answered it exactly -- a kind and a limit of one is how a thing
// stands now -- and nothing here let the rule say so.
var howToArgue = prompt.Text(
	"The tool's own arguments, as a JSON object written as a string:",
	`"{\"kind\":\"battery.level\",\"limit\":1}".`,
	"Work them out the same way you would for calling that tool yourself, for the question in ask.",
	"Without them the tool runs on its defaults, which for a reading means a week of every kind",
	"rather than what that thing says now, and the rule will answer from the wrong row.",
)

// howLongToKeep : How long what a rule says is worth saying,
// written for the model.
var howLongToKeep = prompt.Lines(
	"How long what this rule says is still worth hearing if they are not in the room when it runs. Leave it out for most rules.",
	`  "6h" -- keep it, and say it when they are next about`,
	"Only for something as true an hour later as it was at the time: a call nobody returned, something still waiting to be done.",
	"Leave it out for anything about this moment. A warning that the battery was at four percent, read out at the door that evening, is worse than saying nothing.",
)

// howToTools : When to let it decide for itself, written for the
// model.
var howToTools = prompt.Text(
	"Tools the rule may call for itself when it runs, choosing whether to and with what arguments.",
	"Reading tools only, at most three.",
	"Use this where what is needed depends on what it finds -- \"tell me if anything needs attention\" --",
	"and look_up where the rule always wants the same thing, which costs one round trip less.",
	"Giving both is fine: the lookups run first and the tools are there if more is wanted.",
)

// howToCondition : The condition forms, written for the model.
var howToCondition = prompt.Lines(
	"A condition checked each time it runs, which stops it when it does not hold. Leave it out unless they said one.",
	`  "battery.level < 20", "battery.level is not 100"`,
	`  "power.plugged arrives" -- any reading of that kind, whatever it says`,
	`  "battery.level older than 2h" -- nothing has been reported for that long`,
	`  "presence is here", "presence is away"`,
	`  "hour >= 10", "hour < 18" -- the hour of the day where they are, 0 to 23`,
	`  "day is weekday", "day is weekend", "day is saturday"`,
	`  "usually.home.leaves older than 90m" -- past the time they usually go`,
	`  "link.google is broken" -- a permission that has stopped working, so everything standing on it quietly does nothing`,
	`  "battery.level <= 15m ago" -- no higher than it was then, so it is not rising`,
	`  "battery.level > 1h ago" -- higher than it was an hour back`,
	`The value "15m ago" compares a reading with its own earlier self, which is the only way to know whether something is moving.`,
	"It needs readings going back that far, and says the condition does not hold when there are none rather than guessing.",
	"The name on the left is an event kind their devices report, or one of the above.",
	"Several alternatives may be joined with \"or\", and any one of them satisfies it: \"hour >= 22 or day is weekend\".",
	"For something that must hold as well, give another condition rather than joining it: each one stops the rule on its own, which is the and.",
	"If nothing is known about it, the condition does not hold and the rule stays quiet.",
)

// All : Every rule tool, in the order they are offered.
func All(store Store, clock Clock, id func() string, look Lookup, usually routine.Store) []tool.Tool {
	return []tool.Tool{
		list(store, clock, usually),
		add(store, clock, id, look),
		update(store, clock, look),
		cancel(store),
	}
}

// list : What standing rules exist.
func list(store Store, clock Clock, usually routine.Store) tool.Tool {
	return tool.Tool{
		Name:        "rule_list",
		Domain:      Kind,
		Lists:       true,
		WhenUnasked: WhenUnasked,
		Purpose:     "List the standing rules, with their identifiers.",
		UseWhen: "Any question about what standing rules or automations exist: what they have set up, " +
			"what runs automatically, whether something is still watching. Also when you need an " +
			"identifier in order to call one off.",
		Avoid: "This is not where reminders live. Something to be said once at a time they named is a " +
			"reminder; use reminder_list for those. Never answer from the conversation or from your own " +
			"earlier answer, which was true when you gave it and is not evidence now.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "what rules have I set up", Args: `{"saying":"checking your rules"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if store == nil {
				return tool.Failed("There is nowhere to keep rules on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			found, err := store.Mine(ctx, in.Caller.UserID, Kind, job.Limit)
			if err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(prompt.Block(
				describe(found, clock),
				conditions(ctx, usually, in.Caller.UserID),
			))
		},
	}
}

// add : Sets one up.
func add(store Store, clock Clock, id func() string, look Lookup) tool.Tool {
	return tool.Tool{
		Name:    "rule_add",
		Domain:  Kind,
		Writes:  true,
		Purpose: "Set up a standing rule: something to do on a schedule, or whenever a reading crosses a line.",
		UseWhen: "They describe something that should keep happening, or happen whenever some condition " +
			"comes about: every morning, every twenty minutes, whenever the battery drops, whenever they " +
			"get home. Anything with 'every', 'whenever' or 'each time' in it.",
		Avoid: "Not for one thing at one time -- that is reminder_set, and a rule is the wrong shape for it. " +
			"Not for doing something now; just do it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"name", "schedule"},
			Properties: map[string]tool.Property{
				"name": {
					Type: "string",
					Description: "A short name in their own words, how they would refer to it later. " +
						"\"the morning diary\", \"low battery warning\".",
				},
				"schedule": {Type: "string", Description: howToSchedule},
				"say": {
					Type: "string",
					Description: "The exact words to say, for when the same sentence will do every " +
						"time -- \"Time to stand up.\" If the sentence ought to mention anything that " +
						"changes, such as a reading or what is in the diary, use ask instead: these " +
						"words are fixed now and will be identical on the hundredth time it runs. " +
						"Give this or ask, never both.",
				},
				"ask": {
					Type: "string",
					Description: "What to work out and say, when the words depend on what is true at the " +
						"time and cannot be written now. An instruction to yourself, in the second person: " +
						"\"say what is in the diary today\". You will be asked this each time the rule runs, " +
						"and may answer that there is nothing worth saying. Give this or say, never both.",
				},
				"when": {Type: "string", Description: howToCondition},
				"keep": {Type: "string", Description: howLongToKeep},
				"tools": {
					Type:        "array",
					Description: howToTools,
					MaxItems:    &most,
					Items: &tool.Property{
						Type:        "string",
						Description: "A reading tool's name, such as event_recent.",
					},
				},
				"look_up": {
					Type:        "array",
					Description: howToLookUp,
					MaxItems:    &most,
					Items:       lookUpItem(),
				},
			},
		},
		Examples: []tool.Example{
			{
				Ask:  "every morning at eight tell me what is coming up",
				Args: `{"saying":"setting that up","name":"the morning diary","schedule":"at 08:00","ask":"say what is coming up in their diary today","look_up":[{"tool":"calendar_events","args":"{\"days\":1}"}]}`,
			},
			{
				Ask:  "whenever my battery drops below twenty say something",
				Args: `{"saying":"setting that up","name":"low battery warning","schedule":"when battery.level < 20","ask":"say that the battery is getting low, and what it is at","look_up":[{"tool":"event_recent","args":"{\"kind\":\"battery.level\",\"limit\":5}"}]}`,
			},
			{
				Ask:  "every two hours remind me to stand up, but only when I am here",
				Args: `{"saying":"setting that up","name":"stand up","schedule":"every 2h","when":"presence is here","say":"Time to stand up."}`,
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Name     string   `json:"name"`
				Schedule string   `json:"schedule"`
				Say      string   `json:"say"`
				Ask      string   `json:"ask"`
				When     string   `json:"when"`
				Keep     string   `json:"keep"`
				LookUp   []lookUp `json:"look_up"`
				Tools    []string `json:"tools"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("Those arguments could not be read.")
			}
			if store == nil {
				return tool.Failed("There is nowhere to keep rules on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			steps, bad := assemble(args.When, args.LookUp, args.Tools, args.Say, args.Ask, look)
			if bad != "" {
				return tool.Failed(bad)
			}

			name := strings.TrimSpace(args.Name)
			if name == "" {
				return tool.Failed("A rule needs a name, so they can ask for it later.")
			}
			if err := job.Kept(args.Keep); err != nil {
				return tool.Failed(readable(err))
			}
			made, err := job.New(id(), in.Caller.UserID, name, Kind,
				strings.TrimSpace(args.Schedule), steps, clock.now().In(clock.where()))
			if err != nil {
				return tool.Failed(readable(err))
			}
			made.Keep = strings.TrimSpace(args.Keep)
			if err := store.Keep(ctx, made); err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(prompt.Text("Created:", one(made, clock)))
		},
	}
}

// parts : A rule's arguments, read back off its steps.
//
// The sequence is assembled from these and never stored as them,
// so changing one of them means taking the rest back out. It is
// exact rather than nearly so: [assemble] writes one step per
// thing and nothing else goes in, which is what makes reading it
// back a matter of looking rather than of guessing.
func parts(j job.Job) (when, say, ask string, reads []lookUp, mayCall []string) {
	for _, step := range j.Steps {
		switch step.Do {
		case job.Call:
			reads = append(reads, lookUp{Tool: step.Tool, Args: string(step.Args)})
		case job.Check:
			when = step.When
		case job.Announce:
			say = step.Say
		case job.Write:
			ask, mayCall = step.Ask, step.Tools
		}
	}
	return when, say, ask, reads, mayCall
}

// given : The new value where one was given, and the old where
// none was.
//
// A change names what is changing and nothing else. Somebody
// moving a threshold should not have to say the words again, and
// asking them to is how the second rule gets written instead of
// the first being mended.
func given(now *string, before string) string {
	if now == nil {
		return before
	}
	return strings.TrimSpace(*now)
}

// update : Changes one, without making a second.
//
// Owner, 7 October 2026, on what the engine could not do: a rule
// could be made and called off and nothing else. So moving a
// threshold meant writing a new rule and cancelling the old one,
// and the six battery rules are what that looks like after a
// week -- near-duplicates, because tuning one was more work than
// adding another.
//
// Everything a rule is made of can be read back off its steps, so
// a change can name one thing and leave the rest alone.
func update(store Store, clock Clock, look Lookup) tool.Tool {
	return tool.Tool{
		Name:    "rule_update",
		Domain:  Kind,
		Writes:  true,
		Purpose: "Change a standing rule, leaving alone whatever is not mentioned.",
		UseWhen: "They want one altered rather than replaced: make it twenty-five instead of twenty, " +
			"only on weekdays, say it differently, check it every ten minutes. Anything of the form " +
			"change, move, make it, instead of.",
		Avoid: "Not for a reminder; that is reminder_update. The identifier must come from a listing in " +
			"this turn, never from one you remember. To stop one running use rule_cancel, not this.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Description: "The rule's identifier, exactly as the listing gave it.",
					Pattern:     idPattern,
				},
				"name":     {Type: "string", Description: "A new name for it."},
				"schedule": {Type: "string", Description: howToSchedule},
				"say":      {Type: "string", Description: "New fixed words for it to say."},
				"ask":      {Type: "string", Description: "A new thing for it to work out and say."},
				"when":     {Type: "string", Description: howToCondition},
				"keep":     {Type: "string", Description: howLongToKeep},
				"tools": {
					Type:        "array",
					Description: howToTools,
					MaxItems:    &most,
					Items:       &tool.Property{Type: "string"},
				},
				"look_up": {Type: "array", Description: howToLookUp, MaxItems: &most, Items: lookUpItem()},
			},
		},
		Examples: []tool.Example{
			{
				Ask:  "make the battery one twenty-five instead of twenty",
				Args: `{"saying":"changing that","id":"job_01M48E09RJW0G247RBST6712D3","schedule":"when battery.level < 25"}`,
			},
			{
				Ask:  "only tell me that on weekdays",
				Args: `{"saying":"changing that","id":"job_01M48E09RJW0G247RBST6712D3","when":"day is weekday"}`,
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID       string   `json:"id"`
				Name     *string  `json:"name"`
				Schedule *string  `json:"schedule"`
				Say      *string  `json:"say"`
				Ask      *string  `json:"ask"`
				When     *string  `json:"when"`
				Keep     *string  `json:"keep"`
				LookUp   []lookUp `json:"look_up"`
				Tools    []string `json:"tools"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("Those arguments could not be read.")
			}
			if store == nil {
				return tool.Failed("There is nowhere to keep rules on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			held, err := store.Mine(ctx, in.Caller.UserID, Kind, job.Limit)
			if err != nil {
				return tool.Failed(err.Error())
			}
			var was job.Job
			for _, j := range held {
				if j.ID == args.ID {
					was = j
					break
				}
			}
			if was.ID == "" {
				return tool.Failed("There is no rule with that identifier.")
			}

			when, say, ask, reads, mayCall := parts(was)
			// Words and a question are alternatives, so naming one
			// takes the other away. Otherwise changing a rule from
			// fixed words to a worked-out sentence would be
			// refused for having both.
			if args.Say != nil {
				ask = ""
			}
			if args.Ask != nil {
				say = ""
			}
			if args.LookUp != nil {
				reads = args.LookUp
			}
			if args.Tools != nil {
				mayCall = args.Tools
			}

			steps, bad := assemble(given(args.When, when), reads, mayCall,
				given(args.Say, say), given(args.Ask, ask), look)
			if bad != "" {
				return tool.Failed(bad)
			}
			if err := job.Kept(given(args.Keep, was.Keep)); err != nil {
				return tool.Failed(readable(err))
			}

			// Through job.New, so a changed schedule is read by the
			// same thing that reads a new one and a rule cannot be
			// edited into a state it could not have been made in.
			made, err := job.New(was.ID, was.UserID,
				given(args.Name, was.Name), Kind,
				given(args.Schedule, was.Schedule), steps,
				clock.now().In(clock.where()))
			if err != nil {
				return tool.Failed(readable(err))
			}
			made.Keep = given(args.Keep, was.Keep)
			// What it last said is dropped on purpose. The rule is
			// not the rule it was, so the sentence it is holding
			// back is no longer a repeat of anything -- and
			// somebody who has just changed a rule wants to hear
			// whether it works.
			if err := store.Change(ctx, made); err != nil {
				return tool.Failed(err.Error())
			}
			return tool.OK(prompt.Text("Changed:", one(made, clock)))
		},
	}
}

// cancel : Calls one off.
func cancel(store Store) tool.Tool {
	return tool.Tool{
		Name:    "rule_cancel",
		Domain:  Kind,
		Writes:  true,
		Purpose: "Call off a standing rule, so it stops running.",
		UseWhen: "They want one to stop: stop telling me, turn that off, I do not need that rule any more.",
		Avoid: "Not for a reminder; that is reminder_cancel. The identifier must come from a listing in " +
			"this turn, never from one you remember.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Description: "The rule's identifier, exactly as the listing gave it.",
					Pattern:     idPattern,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "turn off the morning diary", Args: `{"saying":"calling that off","id":"job_01M48E09RJW0G247RBST6712D3"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("Those arguments could not be read.")
			}
			if store == nil {
				return tool.Failed("There is nowhere to keep rules on this server.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			stopped, err := store.Stop(ctx, in.Caller.UserID, strings.TrimSpace(args.ID))
			if err != nil {
				return tool.Failed(err.Error())
			}
			if !stopped {
				return tool.Failed(prompt.Text(
					"No rule of theirs is running under that identifier.",
					"List them again and use one from the listing.",
				))
			}
			return tool.OK("Removed. It will not run again.")
		},
	}
}

// lookUpItem : What one lookup looks like, for a schema.
//
// Shared by making a rule and changing one, so the two cannot
// describe the same argument differently.
func lookUpItem() *tool.Property {
	return &tool.Property{
		Type:     "object",
		Required: []string{"tool"},
		Properties: map[string]tool.Property{
			"tool": {
				Type:        "string",
				Description: "The tool's name, such as event_recent or calendar_events.",
			},
			"args": {Type: "string", Description: howToArgue},
		},
	}
}

// lookUp : One thing a rule reads before it speaks.
type lookUp struct {
	// Tool : Which tool to run.
	Tool string `json:"tool"`
	// Args : Its arguments, as JSON written in a string.
	Args string `json:"args"`
}

// assemble : The sequence a rule's arguments describe.
//
// The order is fixed and not the model's to choose: look things up,
// then check, then speak. A condition is cheap and a lookup is not,
// but the check may be about something a lookup fetched -- so the
// readings come first and the one thing that can stop the rule
// comes last before it speaks.
func assemble(when string, reads []lookUp, mayCall []string, say, ask string, look Lookup) ([]job.Step, string) {
	say, ask = strings.TrimSpace(say), strings.TrimSpace(ask)
	switch {
	case say != "" && ask != "":
		return nil, prompt.Text(
			"A rule either says words you give it or works them out each time, not both.",
			"Use say for words they dictated, ask for anything that depends on the day.",
		)
	case say == "" && ask == "":
		return nil, "A rule has to end in something being said. Give either say or ask."
	}

	var steps []job.Step
	for _, read := range reads {
		name := strings.TrimSpace(read.Tool)
		if name == "" {
			continue
		}
		if len(steps) >= Most {
			return nil, fmt.Sprintf("A rule may look up at most %d things.", Most)
		}
		found, ok := look(name)
		if !ok {
			return nil, fmt.Sprintf("There is no tool called %q.", name)
		}
		if found.Writes {
			// A rule runs unattended and may run every few minutes.
			// Reading is safe to repeat; writing is not, and nobody
			// is there to notice the first time it goes wrong.
			return nil, fmt.Sprintf(
				"%s changes things, and a rule may only look things up.", name)
		}
		args, bad := arguments(name, read.Args)
		if bad != "" {
			return nil, bad
		}
		steps = append(steps, job.Step{Do: job.Call, Tool: name, Args: args})
	}

	if w := strings.TrimSpace(when); w != "" {
		if _, err := job.ParseConditions(w); err != nil {
			return nil, readable(err)
		}
		steps = append(steps, job.Step{Do: job.Check, When: w})
	}

	if ask != "" {
		allowed, bad := reading(mayCall, look)
		if bad != "" {
			return nil, bad
		}
		steps = append(steps, job.Step{Do: job.Write, Ask: ask, Tools: allowed})
	} else {
		steps = append(steps, job.Step{Do: job.Announce, Say: say})
	}
	return steps, ""
}

// reading : The tools a write may call, refusing any that write.
//
// A rule runs unattended and may run every few minutes. Reading
// is safe to repeat; writing is not, and nobody is there to
// notice the first time it goes wrong.
func reading(names []string, look Lookup) ([]string, string) {
	if len(names) == 0 {
		return nil, ""
	}
	if len(names) > Most {
		return nil, fmt.Sprintf("A rule may be given at most %d tools.", Most)
	}
	out := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		found, ok := look(name)
		if !ok {
			return nil, fmt.Sprintf("There is no tool called %q.", name)
		}
		if found.Writes {
			return nil, fmt.Sprintf(
				"%s changes things, and a rule may only read.", name)
		}
		out = append(out, name)
	}
	return out, ""
}

// arguments : A lookup's arguments, checked before they are stored.
//
// Refused here rather than at three in the morning, when the only
// sign would be a rule quietly answering from the wrong row.
func arguments(name, raw string) (json.RawMessage, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ""
	}
	var into map[string]any
	if err := json.Unmarshal([]byte(raw), &into); err != nil {
		return nil, fmt.Sprintf(
			"The arguments for %s are not a JSON object: %s", name, raw)
	}
	return json.RawMessage(raw), ""
}

// readable : An engine error as something worth saying back.
func readable(err error) string {
	return strings.TrimPrefix(err.Error(), "job: ")
}

// describe : The rules, for the model to read back.
func describe(found []job.Job, clock Clock) string {
	if len(found) == 0 {
		return "There are no standing rules."
	}
	sort.Slice(found, func(i, k int) bool { return found[i].Name < found[k].Name })

	lines := make([]string, 0, len(found))
	for _, j := range found {
		lines = append(lines, "- "+one(j, clock))
	}
	return prompt.Lines(lines...)
}

// conditions : What this person's routine offers a rule to stand
// on, or nothing.
//
// Owner, 7 October 2026: *"I will use that to write the rules, and
// further I'll ask the AI to write the rules by itself with
// examples of the existing rules, and it should use the routine
// too."*
//
// Which is why it is here rather than in a tool of its own.
// rule_add writes, and a writing tool refuses to run until the
// domain has been listed in the same turn -- so anything written
// into this listing is in front of whoever is about to write a
// rule, every time, without their having to know to ask.
//
// The rules above are the examples and these are the vocabulary.
// Together they are the whole of what somebody needs to write a
// new one, which was the point.
//
// Counted, because a condition standing on four days out of six is
// a different proposition from one standing on twenty, and the
// model deciding whether to use it should be able to see which.
func conditions(ctx context.Context, usually routine.Store, userID string) string {
	if usually == nil {
		return ""
	}
	held, err := usually.All(ctx, userID)
	if err != nil || len(held) == 0 {
		// Nothing known yet, which is the first weeks and is not
		// worth a sentence. A rule written without these is the
		// rule anybody would have written before there were any.
		return ""
	}

	lines := make([]string, 0, len(held))
	for _, f := range held {
		lines = append(lines, fmt.Sprintf("- %s -- %s %s around %s, on %d of %d days",
			f.Name(), f.Subject, f.What, f.Said(), f.Days, f.Over))
	}
	return prompt.Block(
		prompt.Text(
			"Besides what their devices report, a rule may be conditioned on what they usually do.",
			"These are worked out from weeks of their own readings and are used like any other condition:",
			`"usually.home.leaves older than 90m" is an hour and a half past the time they normally go,`,
			`and "usually.home.arrives within 30m" is the half hour after they normally get back.`,
			"They are conditions only. Nothing about them arrives, so none of them can be what a rule waits for.",
		),
		prompt.Lines(lines...),
	)
}

// one : A single rule on one line.
func one(j job.Job, clock Clock) string {
	parts := []string{fmt.Sprintf("%q (%s)", j.Name, j.ID), j.Schedule}
	if j.Keep != "" {
		parts = append(parts, "kept "+j.Keep)
	}
	if j.State != job.Active {
		parts = append(parts, string(j.State))
	}
	parts = append(parts, doing(j.Steps))
	if !j.Next.IsZero() {
		parts = append(parts, "next "+j.Next.In(clock.where()).Format("Mon 2 Jan 3:04 pm"))
	}
	if j.Why != "" {
		parts = append(parts, "last time: "+j.Why)
	}
	return strings.Join(parts, "; ")
}

// doing : What a rule's steps come to, in a phrase.
func doing(steps []job.Step) string {
	var out []string
	for _, s := range steps {
		switch s.Do {
		case job.Announce:
			out = append(out, fmt.Sprintf("says %q", s.Say))
		case job.Write:
			said := fmt.Sprintf("works out what to say about %q", s.Ask)
			if len(s.Tools) > 0 {
				said += ", and may call " + strings.Join(s.Tools, " or ")
			}
			out = append(out, said)
		case job.Check:
			out = append(out, "only if "+s.When)
		case job.Call:
			out = append(out, "looks up "+s.Tool)
		}
	}
	if len(out) == 0 {
		return "does nothing"
	}
	return strings.Join(out, ", then ")
}
