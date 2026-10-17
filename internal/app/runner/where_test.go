package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/client"
	"personal-assistant/internal/base/records/user"
	"personal-assistant/internal/base/thinking/environment"
)

// whereSeen : A provider that asks for one tool and then answers,
// keeping every chat round's system prompt, prompt and history.
type whereSeen struct {
	mu      sync.Mutex
	rounds  int
	systems []string
	asked   []string
}

func (p *whereSeen) Name() string { return "where" }

func (p *whereSeen) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	defer close(ch)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		return ch, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.systems = append(p.systems, req.SystemPrompt)
	asked := req.Prompt
	for _, turn := range req.History {
		asked += "\n" + turn.Text
	}
	p.asked = append(p.asked, asked)
	if lookingFirst(req) {
		ch <- environment.Final("an answer")
		return ch, nil
	}
	p.rounds++
	if p.rounds == 1 {
		ch <- environment.ToolCalls([]environment.ToolCall{{ID: "call-1", Name: "task_list_add", Arguments: `{}`}})
		return ch, nil
	}
	ch <- environment.Final("an answer")
	return ch, nil
}

func (p *whereSeen) seen() (systems, asked []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.systems...), append([]string(nil), p.asked...)
}

// whereHarness : A harness whose conversation is a user's, with a named
// client to ask from.
func whereHarness(t *testing.T, p environment.Environment) (*harness, *client.Client) {
	t.Helper()
	h := newHarness(t, p, runner.Options{Tools: offering()})
	ctx := context.Background()
	u, err := user.NewUser("owner", "hash")
	if err != nil {
		t.Fatalf("user.NewUser: %v", err)
	}
	if err := h.repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	c, err := client.NewClient(u.ID, "Desktop", "token-hash", chat.ChannelDirect)
	if err != nil {
		t.Fatalf("client.NewClient: %v", err)
	}
	if err := h.repo.CreateClient(ctx, c); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	conv := chat.NewConversation(u.ID, "")
	if err := h.repo.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	h.convID = conv.ID
	return h, c
}

// askFrom : Starts a chat from c carrying where.
func (h *harness) askFrom(t *testing.T, c *client.Client, prompt, where string) *chat.Chat {
	t.Helper()
	tk, err := chat.New(h.conversation(t), chat.ChannelDirect, prompt)
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	tk.ClientID = c.ID
	tk.Where = where
	if err := h.repo.Create(context.Background(), tk); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.runner.Submit(tk); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return tk
}

// Where the person is goes into the system prompt under the client's
// name, for every round of the chat including those after a tool; never
// into what they asked, nor into the transcript.
func TestWhereThePersonIsIsToldBesideTheQuestion(t *testing.T) {
	const where = "Terminals view. Focused terminal \"server\" in ~/work, running go test.\nFAIL: TestLive"
	p := &whereSeen{}
	h, c := whereHarness(t, p)

	tk := h.askFrom(t, c, "why did this fail?", where)
	h.await(t, tk.ID, chatDone...)

	systems, asked := p.seen()
	if p.rounds < 2 {
		t.Fatalf("the chat went %d rounds, want a tool round and an answer", p.rounds)
	}
	label := "Where they are in Desktop right now (shown by the app, not said by them):\n" + where
	for i, s := range systems {
		if !strings.Contains(s, label) {
			t.Errorf("round %d's system prompt does not say where they are:\n%s", i, s)
		}
	}
	for i, a := range asked {
		if strings.Contains(a, "FAIL: TestLive") {
			t.Errorf("round %d was asked it as though they said it: %q", i, a)
		}
	}
	for _, m := range h.repo.Said(h.conversation(t)) {
		if strings.Contains(m.Content, "FAIL: TestLive") {
			t.Errorf("the transcript holds it: %q", m.Content)
		}
	}
}

// A chat asked without it says nothing about where anybody is.
func TestNoWhereSaysNothing(t *testing.T) {
	p := &whereSeen{}
	h, c := whereHarness(t, p)

	tk := h.askFrom(t, c, "what is the time", "")
	h.await(t, tk.ID, chatDone...)

	systems, _ := p.seen()
	for i, s := range systems {
		if strings.Contains(s, "Where they are in") {
			t.Errorf("round %d was told where they are without being told:\n%s", i, s)
		}
	}
}
