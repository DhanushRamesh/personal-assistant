package environment_test

import (
	"testing"

	"personal-assistant/internal/base/thinking/environment"
)

// TestEveryPurposeSaysWhetherItDirects : A new purpose has to declare
// whether the model picks its own steps for it.
//
// Added and not declared, it quietly counts as a workflow -- which may
// be right, and which nobody decided. This makes it a decision.
func TestEveryPurposeSaysWhetherItDirects(t *testing.T) {
	var agents []environment.Purpose
	for _, p := range environment.Purposes() {
		if p.Directs() {
			agents = append(agents, p)
		}
	}

	// Only the chat. Everything the assistant does unasked -- naming a
	// conversation, condensing one, describing him, the greeting, the
	// nightly names, deciding whether something is worth remarking on
	// -- follows a path written in code, and that is what makes it
	// predictable.
	if len(agents) != 1 || agents[0] != environment.PurposeChat {
		t.Errorf("these purposes let the model choose its own steps: %v.\n"+
			"Adding one is a decision: it may call tools in a loop, cost several rounds, "+
			"and end somewhere nobody wrote down. Say here why this one needs that.", agents)
	}
}

// And the reverse, so the chat is never quietly turned into a fixed
// path: it is the one thing that has to be able to look twice.
func TestTheChatDirectsItself(t *testing.T) {
	if !environment.PurposeChat.Directs() {
		t.Error("the chat no longer picks its own steps, so nothing can call a tool after reading one")
	}
}
