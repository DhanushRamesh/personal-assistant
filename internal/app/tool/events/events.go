// Package events : Lets the assistant read what the person's own devices
// reported.
//
// Everything else it knows, it knows because it was told. This is the one
// source that arrives unasked, and reading it is what turns a table that
// fills itself into something the person can ask about: when they got in,
// whether they have moved, how long they were out.
//
// Read on demand and never prefetched. This is the largest thing the
// person has and it grows every day; putting it into every prompt would
// cost every turn for the sake of the few that ask.
//
// Condensed rather than listed. A phone reports what it sees, and what it
// sees includes a network dropping and returning eight times over lunch.
// Handing all of that over spends the turn and gets the whole list read
// back; runs of the same thing are collapsed so that what remains is the
// shape of the day.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
)

const (
	// DefaultDays : How far back a question reaches when it does not say.
	//
	// Today. "What have I been doing" means today unless somebody says
	// otherwise, and a week of events answered to that question buries
	// the morning they were asking about.
	DefaultDays = 1

	// MaxDays : The furthest back one call may reach.
	MaxDays = 90

	// DefaultLimit, MaxLimit : How many events are read.
	//
	// Read before collapsing, so the limit is on rows rather than on
	// lines: a day of a flapping network is three hundred rows and four
	// lines.
	DefaultLimit = 300
	MaxLimit     = 1000

	// MaxLines : How many lines the answer may run to once collapsed.
	//
	// What is left after collapsing is the shape of a day, and a model
	// given more than this says all of it.
	MaxLines = 40

	// Together : How close two of the same thing must be to count as one
	// run.
	//
	// Nothing is collapsed across a gap longer than this, because a gap
	// is the information: joining the same network at nine and at six is
	// two facts, and eight times over lunch is one.
	Together = 2 * time.Hour
)

// Reader : Where the events are.
type Reader interface {
	// Recent : What happened, newest first by when it happened.
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
	// Kinds : Which kinds exist, and how many of each.
	Kinds(ctx context.Context, userID string) ([]event.Kind, error)
}

// Clock : What this needs to know about time.
type Clock struct {
	// Now : The moment, in UTC. Nil uses the real clock.
	Now func() time.Time
	// Location : The person's zone, which is what a written hour means.
	// Nil is UTC.
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

// Tools : Everything in this package.
//
// A reader that also keeps the books (event.Ledgers) is given event_ledger as
// well.
func Tools(reader Reader, clock Clock) []tool.Tool {
	out := []tool.Tool{kinds(reader, clock), recent(reader, clock), untouched(reader, clock)}
	if books, ok := reader.(event.Ledgers); ok {
		out = append(out, ledger(books, clock))
	}
	return out
}

// kinds : What the person's devices report at all.
//
// Prefetched, and the reason is a failure this could not otherwise
// avoid. event_recent's description deliberately lists no kinds,
// because any device may invent one and a list goes stale the day it
// is written. But a model given no list invents its own: asked
// whether the phone was locked it answered that its tools covered
// "calls, networks joined, music playing, battery level" and that
// the lock state was not among them. Three of those four were
// guesses and the lock state had been arriving all day.
//
// So the list comes from the data instead, every turn, for this
// person. Nothing here knows what a battery or a screen is; it knows
// what has turned up. A kind wired on the phone this morning is in
// the prompt this afternoon with no change here.
//
// Cheap enough to do unasked: one grouped count, a line a kind, and
// the whole of it is shorter than a single day of events.
func kinds(reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:     "event_kinds",
		Domain:   "event",
		Lists:    true,
		Prefetch: true,
		Purpose: "What the person's own devices report about them, as a list of the kinds that " +
			"have actually arrived.",
		UseWhen: "To know what is on offer before answering anything about their phone, watch, " +
			"laptop or day. This is the whole of what is reported: a kind in this list can be " +
			"read with event_recent by name, and a thing that is not in it is genuinely not " +
			"reported. New kinds appear here as devices start sending them.",
		Avoid: "Not for the events themselves, which is event_recent. This says only what exists, " +
			"never what happened.",
		WhenUnasked: "This is what their devices report, not a list of things to raise. Use it to " +
			"decide what you can answer and what to ask event_recent for; say nothing about it " +
			"unless the question is about it.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if reader == nil {
				return tool.Failed("Nothing on this server is collecting what your devices see.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			found, err := reader.Kinds(ctx, in.Caller.UserID)
			if err != nil {
				return tool.Broke("What their devices report could not be listed.", err)
			}
			if len(found) == 0 {
				return tool.OK("Their devices have reported nothing at all yet.")
			}

			sort.Slice(found, func(i, j int) bool { return found[i].Kind < found[j].Kind })

			where := clock.where()
			var b strings.Builder
			fmt.Fprintf(&b, "Their devices report these %d kinds, and nothing else. "+
				"Read any of them with event_recent by name, or a whole family with its "+
				"stem such as call. -- the newest of a kind is how that thing stands now.\n",
				len(found))
			for _, k := range found {
				fmt.Fprintf(&b, "  %-22s %5d, last %s\n",
					k.Kind, k.Count, k.Last.In(where).Format("2 Jan 15:04"))
			}
			return tool.OK(strings.TrimRight(b.String(), "\n"))
		},
	}
}

// recent : What the person's devices have reported.
func recent(reader Reader, clock Clock) tool.Tool {
	return tool.Tool{
		Name:   "event_recent",
		Domain: "event",
		Lists:  true,
		Purpose: "Read what the person's own devices report about them: who called, where they " +
			"were, what they connected to, and how anything of theirs stands right now.",
		UseWhen: "A question about what the person has actually been doing rather than what they have " +
			"told you: who they spoke to or missed a call from, what their day looked like, what " +
			"else has been going on, when something last happened. Where they were and how long " +
			"for is place_visits, which reads the same days more plainly; come here for everything " +
			"else, and for the day as a whole even when part of the answer is places. " +
			"Also a question in the present tense about something of theirs -- how much is left, " +
			"what it is doing, what it is connected to, whether it is on. A device reports a " +
			"reading whenever it changes, so the newest event of that kind is how it stands now: " +
			"ask for that kind with a limit of 1. " +
			"Their phone, their watch and their laptop report here and new kinds " +
			"arrive without this description changing, so you cannot know from this text what is " +
			"in here. Never answer that you have no tool for something about their own phone, " +
			"watch, laptop or day: call this once without a kind, read the kinds it lists, and " +
			"ask again by name. Also when you are about to remark " +
			"on their day unprompted and need to know what it was. " +
			"And a question about a person -- whether you know them, who they are, when they " +
			"last came up -- is half answered here and nowhere else: somebody they rang, or " +
			"who rang them, is in here under their name even when nothing else has heard of " +
			"them. Put the name in contains, leave kind out, and set days wide: which way " +
			"round it went is exactly what you are looking for, and the last time somebody " +
			"came up is rarely today. Asked who somebody is, look here as well as " +
			"wherever else you look, and never answer that what you found elsewhere is all " +
			"there is without having looked here too. " +
			"Having looked, answer with what is there rather than with what is not: asked " +
			"when somebody called and finding that they were called instead, say that. " +
			"Reporting the absence of the narrower thing while holding the wider one is " +
			"true and useless.",
		Avoid: "Not for things they asked you to remember, which are memories, and not for what is " +
			"coming, which is the calendar and the reminders. Do not answer from the conversation: " +
			"what you said earlier about their day was true when you said it. And nothing here is " +
			"for something they are telling you rather than asking about: a remark about how they " +
			"feel, or how long something took, is to be answered, not looked up.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"kind": {
					Type: "string",
					Description: "Only this kind of event, as a dotted name such as wifi.joined. " +
						"Leave it out for everything. Call once without it first if you do not " +
						"know which kinds exist: the answer lists them. " +
						"Never guess one from the words of the question. A family holds the " +
						"same thing happening several ways and the question names only one of " +
						"them: asked when somebody called, the answer may be that they were " +
						"called instead, and a kind taken from the phrasing turns a thing " +
						"plainly there into nothing at all. Where the question is about a " +
						"person or a named thing, use contains on its own and read every kind " +
						"that comes back. A few kinds are too " +
						"many to hand over unasked -- a row a song, a position every five " +
						"minutes -- and are left out until you name one, so ask for the kind " +
						"by name when the question is about what is in it. A limit of 1 with " +
						"a kind gives how that thing stands now, because a device reports it " +
						"again whenever it changes. What is playing now is music.state that " +
						"way: the newest one says which track and whether it is playing, and a " +
						"newest one that is not playing means nothing is.",
				},
				"days": {
					Type: "integer",
					Description: "How many days back to look, ending today. 1 is today only, 7 is the " +
						"past week. Use a large number when asking when something last happened.",
					Default: DefaultDays,
				},
				"on": {
					Type: "string",
					Description: "One particular day, as YYYY-MM-DD, instead of a stretch ending today. " +
						"Use this for a question about a named day. Ignores days.",
				},
				"contains": {
					Type: "string",
					Description: "Only events mentioning this somewhere in their details: a network " +
						"name, a place, a person, a song, a band, an app -- anything with a name. " +
						"Use it when the question is about a particular thing rather than a " +
						"particular kind. This includes a name you already know about from the " +
						"world: asked whether you know a singer you can describe perfectly well " +
						"without looking, their own devices may still have been playing that " +
						"singer yesterday, and that is the half of the answer only this can give.",
				},
				"limit": {
					Type: "integer",
					Description: "At most this many, newest first. Use 1 when the question is only " +
						"when something last happened.",
					Default: DefaultLimit,
				},
			},
		},
		Examples: []tool.Example{
			{Ask: "what have I been up to today", Args: `{"saying":"looking at your day"}`},
			{Ask: "who did I talk to today", Args: `{"kind":"call.","saying":"checking your calls"}`},
			{Ask: "what am I listening to right now",
				Args: `{"kind":"music.state","limit":1,"saying":"checking what is playing"}`},
			{Ask: "what was I listening to this afternoon",
				Args: `{"kind":"music.state","saying":"looking at what you played"}`},
			{Ask: "when did I last leave the office", Args: `{"kind":"wifi.left","days":30}`},
			{Ask: "what is my phone on now", Args: `{"saying":"checking what your phone says"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			var args struct {
				Kind     string `json:"kind"`
				Days     int    `json:"days"`
				On       string `json:"on"`
				Contains string `json:"contains"`
				Limit    int    `json:"limit"`
			}
			_ = json.Unmarshal(in.Args, &args)

			if reader == nil {
				return tool.Failed("Nothing on this server is collecting what your devices see.")
			}
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}

			where := clock.where()
			q := event.Query{Contains: strings.TrimSpace(args.Contains)}

			// A kind with no dot in it is a family, and so is one
			// ending in a dot. Only a full dotted name is matched
			// exactly.
			//
			// "wifi." covers joining and leaving without the caller
			// having to know which kinds exist, which it cannot,
			// because any device may invent one. But the dot was
			// required, and a model asking for "call" -- which is how
			// a person says it -- was told nothing of kind call was
			// reported today, in a sentence that went on to list
			// call.received (13) among the kinds that exist.
			//
			// The owner read that answer: *"I got a call from Kliffy,
			// why is it not saying that."* He had had five calls that
			// morning and was told twice that he had had none.
			//
			// Matched as a prefix rather than by appending a dot, so a
			// sender that one day reports a bare "call" is found by
			// the same question.
			kind := strings.TrimSpace(args.Kind)
			if strings.HasSuffix(kind, ".") || !strings.Contains(kind, ".") {
				q.Prefix = kind
			} else {
				q.Kind = kind
			}

			// Never the working material, unless it is what was asked
			// for.
			//
			// A phone reports where it is every five minutes and what
			// it is playing once a track: hundreds a day, against a
			// couple of dozen of everything else. Left in, any window
			// long enough to be interesting is nothing but these, and
			// the kinds worth seeing fall off the end of the limit.
			// "Where was I on Tuesday" wants the three places, not the
			// three hundred readings.
			//
			// The position readings are omitted even when asked for by
			// name, because what they are for is working out stays and
			// nothing in a conversation wants a coordinate. Reading
			// them back is a question for the database.
			//
			// Music is not the same. "What was I listening to this
			// afternoon" is a real question and the tracks are the
			// only answer to it, so asking for the kind by name gets
			// them -- it is being handed them unasked that buries
			// everything else.
			q.Omit = event.Churn
			if event.Working(kind) && kind != event.Fixed && kind != event.Fixed+"." {
				q.Omit = []string{event.Fixed}
			}
			// Naming a thing is asking for it, the same as naming a
			// kind.
			//
			// The omission above is about being handed churn
			// unasked: a window long enough to be interesting is
			// otherwise nothing but position readings and tracks,
			// and the few kinds worth seeing fall off the end of
			// the limit. A text search has the opposite problem --
			// it is already narrowed to one named thing, and
			// almost everything that carries a name is in music.
			//
			// Measured 9 October 2026, from the owner's own
			// transcript. Asked whether it knew a singer, it
			// reached for the right tool with the right arguments
			// -- `contains: "Demi Lovato", days: 90` -- and was
			// told nothing mentioning her had been reported, in a
			// sentence that went on to list music.state (199)
			// among the kinds that do exist. He had played her the
			// previous afternoon and the row was in the table
			// while the sentence was being written.
			//
			// The same shape as the bare "call" that was answered
			// with "nothing of kind call today" above a list
			// containing call.received (13). A filter meant to
			// keep a browse readable, silently emptying a search.
			//
			// Nothing is held back from a search, including the
			// position readings.
			//
			// They were kept out at first, on the reasoning that
			// nothing in a conversation wants a coordinate and
			// nobody searches for one by name. Owner: *"a search
			// should fetch whatever is in the record, even the
			// tiniest one."*
			//
			// He is right, and the reasoning was the same mistake
			// in a smaller size. It is true that a fix holds only
			// a latitude and a longitude **today** -- and what is
			// in a payload is the sender's business, not this
			// file's. A phone that starts putting a street on its
			// fixes would be found by nobody, and nothing would
			// say so: the search would go on answering that there
			// was no mention, which is the failure this whole
			// change is about.
			//
			// The limit is the only bound a search needs. Somebody
			// who names a thing has already narrowed it.
			if q.Contains != "" {
				q.Omit = nil
			}

			q.Limit = args.Limit
			if q.Limit <= 0 {
				q.Limit = DefaultLimit
			}
			q.Limit = min(q.Limit, MaxLimit)

			days := args.Days
			if days <= 0 {
				days = DefaultDays
			}
			days = min(days, MaxDays)

			// From the start of a day in the person's own zone, not from
			// this moment minus a day. "Today" means since midnight;
			// measuring back from now would answer a question about this
			// morning with yesterday evening in it.
			local := clock.now().In(where)
			midnight := time.Date(local.Year(), local.Month(), local.Day(),
				0, 0, 0, 0, where)

			over := window(days)
			if on := strings.TrimSpace(args.On); on != "" {
				day, err := time.ParseInLocation("2006-01-02", on, where)
				if err != nil {
					return tool.Failed("A day has to be written as YYYY-MM-DD; " +
						on + " could not be read as one.")
				}
				q.Since, q.Until = day, day.AddDate(0, 0, 1)
				over = "on " + day.Format("Monday 2 January")
			} else {
				q.Since = midnight.AddDate(0, 0, -(days - 1))
			}

			found, err := reader.Recent(ctx, in.Caller.UserID, q)
			if err != nil {
				return tool.Broke("What their devices reported could not be read.", err)
			}

			if len(found) == 0 {
				return tool.OK(Nothing(ctx, reader, in.Caller.UserID, q, over) +
					Besides(ctx, reader, in.Caller.UserID, q, over))
			}
			return tool.OK(describe(found, q, over, where) +
				Besides(ctx, reader, in.Caller.UserID, q, over))
		},
	}
}

// Nothing : What to say when the window held none.
//
// Exported so a test can ask it directly: the widening below is the
// whole of the behaviour and it is not reachable any other way.
//
// Which kinds do exist, when the question found nothing, because the
// likeliest reason is a kind that was guessed at rather than a quiet day.
// Besides : The rest of a family, when only one of it was asked for.
//
// Asked *"who called me recently"*, the model called this with
// kind call.received, read yesterday's, and answered "you
// received no calls today, sir". There had been a missed call at
// 9:49 that morning. Told about it directly it found it at once,
// with kind call.missed.
//
// The description already warns against this in nearly those
// words -- *a family holds the same thing happening several ways
// and the question names only one of them* -- and the warning did
// not hold. So the server says it instead of asking: a reading of
// one kind comes back with what else of its family is in the same
// window, and the answer cannot be "no calls" while "call.missed
// 1" is sitting under it.
//
// Only for a single kind with a family. A prefix already read the
// whole of it, and a kind with no dot has no siblings.
func Besides(ctx context.Context, reader Reader, userID string, q event.Query, over string) string {
	stem, _, found := strings.Cut(q.Kind, ".")
	if !found || stem == "" {
		return ""
	}

	wider := q
	wider.Kind, wider.Prefix, wider.Limit = "", stem+".", MaxLimit
	also, err := reader.Recent(ctx, userID, wider)
	if err != nil || len(also) == 0 {
		return ""
	}

	seen := map[string]int{}
	order := make([]string, 0, 4)
	for _, e := range also {
		if e.Kind == q.Kind {
			continue
		}
		if _, had := seen[e.Kind]; !had {
			order = append(order, e.Kind)
		}
		seen[e.Kind]++
	}
	if len(order) == 0 {
		return ""
	}
	sort.Strings(order)

	counted := make([]string, 0, len(order))
	for _, kind := range order {
		counted = append(counted, fmt.Sprintf("%s %d", kind, seen[kind]))
	}
	return fmt.Sprintf("\n\nThe same family also reported this %s, and you did not ask for it: %s. "+
		"They are the same thing happening other ways, so answer about the family rather than "+
		"the one kind you chose, and read them by name before saying any of it did not happen.",
		over, strings.Join(counted, ", "))
}

func Nothing(ctx context.Context, reader Reader, userID string, q event.Query, over string) string {
	asked := q.Kind + q.Prefix
	if asked == "" && q.Contains == "" {
		return "Their devices reported nothing " + over + "."
	}

	var what []string
	if asked != "" {
		what = append(what, "of kind "+asked)
	}
	if q.Contains != "" {
		what = append(what, "mentioning "+q.Contains)
	}
	none := "Nothing " + strings.Join(what, " ") + " was reported " + over + "."

	// Asked about a named thing and narrowed to one kind, look again
	// for the name on its own before saying there is nothing.
	//
	// The catalogue below is no use for this. Asked whether Gunalan
	// had called, the model narrowed to the kind that means an
	// incoming one, and the answer said nothing of that kind
	// mentions Gunalan and then listed every kind there is with its
	// total -- call.made (13), which is thirteen calls to anybody.
	// So it reported that he had not called, three hours after a
	// call to him. The name was in the window the whole time, one
	// kind over.
	//
	// Telling it not to guess a kind was written twice and ignored
	// twice: the question says called me and the kind that means
	// that is right there. This does not ask it to resist that. It
	// widens the search once, on its behalf, and only when the name
	// found nothing where it was asked for.
	if q.Contains != "" && asked != "" {
		wider := q
		wider.Kind, wider.Prefix = "", ""
		if also, err := reader.Recent(ctx, userID, wider); err == nil && len(also) > 0 {
			seen := map[string]int{}
			order := []string{}
			for _, e := range also {
				if _, had := seen[e.Kind]; !had {
					order = append(order, e.Kind)
				}
				seen[e.Kind]++
			}
			named := make([]string, 0, len(order))
			for _, k := range order {
				named = append(named, fmt.Sprintf("%s (%d)", k, seen[k]))
			}
			return prompt.Text(
				none,
				q.Contains+" is in the window all the same, under other kinds: "+
					strings.Join(named, ", ")+".",
				"Ask again without a kind and say what is actually there. Answering that the "+
					"narrow thing did not happen, while this is sitting beside it, is true and useless.",
			)
		}
	}

	kinds, err := reader.Kinds(ctx, userID)
	if err != nil || len(kinds) == 0 {
		return none
	}
	names := make([]string, 0, len(kinds))
	for _, k := range kinds {
		// The kinds this tool will not return are not kinds it should
		// offer. Naming one here is an invitation to ask again for
		// something that comes back empty every time.
		if k.Kind == event.Fixed {
			continue
		}
		names = append(names, fmt.Sprintf("%s (%d)", k.Kind, k.Count))
	}
	if len(names) == 0 {
		return none
	}
	return prompt.Text(
		none,
		"The kinds that do exist, with how many of each altogether, are: "+
			strings.Join(names, ", ")+".",
	)
}

// window : How to say the period a question covered.
func window(days int) string {
	switch {
	case days <= 1:
		return "today"
	case days == 7:
		return "in the past week"
	default:
		return fmt.Sprintf("in the past %d days", days)
	}
}

// run : Several of the same thing, close together.
type run struct {
	kind  string
	value string
	first time.Time
	last  time.Time
	count int
}

// describe : The shape of what happened, collapsed.
func describe(found []event.Event, q event.Query, over string, where *time.Location) string {
	// Oldest first: a day is read forwards. The store answers newest
	// first because a listing wants that, and a narrative does not.
	sort.Slice(found, func(i, j int) bool {
		return found[i].OccurredAt.Before(found[j].OccurredAt)
	})

	var runs []run
	for _, e := range found {
		at := e.OccurredAt.In(where)
		v := value(e.Payload)
		if n := len(runs); n > 0 {
			last := &runs[n-1]
			if last.kind == e.Kind && last.value == v &&
				at.Sub(last.last) <= Together &&
				sameDay(last.last, at) {
				last.last = at
				last.count++
				continue
			}
		}
		runs = append(runs, run{kind: e.Kind, value: v, first: at, last: at, count: 1})
	}

	var b strings.Builder
	var narrowed []string
	if asked := q.Kind + q.Prefix; asked != "" {
		narrowed = append(narrowed, "of kind "+asked)
	}
	if q.Contains != "" {
		narrowed = append(narrowed, "mentioning "+q.Contains)
	}
	header := fmt.Sprintf("%d events %s", len(found), over)
	if len(narrowed) > 0 {
		header = fmt.Sprintf("%d events %s %s",
			len(found), strings.Join(narrowed, " "), over)
	}
	if len(runs) < len(found) {
		header += fmt.Sprintf(", as %d entries once repeats are grouped", len(runs))
	}
	b.WriteString(header + ". Times are the person's own.\n")

	day := ""
	shown := 0
	for _, r := range runs {
		if shown >= MaxLines {
			b.WriteString(fmt.Sprintf("... and %d earlier entries not shown.\n", len(runs)-shown))
			break
		}
		if d := r.first.Format("Monday 2 January"); d != day {
			day = d
			b.WriteString("\n" + d + "\n")
		}
		b.WriteString("  " + line(r) + "\n")
		shown++
	}
	return strings.TrimRight(b.String(), "\n")
}

// line : One entry.
func line(r run) string {
	at := r.first.Format("15:04")
	what := r.kind
	if r.value != "" {
		what += " " + r.value
	}
	if r.count == 1 {
		return at + "  " + what
	}
	return fmt.Sprintf("%s  %s  (%d times, last at %s)",
		at, what, r.count, r.last.Format("15:04"))
}

// value : The one thing a payload is mostly about.
//
// Whatever is under "value", which is what a phone writing these sends.
// Anything else is rendered as it stands: a payload nobody planned for is
// still worth showing, and guessing at which of its fields matters would
// drop the one that did.
func value(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var into map[string]any
	if err := json.Unmarshal(payload, &into); err != nil || len(into) == 0 {
		return ""
	}
	if v, ok := into["value"]; ok && len(into) == 1 {
		return fmt.Sprint(v)
	}

	keys := make([]string, 0, len(into))
	for k := range into {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == Seconds {
			if n, ok := into[k].(float64); ok {
				parts = append(parts, "lasted="+span(n))
				continue
			}
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, into[k]))
	}
	return strings.Join(parts, " ")
}

// Seconds : The field a sender uses for how long something ran.
//
// A unit rather than a kind of event, which is why naming it here
// does not break the rule that the event describes itself: seconds
// mean the same whoever sent them, and a sender that writes this
// field has said it is reporting a span.
const Seconds = "seconds"

// span : A number of seconds as somebody would say it.
//
// Owner: *"don't need to say exact 19 secs, 23 secs -- those are
// unwanted."* A model handed 19 says nineteen, and four calls
// recited to the second is a reading of the call log rather than an
// answer about the morning. Anything under a minute is brief and
// nothing more; past that the minutes are the fact and the seconds
// are not.
func span(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d < time.Minute:
		return "brief"
	case d < time.Hour:
		return plural(int(d.Round(time.Minute).Minutes()), "minute")
	case d.Round(time.Minute)%time.Hour == 0:
		return plural(int(d.Round(time.Minute).Hours()), "hour")
	default:
		d = d.Round(time.Minute)
		return plural(int(d.Hours()), "hour") + " " +
			plural(int(d.Minutes())%60, "minute")
	}
}

// plural : A number and its noun, singular when it is one.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// sameDay : Whether two local moments are the same date.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
