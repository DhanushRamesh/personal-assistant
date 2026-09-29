package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

func lister() Tool {
	return Tool{Name: "thing_list", Domain: "thing", Lists: true,
		Purpose: "x", UseWhen: "always", Channels: []chat.Channel{chat.ChannelDirect},
		Run: func(context.Context, Invocation) Result { return OK("here they are") }}
}

func remover() Tool {
	return Tool{Name: "thing_delete", Domain: "thing", Writes: true,
		Purpose: "x", UseWhen: "always", Channels: []chat.Channel{chat.ChannelDirect},
		Run: func(context.Context, Invocation) Result { return OK("gone") }}
}

// TestAWriteNeedsAReadFirst : A write is refused until its own domain has
// been read in the same turn, and the refusal says how to put it right.
func TestAWriteNeedsAReadFirst(t *testing.T) {
	r, err := NewRegistry(lister(), remover())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	who := Caller{UserID: "u1", Channel: chat.ChannelDirect}

	cold := r.Call(context.Background(), "thing_delete", Invocation{Caller: who, Args: []byte(`{}`)})
	if cold.Outcome != conversation.OutcomeFailed {
		t.Fatalf("a write ran with nothing read first: %s", cold.Content)
	}
	for _, want := range []string{"Nothing has been changed", "thing_list", "Read first"} {
		if !strings.Contains(cold.Content, want) {
			t.Errorf("refusal does not mention %q: %s", want, cold.Content)
		}
	}

	warm := r.Call(context.Background(), "thing_delete",
		Invocation{Caller: who, Args: []byte(`{}`), Ran: []string{"thing_list"}})
	if warm.Outcome != conversation.OutcomeOK {
		t.Errorf("a write was refused after its domain was read: %s", warm.Content)
	}

	// A read is never blocked.
	if got := r.Call(context.Background(), "thing_list", Invocation{Caller: who, Args: []byte(`{}`)}); got.Outcome != conversation.OutcomeOK {
		t.Errorf("a read was refused: %s", got.Content)
	}
}
