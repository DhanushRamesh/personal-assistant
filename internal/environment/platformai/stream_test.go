package platformai

import (
	"strings"
	"testing"
)

// live : the events the endpoint actually sent on 3 October 2026, for
// "What did I listen to today? Use the tool." with one tool offered.
const live = `event: generate
data: {"messages":[{"role":"assistant","content":""}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"name":"event_recent","arguments":""},"id":"toolu_01Ch8FtoGHp8kszNuDJQ4XSf","type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":""},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":"{\"days\""},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":": 1"},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":", \"kind"},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":"\": \"mus"},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"tool_calls":[{"function":{"arguments":"ic\"}"},"type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"finish_reason":"tool_calls"}],"object":"stream.chunk"}

event: generate
data: {"data":{"stream":true,"is_final_chunk":true,"usage":{"ai_stats":{"input_tokens":1827}}}}
`

// A tool call arrives as a name once and the arguments in fragments.
// Joined in order they are the JSON the model meant; joined in any
// other way they are not JSON at all.
func TestAToolCallIsPutBackTogether(t *testing.T) {
	got, err := readStream(strings.NewReader(live), nil)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(got.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(got.calls))
	}
	call := got.calls[0]
	if call.Name != "event_recent" {
		t.Errorf("name = %q", call.Name)
	}
	if call.ID != "toolu_01Ch8FtoGHp8kszNuDJQ4XSf" {
		t.Errorf("id = %q", call.ID)
	}
	if call.Arguments != `{"days": 1, "kind": "music"}` {
		t.Errorf("arguments = %q", call.Arguments)
	}
}

const spoken = `event: generate
data: {"messages":[{"role":"assistant","content":""}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"content":"Ready"}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"content":"."}],"object":"stream.chunk"}

event: generate
data: {"data":{"stream":true,"is_final_chunk":true,"usage":{"ai_stats":{"input_tokens":11}}}}
`

// The text arrives in pieces and is handed over as it arrives, which
// is the whole reason for streaming: the first clause can be spoken
// while the rest is still being written.
func TestTextIsPassedOnAsItArrives(t *testing.T) {
	var pieces []string
	got, err := readStream(strings.NewReader(spoken), func(s string) {
		pieces = append(pieces, s)
	})
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if got.text.String() != "Ready." {
		t.Errorf("text = %q, want %q", got.text.String(), "Ready.")
	}
	if len(pieces) != 2 || pieces[0] != "Ready" || pieces[1] != "." {
		t.Errorf("pieces = %q, want the two fragments in order", pieces)
	}
}

// Nothing in a stream may be lost to one unreadable event, and an
// event with no data at all is the blank line between two others.
func TestABadEventDoesNotLoseTheReply(t *testing.T) {
	broken := "event: generate\ndata: {not json\n\n" + spoken
	got, err := readStream(strings.NewReader(broken), nil)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if got.text.String() != "Ready." {
		t.Errorf("text = %q, want the reply despite the bad event", got.text.String())
	}
}

// An empty stream is told apart from a stream of empty text, because
// one is a failure to answer and the other is an answer.
func TestAnEmptyStreamIsNoticed(t *testing.T) {
	got, err := readStream(strings.NewReader(""), nil)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if got.seen {
		t.Error("an empty stream looked like it carried something")
	}
}

// A sentence that comes with a tool call is progress, not the answer,
// and must not be put through as the answer arriving.
func TestTextBesideAToolCallIsNotStreamed(t *testing.T) {
	const withSaying = `event: generate
data: {"messages":[{"tool_calls":[{"function":{"name":"event_recent","arguments":"{}"},"id":"t1","type":"function"}]}],"object":"stream.chunk"}

event: generate
data: {"messages":[{"content":"checking your calls"}],"object":"stream.chunk"}

event: generate
data: {"data":{"stream":true,"is_final_chunk":true}}
`
	var pieces []string
	got, err := readStream(strings.NewReader(withSaying), func(s string) {
		pieces = append(pieces, s)
	})
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(pieces) != 0 {
		t.Errorf("streamed a sentence belonging to a tool call: %q", pieces)
	}
	// Still collected, because the caller sends it on as progress.
	if got.text.String() != "checking your calls" {
		t.Errorf("text = %q, want it kept for the caller", got.text.String())
	}
	if len(got.calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(got.calls))
	}
}
