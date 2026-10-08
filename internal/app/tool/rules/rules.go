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
	"strconv"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/routine"
	"personal-assistant/internal/domain/job"
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
	// Quiet : Puts one aside until a moment, or lets it run again
	// with the zero time.
	Quiet(ctx context.Context, userID, id string, until time.Time) error
	// Change : Replaces one that exists, and forgets what it last
	// said.
	Change(ctx context.Context, j job.Job) error
	Stop(ctx context.Context, userID, id string) (bool, error)
}

// Trier : Runs a rule now and says what it would have said,
// without saying it.
//
// Optional. Nil leaves a rule made and unproven, which is how
// every rule was made before this.
type Trier func(ctx context.Context, j job.Job) job.Trial

// proving : What a rule did when it was tried the moment it was
// written.
//
// Owner, 7 October 2026: *"this is an important rule, the AI must
// know how to use it well without making mistakes. Is there any
// way to check if the AI created or updated a rule properly?"*
//
// The tools already refuse a rule that cannot be read -- an
// unreadable schedule, an hour of ninety-nine, a tool that
// writes. What nothing caught was a rule that reads perfectly and
// is wrong: a condition that can never hold, a trigger on the
// wrong thing, a lookup handing over the very evidence the
// trigger was meant to exclude. Two of those were written in one
// evening and both were found by running them afterwards.
//
// So a rule is run the moment it is made, and what it did comes
// back with it. The model sees in the same turn that its rule
// would say nothing, or would say the wrong thing, and can say so
// or mend it -- which is the cheapest moment there will ever be.
//
// It says nothing aloud and writes nothing down, so a rule made
// and immediately tried is in exactly the state it would have
// been in unmade.
func proving(ctx context.Context, try Trier, j job.Job) string {
	if try == nil {
		return ""
	}
	tried := try(ctx, j)
	if tried.Said != "" {
		return prompt.Text("Run just now, it would say:", strconv.Quote(tried.Said))
	}
	if tried.Why != "" {
		return prompt.Text("Run just now, it would say nothing:", tried.Why+".")
	}
	return "Run just now, it would say nothing."
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

// howToWake : What sets a rule off, written for the model.
var howToWake = prompt.Lines(
	"What sets the rule off. One or more, and ANY ONE of them fires it.",
	`  {"every": "30m"} -- over and over, at that spacing`,
	`  {"at": "08:00"} -- that hour, every day, in their own zone`,
	`  {"once": "2026-10-07 06:40"} -- one time, then finished`,
	`  {"cron": "0 9 * * 1-5"} -- minute, hour, day, month, weekday`,
	`  {"on": {"source": "battery.level", "op": "<", "value": 20}} -- when a reading crosses that line`,
	`  {"on": {"source": "power.plugged", "op": "arrives"}, "after": "15m"} -- when one lands, then wait`,
	"Use cron where the plain words cannot say when NOT to run: weekdays only, the first of the month.",
	`Use "arrives" for a thing that happens rather than a level that moves: a phone cannot always say what it was plugged into, and what matters is that it was.`,
	`"after" is for a question that cannot be answered at the moment it is asked -- whether the phone is really charging is only known later, by whether the level moved.`,
	"Two hooks means two ways in: the door opening or the window opening, one rule.",
	"Only a reading their devices send can be a hook. The hour, the day, presence, a routine, a connection and anything beginning ha. or counted are asked, never delivered, so they can qualify a rule and never start one.",
)

// howToPerform : The sequence, written for the model.
var howToPerform = prompt.Lines(
	"What to do when it holds, in order. No conditions here -- those all live in when.",
	`  {"do": "tool", "use": "event_recent", "with": {"kind": "battery.level", "limit": 1}} -- look something up`,
	`  {"do": "write", "ask": "say what the battery is at"} -- work the words out each time`,
	`  {"do": "say", "text": "Time to stand up."} -- the same words every time`,
	`  {"do": "relay"} -- say what the tool before it already worked out, word for word`,
	"It has to end in something being said: a write, a say, or a relay.",
	"Use say for words they dictated and write for anything that depends on the day -- a reading, the diary, who rang.",
	"Give a tool its arguments. A rule runs with nobody watching, and a tool's defaults are usually everything rather than the one thing the rule is about: a rule asking for the battery every minute, given no arguments, read out a reading from the day before.",
	"Reading tools only, at most three. A rule runs unattended and may run every few minutes; reading is safe to repeat and writing is not.",
	`A write may be given "tools" it can call for itself when the rule runs, where what is needed depends on what it finds.`,
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

// whetherToFollow : Whether a rule's words are worth following them
// out of the room, written for the model.
var whetherToFollow = prompt.Lines(
	"Whether what this rule says should reach them when they are not in the room. Leave it out for almost every rule.",
	"Only for something that matters most when they are out: the phone about to run out before they get home, a door left unlocked.",
	"Not for anything the room is the point of. Said anywhere, the words go out to their phone through a push service, so ask for it only when being out is exactly when they need to know.",
)

// howLongToCool : How long a rule holds off after speaking,
// written for the model.
var howLongToCool = prompt.Lines(
	"How long to say nothing after it has spoken. Leave it out for most rules.",
	`  "1h" -- having said something, stay quiet for an hour however often it runs`,
	"A rule already refuses to say the same sentence twice. This is for the one that would say the same news in different words, which that cannot catch.",
	"Only use it where being told twice is worse than being told late.",
)

// howToWhen : The conditions, written for the model.
var howToWhen = prompt.Lines(
	"Whether it holds, checked each time it runs. Leave it out unless they said one.",
	`  {"all": [ ... ]} -- every one must hold`,
	`  {"any": [ ... ]} -- one of them is enough`,
	"Either may hold a condition or another group, so (here or late) and (the battery is low) can be said.",
	"One condition is:",
	`  {"source": "battery.level", "op": "<", "value": 20}`,
	`  {"source": "presence", "op": "==", "value": "here"} -- also !=, <, >, <=, >=`,
	`  {"source": "power.plugged", "op": "arrives"} -- any reading of that kind, whatever it says`,
	`  {"source": "battery.level", "op": "older_than", "value": "2h"} -- nothing reported for that long`,
	`  {"source": "hour", "op": ">=", "value": 10} -- the hour where they are, 0 to 23`,
	`  {"source": "day", "op": "==", "value": "weekend"} -- or weekday, or saturday`,
	`  {"source": "usually.home.leaves", "op": "older_than", "value": "90m"} -- past when they usually go`,
	`  {"source": "link.google", "op": "==", "value": "broken"} -- a permission that has stopped working`,
	`  {"source": "ha.sensor.pixel_7_battery_state", "op": "==", "value": "charging"} -- what Home Assistant says NOW, not what was last reported`,
	`  {"source": "battery.level", "op": "<=", "ago": "15m"} -- no higher than it was then, so it is not rising`,
	`  {"count": {"kind": "call.missed", "by": "value", "within": "2h"}, "op": ">=", "value": 2} -- how many arrived, counted per caller`,
	`A count takes "kind" and "within"; "is" narrows it to one value and "by": "value" counts each caller separately, so this is "somebody has tried twice" without naming anybody.`,
	`"ago" is its own key and never a value: it compares a reading with its own earlier self, which is the only way to know whether something is moving.`,
	"The name in source is an event kind their devices report, or one of the above.",
	"If nothing is known about it, the condition does not hold and the rule stays quiet.",
)

// All : Every rule tool, in the order they are offered.
func All(store Store, clock Clock, id func() string, look Lookup,
	usually routine.Store, try Trier, writer Writer,
) []tool.Tool {
	return []tool.Tool{
		list(store, clock, usually),
		add(store, clock, id, look, try, writer),
		update(store, clock, look, try, writer),
		quieten(store, clock),
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
				return tool.Broke("Their rules could not be read.", err)
			}
			return tool.OK(prompt.Block(
				describe(found, clock),
				conditions(ctx, usually, in.Caller.UserID),
			))
		},
	}
}

// add : Sets one up.
func add(store Store, clock Clock, id func() string, look Lookup, try Trier, writer Writer) tool.Tool {
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
			Required: []string{"name", "asked"},
			Properties: map[string]tool.Property{
				"name": {
					Type: "string",
					Description: "A short name in their own words, how they would refer to it later. " +
						"\"the morning diary\", \"low battery warning\".",
				},
				"asked": {
					Type: "string",
					Description: "What the rule should do, in their own words, as fully as they said it. " +
						"Something that only writes rules is given this, along with what their devices " +
						"report and the rules they already have, and works out the rest. " +
						"Add anything you know that their words leave out -- which place they mean, " +
						"what they call it, which of two things they were talking about -- and leave " +
						"out nothing they said, including the part that sounds like a detail.",
				},
				"keep":     {Type: "string", Description: howLongToKeep},
				"cool":     {Type: "string", Description: howLongToCool},
				"anywhere": {Type: "boolean", Description: whetherToFollow},
			},
		},
		Examples: []tool.Example{
			{
				Ask: "every morning at eight tell me what is coming up",
				Args: `{"saying":"setting that up","name":"the morning diary",` +
					`"asked":"every morning at eight, say what is coming up in their diary that day"}`,
			},
			{
				Ask: "whenever my battery drops below twenty say something",
				Args: `{"saying":"setting that up","name":"low battery warning",` +
					`"asked":"whenever the phone battery drops below twenty per cent, say so and what it is at"}`,
			},
			{
				Ask: "every two hours remind me to stand up, but only when I am here",
				Args: `{"saying":"setting that up","name":"stand up",` +
					`"asked":"every two hours say \"Time to stand up.\", but only while they are in the room"}`,
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Name     string `json:"name"`
				Asked    string `json:"asked"`
				Keep     string `json:"keep"`
				Cool     string `json:"cool"`
				Anywhere bool   `json:"anywhere"`
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

			name := strings.TrimSpace(args.Name)
			if name == "" {
				return tool.Failed("A rule needs a name, so they can ask for it later.")
			}
			if err := job.Kept(args.Keep); err != nil {
				return tool.Failed(readable(err))
			}

			// Their own rules, as examples, because a rule of
			// theirs is about their devices, their places and
			// their way of saying things.
			held, err := store.Mine(ctx, in.Caller.UserID, Kind, job.Limit)
			if err != nil {
				return tool.Broke("Their rules could not be read.", err)
			}
			when, acts, bad := writer.Written(ctx, Request{
				UserID: in.Caller.UserID, Asked: args.Asked,
				Holding: examples(held),
			}, look)
			if bad != "" {
				return tool.Failed(bad)
			}

			made, err := job.Make(id(), in.Caller.UserID, name, Kind,
				when, acts, clock.now().In(clock.where()))
			if err != nil {
				return tool.Failed(readable(err))
			}
			made.Keep = strings.TrimSpace(args.Keep)
			if err := job.Kept(args.Cool); err != nil {
				return tool.Failed(readable(err))
			}
			made.Cool = strings.TrimSpace(args.Cool)
			made.Anywhere = args.Anywhere
			if err := store.Keep(ctx, made); err != nil {
				return tool.Broke("That rule could not be written down.", err)
			}
			return tool.OK(prompt.Block(
				prompt.Text("Created:", one(made, clock)),
				proving(ctx, try, made),
			))
		},
	}
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
func update(store Store, clock Clock, look Lookup, try Trier, writer Writer) tool.Tool {
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
				"name": {Type: "string", Description: "A new name for it."},
				"asked": {
					Type: "string",
					Description: "What should change about it, in their own words. " +
						"The same thing that writes rules is given this along with the rule as it " +
						"stands, and changes only what they asked about. " +
						"Leave it out when nothing about what the rule does is changing -- when they " +
						"are only renaming it, or asking for it to reach them wherever they are.",
				},
				"keep":     {Type: "string", Description: howLongToKeep},
				"cool":     {Type: "string", Description: howLongToCool},
				"anywhere": {Type: "boolean", Description: whetherToFollow},
			},
		},
		Examples: []tool.Example{
			{
				Ask: "make the battery one twenty-five instead of twenty",
				Args: `{"saying":"changing that","id":"job_01M48E09RJW0G247RBST6712D3",` +
					`"asked":"set it off at twenty-five per cent instead of twenty"}`,
			},
			{
				Ask: "only tell me that on weekdays",
				Args: `{"saying":"changing that","id":"job_01M48E09RJW0G247RBST6712D3",` +
					`"asked":"only say it on weekdays"}`,
			},
			{
				Ask:  "stop sending that one to my phone",
				Args: `{"saying":"changing that","id":"job_01M48E09RJW0G247RBST6712D3","anywhere":false}`,
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID       string  `json:"id"`
				Name     *string `json:"name"`
				Asked    string  `json:"asked"`
				Keep     *string `json:"keep"`
				Cool     *string `json:"cool"`
				Anywhere *bool   `json:"anywhere"`
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
				return tool.Broke("Their rules could not be read.", err)
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

			// What the rule does is rewritten whole or left
			// alone. Renaming one, or asking for it to reach them
			// wherever they are, changes nothing about what it
			// does -- and a rule rewritten in order to be renamed
			// is a rule that could come back different.
			when, acts := was.When, was.Perform
			if strings.TrimSpace(args.Asked) != "" {
				var bad string
				when, acts, bad = writer.Written(ctx, Request{
					UserID: in.Caller.UserID, Asked: args.Asked,
					Holding: examples(others(held, was.ID)), Was: &was,
				}, look)
				if bad != "" {
					return tool.Failed(bad)
				}
			}
			if err := job.Kept(given(args.Keep, was.Keep)); err != nil {
				return tool.Failed(readable(err))
			}

			// Through job.Make, so a changed rule is read by the
			// same thing that reads a new one and cannot be edited
			// into a state it could not have been made in.
			made, err := job.Make(was.ID, was.UserID,
				given(args.Name, was.Name), Kind,
				when, acts, clock.now().In(clock.where()))
			if err != nil {
				return tool.Failed(readable(err))
			}
			made.Keep = given(args.Keep, was.Keep)
			if err := job.Kept(given(args.Cool, was.Cool)); err != nil {
				return tool.Failed(readable(err))
			}
			made.Cool = given(args.Cool, was.Cool)
			made.Anywhere = was.Anywhere
			if args.Anywhere != nil {
				made.Anywhere = *args.Anywhere
			}
			// What it last said is dropped on purpose. The rule is
			// not the rule it was, so the sentence it is holding
			// back is no longer a repeat of anything -- and
			// somebody who has just changed a rule wants to hear
			// whether it works.
			if err := store.Change(ctx, made); err != nil {
				return tool.Broke("That rule could not be changed.", err)
			}
			return tool.OK(prompt.Block(
				prompt.Text("Changed:", one(made, clock)),
				proving(ctx, try, made),
			))
		},
	}
}

// quieten : Puts one aside for a while, or brings it back.
//
// Cancelling was the only way to stop a rule, and it loses the
// rule: somebody who wanted a week's peace had to remember how it
// was written in order to have it again. Being quiet is not being
// cancelled -- it comes back by itself, and nothing has to be
// rewritten.
func quieten(store Store, clock Clock) tool.Tool {
	return tool.Tool{
		Name:    "rule_quiet",
		Domain:  Kind,
		Writes:  true,
		Purpose: "Stop a standing rule for a while, or start it again.",
		UseWhen: "They want one to stop for now rather than for good: not this week, give it a rest, " +
			"stop telling me that until Monday. And the other way: start that one again, " +
			"you can tell me about that now.",
		Avoid: "Not for a rule they are finished with -- that is rule_cancel, and this one comes " +
			"back on its own. The identifier must come from a listing in this turn.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Required: []string{"id"},
			Properties: map[string]tool.Property{
				"id": {
					Type:        "string",
					Description: "The rule's identifier, exactly as the listing gave it.",
					Pattern:     idPattern,
				},
				"how_long": {
					Type: "string",
					Description: "How long to stay quiet, as a length of time: \"7d\", \"2h\", " +
						"\"30m\". Leave it out to start the rule again now.",
				},
			},
		},
		Examples: []tool.Example{
			{
				Ask:  "give the battery one a rest for a week",
				Args: `{"saying":"quieting that","id":"job_01M48E09RJW0G247RBST6712D3","how_long":"7d"}`,
			},
			{
				Ask:  "you can tell me about the battery again",
				Args: `{"saying":"starting that again","id":"job_01M48E09RJW0G247RBST6712D3"}`,
			},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				ID      string `json:"id"`
				HowLong string `json:"how_long"`
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

			var until time.Time
			if how := strings.TrimSpace(args.HowLong); how != "" {
				if err := job.Kept(how); err != nil {
					return tool.Failed(readable(err))
				}
				long, err := job.Span(how)
				if err != nil {
					return tool.Failed(readable(err))
				}
				until = clock.now().Add(long)
			}
			if err := store.Quiet(ctx, in.Caller.UserID, args.ID, until); err != nil {
				return tool.Broke("That rule could not be put aside.", err)
			}
			if until.IsZero() {
				return tool.OK("That rule is running again.")
			}
			return tool.OK(prompt.Text("Quiet until",
				until.In(clock.where()).Format("Monday 2 January, 3:04 pm")+"."))
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
				return tool.Broke("That rule could not be stopped.", err)
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

// allowed : Whether a sequence may run, and why not.
//
// Everything assemble used to refuse, asked of a sequence the model
// wrote itself rather than of the parts it was given.
//
// A rule runs unattended and may run every few minutes. Reading is
// safe to repeat; writing is not, and nobody is there to notice the
// first time it goes wrong.
func allowed(acts []job.Act, look Lookup) string {
	if len(acts) == 0 {
		return "A rule has to do something. Give it at least one thing to perform."
	}
	tools := 0
	for _, a := range acts {
		switch a.Do {
		case job.DoSay:
			if strings.TrimSpace(a.Text) == "" {
				return "A say needs the words to say."
			}
		case job.DoWrite:
			if strings.TrimSpace(a.Ask) == "" {
				return "A write needs to be told what to work out and say."
			}
			if names, bad := reading(a.Tools, look); bad != "" {
				return bad
			} else if len(names) != len(a.Tools) {
				return "Some of those tools could not be read."
			}
		case job.DoTool:
			tools++
			if tools > Most {
				return fmt.Sprintf("A rule may look up at most %d things.", Most)
			}
			name := strings.TrimSpace(a.Use)
			found, ok := look(name)
			if !ok {
				return fmt.Sprintf("There is no tool called %q.", name)
			}
			if found.Writes {
				return fmt.Sprintf(
					"%s changes things, and a rule may only look things up.", name)
			}
			if len(a.With) == 0 {
				return fmt.Sprintf(
					"%s needs its arguments. A rule runs with nobody watching, "+
						"and without them it answers from the wrong row.", name)
			}
		case job.DoRelay:
		default:
			return fmt.Sprintf("There is nothing a rule does called %q.", a.Do)
		}
	}
	// It has to end in something being said, or it is a rule that
	// looks things up and tells nobody.
	if !job.Speaks(acts) {
		return prompt.Text(
			"A rule has to end in something being said.",
			"Finish it with a say for words they dictated, a write for anything that depends on the day, or a relay where the tool before it has already worked the words out.",
		)
	}
	return ""
}

// others : Their rules, without the one being changed.
//
// That one is shown whole and on its own, as the thing being
// changed. Listing it twice would have the writer reading it as an
// example of a rule to copy as well as the rule to mend.
func others(held []job.Job, id string) []job.Job {
	out := make([]job.Job, 0, len(held))
	for _, j := range held {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return out
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
	if j.Cool != "" {
		parts = append(parts, "quiet for "+j.Cool+" after speaking")
	}
	if j.Anywhere {
		parts = append(parts, "said wherever they are")
	}
	if !j.Quiet.IsZero() && j.Quiet.After(clock.now()) {
		parts = append(parts, "quiet until "+j.Quiet.In(clock.where()).Format("Mon 2 Jan 3:04 pm"))
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
