package platformai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"personal-assistant/internal/base/thinking/environment"
)

// Streaming : What the endpoint is asked for.
//
// It answers either way. Asked plainly it sends one JSON body when the
// whole reply is finished; asked to stream it sends server-sent events
// as the model produces them, which is the same reply arriving sooner.
//
// Nothing downstream was waiting for a whole answer, which is the
// surprise. Home Assistant's Ollama integration asks for a stream and
// reads deltas, its pipeline carries a tts_stream, and Piper
// advertises supports_synthesize_streaming. Every hop between the
// model and the speaker was ready; this was the one sending the reply
// in a single lump.
const Streaming = true

// chunk : One server-sent event from a streamed reply.
//
// The shapes it carries, measured against the live endpoint on 3
// October 2026:
//
//	{"messages":[{"role":"assistant","content":""}]}
//	{"messages":[{"content":"Ready"}]}
//	{"messages":[{"tool_calls":[{"function":{"name":"event_recent",
//	              "arguments":""},"id":"toolu_01…","type":"function"}]}]}
//	{"messages":[{"tool_calls":[{"function":{"arguments":"{\"days"}}]}]}
//	{"messages":[{"finish_reason":"tool_calls"}]}
//	{"data":{"stream":true,"is_final_chunk":true,"usage":{…}}}
//
// A tool call arrives in pieces: the name and the identifier once, in
// the first of them, and the arguments as fragments of a JSON string
// to be joined in the order they came.
type chunk struct {
	Messages []struct {
		Content      json.RawMessage `json:"content"`
		ToolCalls    []wireToolCall  `json:"tool_calls"`
		FinishReason string          `json:"finish_reason"`
	} `json:"messages"`
	Data *struct {
		Final bool     `json:"is_final_chunk"`
		Usage wireCost `json:"usage"`
	} `json:"data"`
}

// gathered : A streamed reply, put back together.
type gathered struct {
	text  strings.Builder
	calls []environment.ToolCall
	usage wireUsage
	// spent, available : The gateway's own credit figures for this
	// call and the balance after it.
	spent     float64
	available float64
	// seen : Whether anything at all arrived, so an empty stream is
	// told apart from a stream of empty text.
	seen bool
	// events : How many of them arrived.
	events int
	// finish : Why the model stopped, as the endpoint last said.
	//
	// Read only to explain a reply that came back with nothing in
	// it. A stream can run for a minute, end tidily and carry no
	// text at all, and without this there is nothing in the log
	// that says whether the model was cut off, refused, or simply
	// wrote nothing.
	finish string
}

// readStream : Reads server-sent events until the stream ends, and
// returns the reply they add up to.
//
// Every fragment is also handed to onText as it arrives, which is what
// the streaming is for. Nil means nobody is listening yet and the
// pieces are only being collected.
func readStream(body io.Reader, onText func(string)) (gathered, error) {
	var out gathered
	// Arguments are accumulated per call rather than per chunk: a
	// fragment carries no name and no identifier, only more of the
	// JSON, and which call it belongs to is "the one being built".
	var building []*environment.ToolCall
	var args []*strings.Builder

	lines := bufio.NewScanner(body)
	lines.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	for lines.Scan() {
		line := bytes.TrimSpace(lines.Bytes())
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			// "event: generate" and the blank line between events.
			continue
		}
		raw := bytes.TrimSpace(line[len("data:"):])
		if len(raw) == 0 || bytes.Equal(raw, []byte("[DONE]")) {
			continue
		}

		var c chunk
		if err := json.Unmarshal(raw, &c); err != nil {
			// One unreadable event is not worth losing the reply
			// over; the rest of the stream usually carries it.
			continue
		}
		out.seen = true
		out.events++

		if c.Data != nil {
			out.usage = c.Data.Usage.Stats
			out.spent, out.available = c.Data.Usage.Spent, c.Data.Usage.Available
			if c.Data.Final {
				break
			}
			continue
		}

		for _, m := range c.Messages {
			if m.FinishReason != "" {
				out.finish = m.FinishReason
			}
			if text := contentText(m.Content); text != "" {
				out.text.WriteString(text)
				// Only while this round is still an answer. A model
				// asking for a tool sometimes sends a sentence with
				// it, describing what it is about to do; that is
				// progress, spoken beside the turn, and putting it
				// through here would have it spoken as the answer and
				// then again as the aside.
				//
				// The shape makes this enough: in every streamed tool
				// call measured, the call arrives before any text of
				// its own.
				if onText != nil && len(building) == 0 {
					onText(text)
				}
			}
			for _, call := range m.ToolCalls {
				// A call with a name starts a new one. A call
				// without is more of the arguments of the last.
				if call.Function.Name != "" {
					made := fromWireCalls([]wireToolCall{call})
					if len(made) == 0 {
						continue
					}
					made[0].Arguments = ""
					building = append(building, &made[0])
					args = append(args, &strings.Builder{})
				}
				if len(args) == 0 {
					continue
				}
				args[len(args)-1].WriteString(call.Function.Arguments)
			}
		}
	}
	if err := lines.Err(); err != nil {
		return out, fmt.Errorf("platformai: reading the streamed reply: %w", err)
	}

	for i, call := range building {
		if joined := strings.TrimSpace(args[i].String()); joined != "" {
			call.Arguments = joined
		}
		out.calls = append(out.calls, *call)
	}
	return out, nil
}

// fromStream : A streamed reply, read as it arrives and returned in the
// same shape the plain endpoint gives.
//
// The caller cannot tell the difference afterwards, which is the point:
// only the timing changes, and everything that reads a reply carries on
// reading the same thing.
func (p *Environment) fromStream(_ context.Context, body io.Reader, status int,
	onText func(string),
) (reply, int, error) {
	got, err := readStream(body, onText)
	if err != nil {
		// What it had said the call cost by then, if anything: a
		// stream cut off near its end has usually been charged.
		return reply{Usage: got.usage, Spent: got.spent, Available: got.available}, status, err
	}
	if !got.seen {
		return reply{}, status, fmt.Errorf("platformai: the streamed reply was empty")
	}

	// Cut off at the budget rather than finished.
	//
	// Read whatever came back, which until 19 October 2026 was only
	// read when nothing came back at all: a reply that hit the three
	// thousand tokens with text in it, or a tool call whose arguments
	// stopped mid-JSON, was handed on as a normal answer. The model
	// cannot tell, and neither could anything after it.
	//
	// A cut-off tool call is the worse half. Arguments that stop
	// halfway do not parse, and an unparseable call reads from here
	// as a call that was never made.
	text := strings.TrimSpace(got.text.String())
	if Cut(got.finish) {
		return reply{Text: text, ToolCalls: got.calls, Usage: got.usage, Spent: got.spent, Available: got.available}, status, fmt.Errorf(
			"%w (%d events, %d words written)", ErrCut, got.events, got.usage.Output)
	}

	if len(got.calls) > 0 {
		return reply{Text: text, ToolCalls: got.calls, Usage: got.usage, Spent: got.spent, Available: got.available}, status, nil
	}
	if text == "" {
		// What the stream did carry, because the failure is
		// otherwise unexplainable: sixty seconds of events that
		// add up to no answer look exactly like one second of
		// them.
		return reply{Usage: got.usage, Spent: got.spent, Available: got.available}, status, fmt.Errorf(
			"platformai: the reply was empty (%d events, finished %q, %d words written)",
			got.events, got.finish, got.usage.Output)
	}
	return reply{Text: text, Usage: got.usage, Spent: got.spent, Available: got.available}, status, nil
}

// ErrCut : The reply stopped at the budget rather than at its end.
//
// Carried as an error because a cut-off answer is not an answer: half a
// sentence reads as a whole one, and a tool call whose arguments stop
// mid-JSON does not parse at all. What came back is returned beside it,
// so a caller that would rather have half than nothing still can.
var ErrCut = errors.New("platformai: the reply was cut off at the token budget")

// Cut : Whether a finish reason says the reply ran out of budget.
//
// Both spellings, because the gateway has used each: "maxtokens" is
// what it sends, and "max_tokens" is what the vendor's own API sends
// and what it may pass through unchanged.
func Cut(finish string) bool {
	switch strings.ToLower(strings.TrimSpace(finish)) {
	case "maxtokens", "max_tokens", "length":
		return true
	}
	return false
}
