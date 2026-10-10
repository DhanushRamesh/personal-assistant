// Package calling lets the assistant ring somebody from the person's
// own phone.
//
// Owner, 10 October 2026: *"we can set an alarm right -- likewise
// there should be an option to call a person. For that it should have
// the contacts list on my phone."* And, asked whether every call
// should be put to them first: *"confirm always."*
//
// Two tools rather than one, which is the shape every writing domain
// here has. call_people says who the server knows can be rung;
// call_place rings one of them. The pairing is not decoration: a
// write is refused until its own domain has been read in the same
// turn, so the names in front of the model when it chooses one are
// real names and not remembered ones.
//
// Where the names come from is the whole of the design. The server
// has no address book -- the phone has one, and resolves the name it
// is sent against it. What the server has is every call the phone has
// ever reported, each naming who it was with, which is the list of
// people actually rung. That list is short enough to read out and
// short enough to match a mis-heard name against, and it costs
// nothing to keep in step.
package calling

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/world/calling"
	"personal-assistant/internal/base/world/event"
)

// Prefix : The kinds a call is reported under -- call.made,
// call.received, call.missed, call.unanswered.
//
// A prefix rather than a list, because the phone may invent another
// one tomorrow and a call by any name still names somebody.
const Prefix = "call."

// Behind : How far back to look for who is worth ringing.
//
// Generous. Somebody rung once in March is still somebody worth
// offering, and the list is short at any length: ten names over the
// eight days this has been recording.
const Behind = 365 * 24 * time.Hour

// Most : How many names to read out.
//
// Enough to be the whole list in practice, capped so that a year of
// cold calls cannot turn a listing into a recital.
const Most = 25

// Reader : Where the events are.
type Reader interface {
	// Recent : What happened, newest first by when it happened.
	Recent(ctx context.Context, userID string, q event.Query) ([]event.Event, error)
}

// Asker : What places a call, behind the answer.
//
// The errand queue rather than the phone, for the reason the whole
// errand package exists: asking and getting there are different
// jobs, and only the asking happens inside somebody's turn.
type Asker interface {
	// Ring : Writes down that this person is to be called.
	Ring(ctx context.Context, userID string, c calling.Call) error
}

// Tools : Everything in this package.
func Tools(reader Reader, ask Asker, heard Heard, change Changer,
	now func() time.Time) []tool.Tool {
	asking := NewAsking(now)
	out := []tool.Tool{people(reader, now), place(reader, ask, heard, change, asking, now)}
	// Nothing rather than tools that quietly do nothing. The phone
	// listens for a mid-call change on its own path, and with no
	// address for it a model told "on speaker" would say so and
	// nothing would happen -- which is worse than not being able to.
	if change == nil {
		return out
	}
	return append(out, during(change)...)
}

// Person : Somebody the phone has had a call with.
type Person struct {
	// Name : What the phone called them.
	Name string
	// Calls : How many calls, either way.
	Calls int
	// Last : The most recent of them.
	Last time.Time
}

// people : Who there is to ring.
func people(reader Reader, now func() time.Time) tool.Tool {
	return tool.Tool{
		Name:    "call_people",
		Domain:  "calling",
		Lists:   true,
		Purpose: "List the people the phone has had calls with, who can therefore be rung by name.",
		UseWhen: "Before placing a call, to find the name as the phone's contacts have it. " +
			"Also when they ask who they have been speaking to lately.",
		Avoid: "Not the phone's whole address book -- only people there has been a call with. " +
			"Somebody missing from this list may still be a contact on the phone and may " +
			"still be rung; the list is for matching what was heard against real names, " +
			"not a gate. For what was said in a call, or when one happened, use event_recent.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Examples: []tool.Example{
			{Ask: "who can you call", Args: `{"saying":"looking at who you have called"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			found, err := Known(ctx, reader, in.Caller.UserID, when(now))
			if err != nil {
				return tool.Broke("Who there is to call could not be read.", err)
			}
			if len(found) == 0 {
				return tool.OK("The phone has not reported a call with anybody, so there is " +
					"no name here to ring. A name can still be given and the phone will look " +
					"it up in its own contacts.")
			}
			return tool.OK(Said(found))
		},
	}
}

// place : Rings one of them.
func place(reader Reader, ask Asker, heard Heard, change Changer, asking *Asking,
	now func() time.Time) tool.Tool {
	return tool.Tool{
		Name:    "call_place",
		Domain:  "calling",
		Writes:  true,
		Purpose: "Ring somebody from the person's own phone, by name.",
		UseWhen: "They ask to call, ring or phone somebody. Give the name; the phone looks it " +
			"up in its own contacts and dials.",
		Avoid: "Never a telephone number -- only a name, and it is refused otherwise. Not for " +
			"a call to make later, which is a reminder. This dials now.",
		// Reachable by voice, which is the point of it, and the
		// reason the confirmation is not optional. A spoken name is
		// the whole authorisation and a mis-heard one dials a
		// stranger, so the first call never places anything: it
		// comes back with the name it would ring and the question to
		// put.
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params: tool.Schema{
			Properties: map[string]tool.Property{
				"who": {
					Type: "string",
					Description: "Who to ring, by name, as the phone's contacts have it. " +
						"Take it from call_people where one of those names is what was " +
						"meant. Never a telephone number.",
				},
				"speaker": {
					Type: "boolean",
					Description: "True when they asked for the call to be on " +
						"loudspeaker as well -- \"call Amma and put it on " +
						"speaker\". It goes on a few seconds after the dialling " +
						"starts. Leave it out otherwise.",
					Default: false,
				},
				"mute": {
					Type: "boolean",
					Description: "True when they asked to start the call muted, so the " +
						"other side cannot hear them until they say otherwise. " +
						"Rare. Leave it out otherwise.",
					Default: false,
				},
				"confirm": {
					Type: "string",
					Description: "Leave this out on the first call. It comes back with the " +
						"question to put to them, and nothing is dialled. Put that " +
						"question to them, wait for their answer, and only if they say " +
						"yes call this again with the same name and the word given " +
						"back. It cannot be guessed or made up, and it is refused if " +
						"they have not answered yet.",
				},
			},
			Required: []string{"who"},
		},
		Examples: []tool.Example{
			{Ask: "call Amma", Args: `{"who":"Amma","saying":"one moment"}`},
			{Ask: "call Amma and put it on speaker",
				Args: `{"who":"Amma","speaker":true,"saying":"one moment"}`},
			{Ask: "yes, ring her", Args: `{"who":"Amma","confirm":"3f9a1c20b4d8","saying":"calling her now"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			var args struct {
				Who     string `json:"who"`
				Confirm string `json:"confirm"`
				Speaker bool   `json:"speaker"`
				Mute    bool   `json:"mute"`
			}
			if err := json.Unmarshal(in.Args, &args); err != nil {
				return tool.Failed("Those arguments could not be read.")
			}
			said := calling.Tidy(args.Who)
			if said == "" {
				return tool.Failed("Nobody was named. Ask who to ring.")
			}
			if calling.Digits(said) {
				return tool.Failed(fmt.Sprintf(
					"%q is a telephone number, and only a name can be rung. The phone holds "+
						"the contacts and looks the name up itself. Ask whose number that "+
						"is, and ring them by name.", said))
			}

			// Answering one already put to them.
			if strings.TrimSpace(args.Confirm) != "" {
				return redeem(ctx, ask, change, heard, asking, in.Caller.UserID,
					strings.TrimSpace(args.Confirm), said, args.Speaker, args.Mute)
			}

			found, err := Known(ctx, reader, in.Caller.UserID, when(now))
			if err != nil {
				return tool.Broke("Who there is to call could not be read.", err)
			}
			who, how := Match(said, found)

			// Every road from here leads to the same place: a
			// question, and a word that answers it. Nothing dials.
			p := asking.Put(in.Caller.UserID, who, said, args.Speaker, args.Mute)
			switch how {
			case Near:
				// Heard rather than read, and near is not the same
				// as right, so what needs agreeing is which person.
				return tool.Failed(fmt.Sprintf(
					"Nothing has been dialled. They said %q and the nearest person the "+
						"phone has had a call with is %q, which is not the same words. "+
						"Ask whether that is who they mean, as a question they can "+
						"answer yes or no. If they say yes, call this again with who "+
						"%q and confirm %q.", said, who, who, p.Token))
			case Stranger:
				// Not refused. The phone's contacts are larger than
				// the list of people called from it, and somebody
				// rung for the first time is exactly the case where
				// refusing would be wrong.
				return tool.Failed(strangerTo(said, found, p.Token))
			default:
				return tool.Failed(fmt.Sprintf(
					"Nothing has been dialled yet. A call is always put to them first: "+
						"ask whether to ring %s, as a question they can answer yes or "+
						"no. If they say yes, call this again with who %q and confirm "+
						"%q.", who, who, p.Token))
			}
		},
	}
}

// redeem : A call that has been answered, or the reason it has not.
//
// The token proves the question was asked. It cannot prove it was
// heard, and that is the half that was missing: a model can ask for
// one and hand it straight back in the same breath, which is the
// assistant agreeing with itself. So the person has to have said
// something since.
func redeem(ctx context.Context, ask Asker, change Changer, heard Heard, asking *Asking,
	userID, given, said string, speaker, mute bool) tool.Result {
	p, ok := asking.Take(userID)
	if !ok {
		return tool.Failed("Nothing has been dialled. No call has been put to them, so " +
			"there is nothing for them to have agreed to. Ask first: call this again " +
			"with just the name, and it will give you the question to put.")
	}
	if p.Token == "" || given != p.Token {
		return tool.Failed("Nothing has been dialled. That is not the word this call came " +
			"back with, and it cannot be guessed at. Call this again with just the name " +
			"to be given the question to put to them.")
	}
	if asking.Stale(p) {
		return tool.Failed(fmt.Sprintf(
			"Nothing has been dialled. Ringing %s was put to them too long ago for an "+
				"answer now to be about that. Ask again: call this with just the name.",
			p.Who))
	}
	if !Answered(ctx, heard, userID, p) {
		// Kept rather than spent. They are about to answer, and
		// making the model ask again would have them hear the same
		// question twice for no reason.
		asking.Keep(userID, p)
		return tool.Failed(fmt.Sprintf(
			"Nothing has been dialled. They have not said anything since you were given "+
				"this question, so nobody has agreed to it yet: asking and answering "+
				"in the same breath is you agreeing with yourself. Put the question to "+
				"them, let them answer, and call this again with confirm %q once they "+
				"have. If they say no, let it go.", p.Token))
	}
	// Rung by what was asked about rather than by what this call
	// carries, so a question answered about one person can never
	// dial another.
	// Either the name it asked about or the words they first used.
	// What is rung is p.Who regardless; this only catches a model
	// that has moved on to somebody else.
	asked := calling.Tidy(said)
	if said != "" && !strings.EqualFold(asked, p.Who) && !strings.EqualFold(asked, p.Said) {
		return tool.Failed(fmt.Sprintf(
			"Nothing has been dialled. What was put to them was ringing %s, and this "+
				"names %s instead. Ask about %s on its own.", p.Who, said, said))
	}

	if err := ask.Ring(ctx, userID, calling.Call{Who: p.Who}); err != nil {
		return tool.Broke("That call could not be asked for.", err)
	}

	// Asked for on either call, since "and put it on speaker" may
	// come with the name or with the yes.
	return tool.OK(ringing(ctx, change, userID, p.Who,
		speaker || p.Speaker, mute || p.Mute))
}

// ringing : What to say about a call that is now being placed, and
// whatever was asked for alongside it.
//
// The speaker and the microphone are asked for a few seconds behind
// the call rather than with it. Nothing here learns when the call
// connects -- the phone answers the request, not the call -- so the
// wait is a guess, and it is made on the server where it can be
// changed rather than on the phone where it cannot.
func ringing(ctx context.Context, change Changer, userID, who string,
	speaker, mute bool) string {
	said := fmt.Sprintf("The phone has been asked to ring %s. Tell them so. "+
		"It dials on the phone itself, so the call is there and not here.", who)
	if !speaker && !mute {
		return said
	}
	if change == nil {
		// Said as a limit rather than as a gap in the equipment,
		// and the call still goes: half of what they asked for is
		// better than none of it.
		return said + " They also wanted it on speaker or muted, and that cannot be " +
			"done from here -- tell them they will have to reach over, in your own " +
			"words, without naming any of this."
	}

	var trouble bool
	if speaker {
		if err := change.Change(ctx, userID,
			calling.During{What: calling.Speaker, On: true}, calling.Connecting); err != nil {
			trouble = true
		}
	}
	if mute {
		if err := change.Change(ctx, userID,
			calling.During{What: calling.Mic, On: false}, calling.Connecting); err != nil {
			trouble = true
		}
	}
	if trouble {
		return said + " What they wanted alongside it -- the speaker or the microphone " +
			"-- could not be asked for. Say only that the call is going."
	}
	switch {
	case speaker && mute:
		return said + " It will go on speaker, muted, a moment after it starts."
	case speaker:
		return said + " It will go on speaker a moment after it starts."
	default:
		return said + " They will start the call muted."
	}
}

// How : How well a name matched somebody known.
type How int

const (
	// Exactly : The same name, give or take case.
	Exactly How = iota
	// Near : Close enough to be what was meant, and not the same
	// words.
	Near
	// Stranger : Nobody known is anything like it.
	Stranger
)

// Match : Who was meant, and how sure that is.
//
// Heard rather than read, always. "Kliffy" has arrived as Cliffy and
// as Clifford, and a name that comes out of speech-to-text wrong
// looks exactly like a name that is not in the list -- so nothing
// here ever concludes that somebody does not exist.
func Match(said string, known []Person) (string, How) {
	folded := fold(said)
	for _, p := range known {
		if fold(p.Name) == folded {
			return p.Name, Exactly
		}
	}
	// A first name on its own, which is how anybody is actually
	// asked for: "call Alekhya" against "Alekhya Chintada".
	for _, p := range known {
		for _, part := range strings.Fields(fold(p.Name)) {
			if part == folded {
				return p.Name, Exactly
			}
		}
	}
	best, score := "", 0
	for _, p := range known {
		if n := alike(folded, fold(p.Name)); n > score {
			best, score = p.Name, n
		}
	}
	if score >= len(folded)-1 && score >= 3 {
		return best, Near
	}
	return calling.Tidy(said), Stranger
}

// alike : How many letters of the shorter name the longer one has, in
// order.
//
// A longest common subsequence, which is what survives a syllable
// being heard wrong in the middle. Deliberately not a full edit
// distance: the failure to catch is a letter or two changed in a
// short name, and the cost of being generous is a question, not a
// wrong call.
func alike(a, b string) int {
	x, y := []rune(a), []rune(b)
	was := make([]int, len(y)+1)
	for i := 1; i <= len(x); i++ {
		now := make([]int, len(y)+1)
		for j := 1; j <= len(y); j++ {
			if x[i-1] == y[j-1] {
				now[j] = was[j-1] + 1
			} else if was[j] >= now[j-1] {
				now[j] = was[j]
			} else {
				now[j] = now[j-1]
			}
		}
		was = now
	}
	return was[len(y)]
}

// fold : A name as it is compared -- lowercase, spaces collapsed.
func fold(s string) string {
	return strings.ToLower(calling.Tidy(s))
}

// strangerTo : What to say when nobody known is anything like the
// name that was heard.
func strangerTo(said string, known []Person, token string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nothing has been dialled. Nobody the phone has had a call with is called "+
		"%q, though that was heard rather than read and may have come out wrong. ", said)
	if len(known) > 0 {
		b.WriteString("These are the people there have been calls with: " +
			strings.Join(names(known), ", ") + ". ")
		b.WriteString("If one of them is who was meant, ask whether that is the one, as a " +
			"yes-or-no question, then call this again with just their name. ")
	}
	fmt.Fprintf(&b, "If %q really is who they want, that name may still be in the phone's own "+
		"contacts: ask whether to ring them, and if they say yes call this again with that "+
		"name and confirm %q. Do not tell them there is no such person.", said, token)
	return b.String()
}

// names : Just the names.
func names(known []Person) []string {
	out := make([]string, 0, len(known))
	for _, p := range known {
		out = append(out, p.Name)
	}
	return out
}

// Known : Who the phone has had calls with, most called first.
//
// Both directions. Somebody who rings the person every day and is
// never rung back is still somebody they would ask to call, and the
// kind says which way it went -- which is of no interest here.
func Known(ctx context.Context, reader Reader, userID string, now time.Time) ([]Person, error) {
	if reader == nil {
		return nil, nil
	}
	got, err := reader.Recent(ctx, userID, event.Query{
		Since:  now.Add(-Behind),
		Prefix: Prefix,
	})
	if err != nil {
		return nil, err
	}

	by := map[string]*Person{}
	for _, e := range got {
		name := calling.Tidy(value(e))
		// A stranger's call names nobody, and a bare number is not
		// a name. Neither can be rung back by name, which is the
		// only thing this list is for.
		if name == "" || strings.EqualFold(name, event.Unknown) || calling.Digits(name) {
			continue
		}
		at := e.OccurredAt
		if p, ok := by[fold(name)]; ok {
			p.Calls++
			if at.After(p.Last) {
				p.Last = at
			}
			continue
		}
		by[fold(name)] = &Person{Name: name, Calls: 1, Last: at}
	}

	out := make([]Person, 0, len(by))
	for _, p := range by {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Calls != out[j].Calls {
			return out[i].Calls > out[j].Calls
		}
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > Most {
		out = out[:Most]
	}
	return out, nil
}

// value : What an event is labelled with.
func value(e event.Event) string {
	var payload struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return ""
	}
	return payload.Value
}

// Said : The people, as the model reads them.
func Said(known []Person) string {
	var b strings.Builder
	b.WriteString("People the phone has had calls with, most often first. Any of these can be " +
		"rung by name:\n")
	for _, p := range known {
		fmt.Fprintf(&b, "- %s (%s)\n", p.Name, times(p.Calls))
	}
	b.WriteString("\nSomebody not here may still be a contact on the phone and can still be " +
		"rung by name.")
	return b.String()
}

// times : How many calls, said rather than counted.
func times(n int) string {
	if n == 1 {
		return "one call"
	}
	return fmt.Sprintf("%d calls", n)
}

// when : Now, in UTC.
func when(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}
