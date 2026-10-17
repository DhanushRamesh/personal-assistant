package memory_test

import (
	"context"
	"testing"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/chat/memory"
)

// Where the person was is kept with the chat, as the MySQL store keeps
// it, through the lifecycle writing over it.
func TestAChatRemembersWhereThePersonWas(t *testing.T) {
	r := memory.New()
	ctx := context.Background()

	asked, err := chat.New("", chat.ChannelDirect, "why did this fail?")
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	asked.Where = "Terminals view. Focused terminal \"server\": FAIL TestX"
	if err := r.Create(ctx, asked); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := r.Get(ctx, asked.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Where != asked.Where {
		t.Errorf("Where = %q, want %q", got.Where, asked.Where)
	}
	if err := got.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := r.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if again, _ := r.Get(ctx, asked.ID); again == nil || again.Where != asked.Where {
		t.Errorf("Where after an update = %+v, want it kept", again)
	}
}
