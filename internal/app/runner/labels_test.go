package runner_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// labelling : An environment that notes the label each request came with.
type labelling struct {
	environment.Stub
	mu   sync.Mutex
	seen map[environment.Purpose]environment.Label
}

func (l *labelling) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	got, _ := environment.LabelOf(ctx)
	l.mu.Lock()
	if l.seen == nil {
		l.seen = map[environment.Purpose]environment.Label{}
	}
	l.seen[req.Purpose] = got
	l.mu.Unlock()
	return l.Stub.Run(ctx, req)
}

func (l *labelling) label(p environment.Purpose) (environment.Label, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	got, ok := l.seen[p]
	return got, ok
}

// Every model call the runner makes says what it is and whose chat it
// is for, which is what the record of what each call cost is filed
// under. The chat's own conversation names the person: the chat does
// not carry one.
func TestEveryCallSaysWhatAndForWhom(t *testing.T) {
	env := &labelling{}
	h := newHarness(t, env, runner.Options{})
	c := chat.NewConversation("usr_01LABELLED00000000000000", "")
	if err := h.repo.CreateConversation(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	h.convID = c.ID

	tk := h.submit(t, "what is on today")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	answer, ok := env.label(environment.PurposeChat)
	if !ok {
		t.Fatal("the chat was never asked")
	}
	want := environment.Label{UserID: "usr_01LABELLED00000000000000", Feature: environment.FeatureChat,
		ChatID: tk.ID, ConversationID: c.ID}
	if answer != want {
		t.Errorf("chat round labelled %+v, want %+v", answer, want)
	}

	// Named after the answer is delivered, so waited for.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if title, ok := env.label(environment.PurposeTitle); ok {
			if title.Feature != environment.FeatureTitle || title.UserID != want.UserID ||
				title.ConversationID != c.ID {
				t.Errorf("naming labelled %+v", title)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the conversation was never named")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
