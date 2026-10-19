package platformai

import (
	"errors"
	"testing"
)

// A reply cut off at the budget is not a finished reply. Until 19
// October 2026 the finish reason was read only when nothing came back,
// so a reply that hit the three thousand tokens with text in it, or a
// tool call whose arguments stopped mid-JSON, passed as normal.
func TestACutOffReplySaysSo(t *testing.T) {
	for _, finish := range []string{"maxtokens", "max_tokens", "MaxTokens", " length "} {
		if !Cut(finish) {
			t.Errorf("%q was not taken as cut off", finish)
		}
	}
	for _, finish := range []string{"stop", "tool_calls", "end_turn", ""} {
		if Cut(finish) {
			t.Errorf("%q was taken as cut off", finish)
		}
	}
	if !errors.Is(ErrCut, ErrCut) {
		t.Error("ErrCut is not itself")
	}
}
