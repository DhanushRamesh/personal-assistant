package tool

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Saying : The argument every tool takes for what it is about to do.
//
// An argument rather than prose alongside the call, because prose is
// optional and was not written: asked for one in the system prompt, the
// model still returned tool calls with nothing beside them. A schema
// property is part of the call itself, described where the model is already
// reading, and filled in the same breath as the other arguments.
const Saying = "saying"

// saidWhile : How the argument is described to the model, for a persona
// that addresses the person as the given word.
//
// The address is woven into the examples rather than demanded separately.
// An example carrying it is followed; a rule beside examples that lack it
// is not, which is how the argument itself came to be ignored at first.
func saidWhile(address string) Property {
	end, second := "", ""
	if address != "" {
		end = ", " + address
		second = ", " + address
	}
	return Property{
		Type: "string",
		Description: "One short sentence, present tense, saying what you are about to do with this call. " +
			"It is read aloud while the call runs, so the person is not waiting in silence. " +
			"Under ten words, no question, and name the thing rather than the tool: " +
			strconv.Quote("creating the event on the twenty-second of August"+end) + ", " +
			strconv.Quote("looking through your reminders"+second) + ". " +
			"It is not the answer and not a claim that anything worked.",
	}
}

// Narrated : The schema as the model is shown it, with the saying argument
// added.
//
// The tool's own schema is left alone, so nothing that runs a tool has to
// know this exists: it is added on the way out and taken off on the way
// back. A tool that already has an argument of this name keeps its own.
func Narrated(s Schema, address string) Schema {
	if _, taken := s.Properties[Saying]; taken {
		return s
	}

	properties := make(map[string]Property, len(s.Properties)+1)
	for name, p := range s.Properties {
		properties[name] = p
	}
	properties[Saying] = saidWhile(address)

	required := make([]string, 0, len(s.Required)+1)
	required = append(required, s.Required...)
	required = append(required, Saying)

	return Schema{Properties: properties, Required: required}
}

// TakeSaying : Reads the saying argument out of a call's arguments and
// returns the rest.
//
// Taken off before the tool is validated or run, since the tool's own schema
// does not have it and an unknown argument is refused. Arguments that will
// not parse are handed back untouched, so a malformed call still reaches the
// validator, which explains what is wrong far better than this could.
func TakeSaying(args json.RawMessage) (string, json.RawMessage) {
	if len(args) == 0 || string(args) == "null" {
		return "", args
	}

	var given map[string]json.RawMessage
	if err := json.Unmarshal(args, &given); err != nil {
		return "", args
	}
	raw, present := given[Saying]
	if !present {
		return "", args
	}
	delete(given, Saying)

	var said string
	// A non-string here is the model's mistake and not worth failing the
	// call over: the sentence is dropped and the work still happens.
	_ = json.Unmarshal(raw, &said)

	rest, err := json.Marshal(given)
	if err != nil {
		return said, args
	}
	return said, rest
}

// NarratedDescription : The tool's description with the saying argument
// explained beside it.
//
// The schema alone did not get it filled. Every tool's description ends in
// worked examples whose arguments do not have it, and an example of the
// right shape outweighs a property in a schema: offered it as required, the
// model returned calls without it. This says it again where the examples
// are read.
func NarratedDescription(description, address string) string {
	example := "creating the event on the twenty-second of August"
	if address != "" {
		example += ", " + address
	}
	return strings.TrimSpace(description) + " " + strings.Join([]string{
		"Every call also takes " + strconv.Quote(Saying) + ", which the examples above leave out but you must always include:",
		"one short present-tense sentence, under ten words, saying what this call is about to do.",
		"It is read aloud while the call runs so the person is not waiting in silence.",
		"For instance " + strconv.Quote(`"`+Saying+`": `+strconv.Quote(example)) + ".",
		"Name the thing, not the tool, and never claim it has worked.",
	}, " ")
}
