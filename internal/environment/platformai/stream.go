package platformai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/DhanushRamesh/personal-assistant/internal/environment"
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
		Final bool `json:"is_final_chunk"`
		Usage struct {
			Stats wireUsage `json:"ai_stats"`
		} `json:"usage"`
	} `json:"data"`
}

// gathered : A streamed reply, put back together.
type gathered struct {
	text  strings.Builder
	calls []environment.ToolCall
	usage wireUsage
	// seen : Whether anything at all arrived, so an empty stream is
	// told apart from a stream of empty text.
	seen bool
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

		if c.Data != nil {
			out.usage = c.Data.Usage.Stats
			if c.Data.Final {
				break
			}
			continue
		}

		for _, m := range c.Messages {
			if text := contentText(m.Content); text != "" {
				out.text.WriteString(text)
				if onText != nil {
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
