package platformai

import (
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// TestOnlyJudgementReasons : Owner: *"use a high reasoning model
// for this"*, of the greeting. It already ran on the same model as
// everything else; what it did not have was permission to think.
//
// Given per purpose rather than globally, because the two are
// opposite requirements. A greeting reads a diary against a day of
// positions and decides what one sentence should be about; "what
// is the time" has one answer, and reasoning first spends seconds
// of a thirty-second turn to arrive at it.
func TestOnlyJudgementReasons(t *testing.T) {
	for _, c := range []struct {
		purpose environment.Purpose
		want    string
	}{
		{environment.PurposeProfile, "low"},
		// Not the greeting, though it wants it most: 29 to 46
		// seconds against a five-second budget means a better
		// greeting written every time and heard none of the time.
		{environment.PurposeGreeting, ""},
		{environment.PurposeRemark, ""},
		{environment.PurposeChat, ""},
		{environment.PurposeTitle, ""},
	} {
		if got := thinking("", c.purpose); got != c.want {
			t.Errorf("%q thinks %q, want %q", c.purpose, got, c.want)
		}
	}
}

// TestItNeverReasonsLessThanAsked : Somebody who set it globally
// meant it, and a purpose asking for medium must not quieten a
// server told to think hard about everything.
func TestItNeverReasonsLessThanAsked(t *testing.T) {
	if got := thinking("high", environment.PurposeRemark); got != "high" {
		t.Errorf("a global high was reduced to %q", got)
	}
	if got := thinking("none", environment.PurposeProfile); got != "low" {
		t.Errorf("a purpose asking for more got %q", got)
	}
	if got := thinking("medium", environment.PurposeChat); got != "medium" {
		t.Errorf("an ordinary turn lost its configured %q", got)
	}
}

// TestACallAndItsAnswerAreTwoMessages : A turn carrying both a tool
// call and its result used to return only the result, so the call was
// dropped without a word and the gateway refused the request -- a
// tool_result with no tool_use in front of it.
func TestACallAndItsAnswerAreTwoMessages(t *testing.T) {
	got := asWire(environment.Turn{
		Role:        environment.RoleAssistant,
		Text:        "Looking.",
		ToolCalls:   []environment.ToolCall{{ID: "c1", Name: "time_now", Arguments: "{}"}},
		ToolResults: []environment.ToolResult{{ID: "c1", Content: "5:11 pm"}},
	})

	if len(got) != 2 {
		t.Fatalf("a turn with a call and its answer became %d messages, want 2", len(got))
	}
	if got[0].Role != "assistant" || len(got[0].ToolCalls) != 1 {
		t.Errorf("the call is not first: %+v", got[0])
	}
	if got[0].ToolCalls[0].ID != "c1" {
		t.Errorf("the call lost its identifier: %+v", got[0].ToolCalls[0])
	}
	if got[1].Role != "tool" || len(got[1].ToolResults) != 1 {
		t.Errorf("the answer is not second: %+v", got[1])
	}
	if got[1].ToolResults[0].ID != "c1" {
		t.Errorf("the answer lost the call it belongs to: %+v", got[1].ToolResults[0])
	}
}

// TestATurnWithOneOrTheOtherIsUnchanged : The shapes the runner
// actually builds, which must come out exactly as they did before.
func TestATurnWithOneOrTheOtherIsUnchanged(t *testing.T) {
	for _, c := range []struct {
		what string
		turn environment.Turn
		want string
	}{
		{"spoken", environment.Turn{Role: environment.RoleUser, Text: "hello"}, "user"},
		{
			"asked for a tool",
			environment.Turn{
				Role:      environment.RoleAssistant,
				ToolCalls: []environment.ToolCall{{ID: "c1", Name: "time_now"}},
			},
			"assistant",
		},
		{
			"answered one",
			environment.Turn{
				Role:        environment.RoleTool,
				ToolResults: []environment.ToolResult{{ID: "c1", Content: "5:11 pm"}},
			},
			"tool",
		},
	} {
		got := asWire(c.turn)
		if len(got) != 1 {
			t.Errorf("%s became %d messages, want 1", c.what, len(got))
			continue
		}
		if got[0].Role != c.want {
			t.Errorf("%s came out as %q, want %q", c.what, got[0].Role, c.want)
		}
	}
}
