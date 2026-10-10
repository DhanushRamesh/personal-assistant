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
func Tools(reader Reader, ask Asker, now func() time.Time) []tool.Tool {
	return []tool.Tool{people(reader, now), place(reader, ask, now)}
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
func place(reader Reader, ask Asker, now func() time.Time) tool.Tool {
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
				"confirmed": {
					Type: "boolean",
					Description: "Only true once they have been asked, in so many words, " +
						"whether to ring this person and have said yes. Leave it out " +
						"otherwise: the call then comes back with the name and the " +
						"question to put, and nothing is dialled.",
					Default: false,
				},
			},
			Required: []string{"who"},
		},
		Examples: []tool.Example{
			{Ask: "call Amma", Args: `{"who":"Amma","saying":"one moment"}`},
			{Ask: "yes, ring her", Args: `{"who":"Amma","confirmed":true,"saying":"calling her now"}`},
		},
		Run: func(ctx context.Context, in tool.Invocation) tool.Result {
			if in.Caller.UserID == "" {
				return tool.Failed("This request did not come from a known person.")
			}
			var args struct {
				Who       string `json:"who"`
				Confirmed bool   `json:"confirmed"`
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

			found, err := Known(ctx, reader, in.Caller.UserID, when(now))
			if err != nil {
				return tool.Broke("Who there is to call could not be read.", err)
			}
			who, how := Match(said, found)

			switch how {
			case Near:
				// Heard rather than read, and near is not the same
				// as right. Put before anything is dialled, which
				// is the general rule for a change made on a name
				// that was only close.
				return tool.Confirm("person", said, who, "ringing them")
			case Stranger:
				// Not refused. The phone's contacts are larger than
				// the list of people called from it, and somebody
				// rung for the first time is exactly the case where
				// refusing would be wrong.
				if !args.Confirmed {
					return tool.Failed(strangerTo(said, found))
				}
			default:
				if !args.Confirmed {
					return tool.Failed(fmt.Sprintf(
						"Nothing has been dialled yet. A call is always put to them first: "+
							"ask whether to ring %s, as a question they can answer yes or "+
							"no, and call this again with confirmed true if they say yes.",
						who))
				}
			}

			if err := ask.Ring(ctx, in.Caller.UserID, calling.Call{Who: who}); err != nil {
				return tool.Broke("That call could not be asked for.", err)
			}
			return tool.OK(fmt.Sprintf("The phone has been asked to ring %s. Tell them so. "+
				"It dials on the phone itself, so the call is there and not here.", who))
		},
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
func strangerTo(said string, known []Person) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nothing has been dialled. Nobody the phone has had a call with is called "+
		"%q, though that was heard rather than read and may have come out wrong. ", said)
	if len(known) > 0 {
		b.WriteString("These are the people there have been calls with: " +
			strings.Join(names(known), ", ") + ". ")
		b.WriteString("If one of them is who was meant, ask whether that is the one, as a " +
			"yes-or-no question, and call this again with their name and confirmed true. ")
	}
	fmt.Fprintf(&b, "If %q really is who they want, that name may still be in the phone's own "+
		"contacts: ask whether to ring them, and call this again with confirmed true if they "+
		"say yes. Do not tell them there is no such person.", said)
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
