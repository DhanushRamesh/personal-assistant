package asking_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"personal-assistant/internal/app/asking"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/chat/memory"
	"personal-assistant/internal/base/world/events"
)

type runner struct{}

func (runner) Submit(*chat.Chat) error { return nil }
func (runner) Cancel(string) bool      { return false }

type bus struct{}

func (bus) Subscribe(string) (<-chan events.Event, func()) {
	return make(chan events.Event), func() {}
}

// Where the person is is stored on the chat, trimmed and cut, so a chat
// recovered after a restart is answered as it was asked.
func TestWhereIsKeptOnTheChat(t *testing.T) {
	ctx := context.Background()
	repo := memory.New()
	conv := chat.NewConversation("usr_1", "")
	if err := repo.CreateConversation(ctx, conv); err != nil {
		t.Fatal(err)
	}
	s := asking.New(slog.New(slog.NewTextHandler(io.Discard, nil)), repo, runner{}, bus{})

	for _, c := range []struct{ sent, want string }{
		{"  Terminals view: FAIL TestX\n", "Terminals view: FAIL TestX"},
		{strings.Repeat("x", chat.MaxWhereBytes+1), strings.Repeat("x", chat.MaxWhereBytes)},
		{"", ""},
	} {
		started, err := s.Ask(ctx, asking.Request{
			UserID: "usr_1", Channel: chat.ChannelDirect, ConversationID: conv.ID,
			Prompt: "why did this fail?", Where: c.sent,
		})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		started.Stop()
		got, err := repo.Get(ctx, started.Chat.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Where != c.want {
			t.Errorf("where = %d bytes %.40q, want %d bytes %.40q", len(got.Where), got.Where, len(c.want), c.want)
		}
	}
}
