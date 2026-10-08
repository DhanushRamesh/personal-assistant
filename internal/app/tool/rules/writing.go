package rules

import (
	"context"
	"encoding/json"
	"strings"

	"personal-assistant/internal/base/thinking/prompt"
	"personal-assistant/internal/base/world/event"
	"personal-assistant/internal/domain/job"
)

// A rule is written by something that does nothing else.
//
// Owner, 9 October 2026: *"for writing a rule we should have a
// separate agent with all the specification clearly written so that
// no mistake is made."*
//
// Two things were wrong with the tool writing its own arguments.
//
// The grammar had to be in the tool's description, and a
// description is paid for on every turn by every conversation,
// whether or not anybody is writing a rule. Sources, operators,
// values, hooks and the sequence came to some sixty lines sitting
// in front of a model being asked what the weather is.
//
// And it could not be measured. The eval that exists asks which
// tool the model reaches for, which is the thing that used to go
// wrong; what it says in its own comment is that it does not
// measure the rule that comes out. Arguments written in the middle
// of a turn, among fifty-one other tools, by a model holding a
// conversation, are not a thing that can be tested on its own.
//
// Written by something whose whole prompt is the specification,
// they are. It is given the grammar, what this person's devices
// actually report, what they usually do, and the rules they
// already have; it answers with JSON and nothing else; and what it
// answers is read, validated and -- when it is wrong -- handed
// back with the reason, in the same words a person would have been
// given.

// Ask : One question to a model, answered as text.
type Ask func(ctx context.Context, system, ask string) (string, error)

// Kinds : What a person's devices have actually reported.
type Kinds func(ctx context.Context, userID string) ([]event.Kind, error)

// Writer : What turns a request into a rule.
//
// Nil Ask leaves the tool refusing to write rules, which is what a
// server with no model behind it should do rather than guess.
type Writer struct {
	// Ask : The model that writes it.
	Ask Ask
	// Kinds : What their devices report, so the rule names a kind
	// that exists rather than one that sounds right.
	Kinds Kinds
	// Reading : The tools a rule may look things up with.
	Reading func() []string
	// Tries : How many goes it gets. Zero selects [Tries].
	Tries int
}

// Tries : How many goes the writer gets before it gives up.
//
// Three. The first is the answer; the second is the answer with the
// mistake pointed out, which is where nearly all of them are
// mended; the third is for a model having a bad minute. A fourth
// has never mended anything a third did not, and every one of them
// is a second the person is standing there waiting.
const Tries = 3

// instruction : What the writer is, said once.
const instruction = "You write standing rules for a personal assistant. " +
	"You answer with one JSON object and nothing else: no explanation, no markdown fence, no prose. " +
	"If what they asked for cannot be written as a rule, answer with {\"cannot\": \"<one sentence saying why>\"}."

// Written : A rule from what somebody asked for, or why not.
//
// The validation is the specification. Every refusal below is a
// sentence somebody could read, and it is handed straight back to
// the writer -- so a mistake is described in the same words whether
// it is being explained to a person or corrected by a model.
func (w Writer) Written(ctx context.Context, userID, asked, holding string, look Lookup) (job.Occasion, []job.Act, string) {
	if w.Ask == nil {
		return job.Occasion{}, nil, "There is no model on this server to write a rule with."
	}
	if strings.TrimSpace(asked) == "" {
		return job.Occasion{}, nil, "Say what the rule should do."
	}

	brief := prompt.Block(
		specification,
		w.reported(ctx, userID),
		w.available(),
		holding,
		prompt.Text("What they asked for:", strings.TrimSpace(asked)),
		"Answer with the JSON object for this rule.",
	)

	tries := w.Tries
	if tries <= 0 {
		tries = Tries
	}
	var wrong string
	for try := range tries {
		said := brief
		if wrong != "" {
			said = prompt.Block(brief,
				prompt.Text("Your last answer was refused:", wrong),
				"Answer again, with the whole object, mended.")
		}
		answer, err := w.Ask(ctx, instruction, said)
		if err != nil {
			return job.Occasion{}, nil, "That rule could not be worked out."
		}
		when, acts, why, settled := read(answer, look)
		switch {
		case why == "":
			return when, acts, ""
		case settled:
			// Not a mistake to mend. It has read the request and
			// says it is not a rule, and asking it twice more
			// spends two model calls and the seconds somebody is
			// standing there, on an answer that will not change.
			return job.Occasion{}, nil, why
		}
		wrong = why
		_ = try
	}
	// Its own words, because the person asked for something and the
	// reason it could not be done is the only useful thing to say.
	return job.Occasion{}, nil, wrong
}

// cannot : What the writer answers when the request is not a rule.
type cannot struct {
	Cannot string `json:"cannot"`
}

// read : One answer, as a rule, or why it is not one -- and
// whether that is the end of it.
//
// Settled means there is nothing to have another go at. A malformed
// object is a mistake and the writer is told what was wrong; a
// writer that has read the request and says it is not a rule has
// already done the thinking, and asking again only costs.
func read(answer string, look Lookup) (when job.Occasion, acts []job.Act, why string, settled bool) {
	body := fenced(answer)
	var refused cannot
	if err := json.Unmarshal([]byte(body), &refused); err == nil && refused.Cannot != "" {
		return job.Occasion{}, nil, strings.TrimSpace(refused.Cannot), true
	}

	var written struct {
		When    job.Occasion `json:"when"`
		Perform []job.Act    `json:"perform"`
	}
	if err := json.Unmarshal([]byte(body), &written); err != nil {
		return job.Occasion{}, nil, "That was not a JSON object: " + err.Error(), false
	}
	if bad := allowed(written.Perform, look); bad != "" {
		return job.Occasion{}, nil, bad, false
	}
	if err := written.When.Sound(); err != nil {
		return job.Occasion{}, nil, readable(err), false
	}
	return written.When, written.Perform, "", false
}

// fenced : The JSON out of an answer that was wrapped in a code
// fence anyway.
//
// Told not to and it mostly does not, but a rule refused because a
// model put three backticks round a perfectly good object is a
// refusal nobody can act on.
func fenced(answer string) string {
	body := strings.TrimSpace(answer)
	if !strings.HasPrefix(body, "```") {
		return body
	}
	if _, rest, found := strings.Cut(body, "\n"); found {
		body = rest
	}
	if cut := strings.LastIndex(body, "```"); cut >= 0 {
		body = body[:cut]
	}
	return strings.TrimSpace(body)
}

// reported : What this person's devices actually send.
//
// Given because a rule naming a kind nobody reports is a rule that
// never fires, and a model with no list in front of it will write
// the kind that sounds right. There is no list of kinds anywhere in
// the engine on purpose, so this is read from what has arrived.
func (w Writer) reported(ctx context.Context, userID string) string {
	if w.Kinds == nil {
		return ""
	}
	found, err := w.Kinds(ctx, userID)
	if err != nil || len(found) == 0 {
		return ""
	}
	names := make([]string, 0, len(found))
	for _, k := range found {
		names = append(names, k.Kind)
	}
	return prompt.Lines(
		"The kinds this person's devices actually report, and nothing else exists:",
		"  "+strings.Join(names, ", "))
}

// available : The tools a rule may look things up with.
func (w Writer) available() string {
	if w.Reading == nil {
		return ""
	}
	names := w.Reading()
	if len(names) == 0 {
		return ""
	}
	return prompt.Lines(
		"The tools a rule may use, and nothing else exists:",
		"  "+strings.Join(names, ", "))
}

// specification : The whole of how a rule is written, for the thing
// that writes them.
//
// As long as it needs to be, which is the point of it being here
// rather than in a description every turn pays for.
var specification = prompt.Block(
	prompt.Lines(
		"A rule is one object with two halves:",
		`  {"when": {...}, "perform": [...]}`,
		"when is when it looks and whether it holds. perform is what it does when it holds.",
		"There are no conditions in perform. Every condition lives in when.",
	),
	howToWake,
	howToWhen,
	howToPerform,
	prompt.Lines(
		"Two things to get right above all:",
		"A rule that nothing would ever wake is refused. Only a reading a device sends can wake one.",
		"A rule that never says anything is refused. It has to end in a write, a say or a relay.",
	),
)

// describing : A rule written the structured way, as JSON, for
// showing the writer what one looks like.
func describing(j job.Job) string {
	out, err := json.Marshal(map[string]any{
		"name": j.Name, "when": j.When, "perform": j.Perform,
	})
	if err != nil {
		return ""
	}
	return "  " + string(out)
}

// examples : The rules they already have, as the writer should
// write them.
//
// Owner, 7 October 2026: *"I will ask the AI to write the rules by
// itself with examples of the existing rules."* Their own rules are
// better than any invented example, because they are about their
// own devices, their own places and their own way of saying things.
func examples(held []job.Job) string {
	if len(held) == 0 {
		return ""
	}
	lines := make([]string, 0, len(held)+1)
	lines = append(lines, "The rules they already have, written the same way:")
	for _, j := range held {
		if one := describing(j); one != "" {
			lines = append(lines, one)
		}
	}
	if len(lines) == 1 {
		return ""
	}
	return prompt.Lines(lines...)
}
