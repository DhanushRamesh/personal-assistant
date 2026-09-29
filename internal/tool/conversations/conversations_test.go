package conversations_test

import (
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
	"github.com/DhanushRamesh/personal-assistant/internal/tool/conversations"
)

// Every conversation tool has to survive registration, which is where a
// missing description or an untyped argument is caught.
func TestEveryConversationToolRegisters(t *testing.T) {
	if _, err := tool.NewRegistry(conversations.All(nil)...); err != nil {
		t.Fatalf("a conversation tool is not usable: %v", err)
	}
}

// Deleting is the one thing that cannot be undone, and on voice a misheard
// sentence is the whole authorisation.
func TestVoiceCannotDelete(t *testing.T) {
	r, err := tool.NewRegistry(conversations.All(nil)...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, x := range r.For(chat.ChannelVoice) {
		if x.Name == "conversation_delete" {
			t.Error("voice is offered conversation_delete")
		}
	}

	var typedHasIt bool
	for _, x := range r.For(chat.ChannelDirect) {
		if x.Name == "conversation_delete" {
			typedHasIt = true
		}
	}
	if !typedHasIt {
		t.Error("typed cannot delete either, so nothing can")
	}
}

// Every tool shows the model at least one worked call, and the examples have
// to be valid against the schema the model is actually shown: an example
// that lies about its arguments teaches the model to get them wrong.
//
// Checked against the narrated schema rather than the tool's own, because
// that is the one the model reads. The two differ by the saying argument,
// which is added on the way out and taken off on the way back, and an
// example without it teaches the model to leave it out -- which is exactly
// what happened when these examples were written before it existed.
func TestEveryExampleMatchesItsSchema(t *testing.T) {
	for _, x := range conversations.All(nil) {
		if len(x.Examples) == 0 {
			t.Errorf("%s shows no example call", x.Name)
			continue
		}
		offered := tool.Narrated(x.Params, "sir")
		for _, e := range x.Examples {
			if err := tool.Validate(offered, []byte(e.Args)); err != nil {
				t.Errorf("%s has an example the model would be refused for copying: %s — %v",
					x.Name, e.Args, err)
			}
			if !strings.Contains(e.Args, `"saying"`) {
				t.Errorf("%s has an example with no saying, which teaches the model to omit it: %s",
					x.Name, e.Args)
			}
		}
	}
}
