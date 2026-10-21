package rules

import (
	"strings"
	"testing"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/domain/job"
)

// looking : a lookup with one read and one write in it.
func looking(_, name string) (tool.Tool, bool) {
	switch name {
	case "read_terminal":
		return tool.Tool{Name: name}, true
	case "type_into_terminal":
		return tool.Tool{Name: name, Writes: true}, true
	}
	return tool.Tool{}, false
}

// A rule may change something, but only behind an approval.
//
// Until 21 October it could not change anything at all: it runs
// unattended every few minutes, nobody is there the first time it goes
// wrong, and reading is safe to repeat where writing is not. What
// changed is that a rule can now stop and say what it is about to do,
// and go no further until he answers.
func TestAWritingToolIsAllowedOnlyBehindAnApproval(t *testing.T) {
	// Without an ask, refused, and the refusal says what to do.
	why := allowed("usr_1", []job.Act{
		{Do: job.DoTool, Use: "read_terminal", With: []byte(`{"terminal":"Jarvis Server"}`)},
		{Do: job.DoTool, Use: "type_into_terminal", With: []byte(`{"terminal":"Laptop client","text":"carry on"}`)},
		{Do: job.DoSay, Text: "Told it, sir."},
	}, looking)
	if why == "" {
		t.Fatal("a rule was allowed to change something with nobody watching")
	}
	if !strings.Contains(why, "ask") {
		t.Errorf("the refusal does not say what to do about it: %q", why)
	}

	// Behind one, allowed.
	if why := allowed("usr_1", []job.Act{
		{Do: job.DoTool, Use: "read_terminal", With: []byte(`{"terminal":"Jarvis Server"}`)},
		{Do: job.DoAsk, Text: "Shall I tell the Laptop client to carry on, sir?"},
		{Do: job.DoTool, Use: "type_into_terminal", With: []byte(`{"terminal":"Laptop client","text":"carry on"}`)},
		{Do: job.DoSay, Text: "Told it, sir."},
	}, looking); why != "" {
		t.Errorf("a write behind an approval was refused: %q", why)
	}

	// An ask after the write is a rule that acts and then enquires.
	if why := allowed("usr_1", []job.Act{
		{Do: job.DoTool, Use: "type_into_terminal", With: []byte(`{"terminal":"Laptop client","text":"carry on"}`)},
		{Do: job.DoAsk, Text: "Shall I?"},
		{Do: job.DoSay, Text: "Told it, sir."},
	}, looking); why == "" {
		t.Error("a write before its approval was allowed")
	}

	// And an approval that says nothing is no approval.
	if why := allowed("usr_1", []job.Act{
		{Do: job.DoAsk, Text: "  "},
		{Do: job.DoTool, Use: "type_into_terminal", With: []byte(`{"terminal":"x","text":"y"}`)},
		{Do: job.DoSay, Text: "Done."},
	}, looking); why == "" {
		t.Error("an approval with no words was allowed")
	}
}
