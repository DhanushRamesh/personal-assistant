package platformai_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/thinking/environment/platformai"
)

// keeping : A recorder that holds what it was given.
type keeping struct {
	mu    sync.Mutex
	calls []environment.Call
	got   chan struct{}
}

func newKeeping() *keeping { return &keeping{got: make(chan struct{}, 16)} }

func (k *keeping) Record(_ context.Context, c environment.Call) {
	k.mu.Lock()
	k.calls = append(k.calls, c)
	k.mu.Unlock()
	k.got <- struct{}{}
}

func (k *keeping) kept() []environment.Call {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]environment.Call(nil), k.calls...)
}

// wait : Blocks until n calls have been recorded.
func (k *keeping) wait(t *testing.T, n int) []environment.Call {
	t.Helper()
	for i := len(k.kept()); i < n; i++ {
		select {
		case <-k.got:
		case <-time.After(5 * time.Second):
			t.Fatalf("recorded %d calls, want %d", len(k.kept()), n)
		}
	}
	return k.kept()
}

// labelled : A context saying a chat round asked.
func labelled() context.Context {
	return environment.Labelled(context.Background(), environment.Label{
		UserID: "usr_A", ChatID: "chat_1", ConversationID: "conv_1",
		Feature: environment.FeatureChat,
	})
}

// recording : A provider against f that records into k.
func recording(t *testing.T, cfg platformai.Config, k *keeping) *platformai.Environment {
	t.Helper()
	cfg.Recorder = k
	return newProvider(t, cfg)
}

// An answer is kept with what it cost, who asked it, what it was sent
// to and how long it took. This is the whole point of the row.
func TestAnAnswerIsKeptWithItsCost(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		time.Sleep(5 * time.Millisecond)
		w.Write([]byte(`{"data":{"messages":[{"content":"Four."}],
			"usage":{"ai_stats":{"input_tokens":1500,"output_tokens":12,"total_tokens":1512}}}}`))
	}
	k := newKeeping()
	p := recording(t, cfg, k)

	ch, err := p.Run(labelled(), environment.Request{Prompt: "how many",
		Vendor: "openai", Model: "gpt-4.1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	collect(t, ch)

	got := k.wait(t, 1)
	if len(got) != 1 {
		t.Fatalf("kept %d calls, want 1", len(got))
	}
	c := got[0]
	if c.Ended != environment.EndedAnswered || c.Input != 1500 || c.Output != 12 {
		t.Errorf("kept %+v, want answered with 1500 in and 12 out", c)
	}
	if c.Vendor != "openai" || c.Model != "gpt-4.1" {
		t.Errorf("kept %s/%s, want what was sent", c.Vendor, c.Model)
	}
	if c.UserID != "usr_A" || c.ChatID != "chat_1" || c.ConversationID != "conv_1" ||
		c.Feature != environment.FeatureChat {
		t.Errorf("kept label %+v, want the chat's", c.Label)
	}
	if c.Took < 5*time.Millisecond || c.At.IsZero() {
		t.Errorf("took %v at %v, want the time it ran", c.Took, c.At)
	}
}

// A round that asked for tools is kept as tools, and says so in its
// detail when its caller did not give one.
func TestARoundAskingForToolsIsKeptAsTools(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.Write([]byte(`{"data":{"messages":[{"content":"","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"event_recent","arguments":"{}"}}
		]}],"usage":{"ai_stats":{"input_tokens":900,"output_tokens":20}}}}`))
	}
	k := newKeeping()
	ch, _ := recording(t, cfg, k).Run(labelled(), environment.Request{Prompt: "what happened"})
	collect(t, ch)

	c := k.wait(t, 1)[0]
	if c.Ended != environment.EndedTools || c.Detail != environment.DetailTools || c.Input != 900 {
		t.Errorf("kept %+v, want tools", c)
	}
}

// A failed call keeps what the gateway said it cost. The tokens of a
// reply that came to nothing were still spent.
func TestAFailedCallKeepsItsTokens(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		w.Write([]byte(`{"data":{"messages":[],"usage":{"ai_stats":{"input_tokens":700,"output_tokens":0}}}}`))
	}
	k := newKeeping()
	ch, _ := recording(t, cfg, k).Run(labelled(), environment.Request{Prompt: "anything"})
	msgs := collect(t, ch)
	if msgs[len(msgs)-1].Kind != environment.KindError {
		t.Fatalf("ended %q, want a failure", msgs[len(msgs)-1].Kind)
	}

	c := k.wait(t, 1)[0]
	if c.Ended != environment.EndedFailed || c.Input != 700 {
		t.Errorf("kept %+v, want failed with 700 in", c)
	}
}

// A refused token and the retry are two calls, both sent, both kept --
// the first as failed, the second as whatever it came to.
func TestARetryIsKeptTwice(t *testing.T) {
	f, cfg := newFakeService(t)
	var calls atomic.Int32
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"INVALID_OAUTHTOKEN"}}`))
			return
		}
		w.Write([]byte(`{"data":{"messages":[{"content":"the answer"}],
			"usage":{"ai_stats":{"input_tokens":40,"output_tokens":3}}}}`))
	}
	k := newKeeping()
	ch, _ := recording(t, cfg, k).Run(labelled(), environment.Request{Prompt: "ask me"})
	collect(t, ch)

	got := k.wait(t, 2)
	if len(got) != 2 {
		t.Fatalf("kept %d calls, want 2", len(got))
	}
	if got[0].Ended != environment.EndedFailed || got[1].Ended != environment.EndedAnswered ||
		got[1].Input != 40 {
		t.Errorf("kept %+v then %+v, want failed then answered", got[0], got[1])
	}
	if got[0].Feature != environment.FeatureChat || got[1].Feature != environment.FeatureChat {
		t.Error("the retry lost the label")
	}
}

// A call nobody labelled is still kept, as other. A gap in the record
// would be worse than a row that says it does not know who asked.
func TestAnUnlabelledCallIsKeptAsOther(t *testing.T) {
	_, cfg := newFakeService(t)
	k := newKeeping()
	ch, _ := recording(t, cfg, k).Run(context.Background(), environment.Request{Prompt: "hello"})
	collect(t, ch)

	c := k.wait(t, 1)[0]
	if c.Feature != environment.FeatureOther || c.UserID != "" || c.Ended != environment.EndedAnswered {
		t.Errorf("kept %+v, want other", c)
	}
}

// A round started early and dropped unread was not stopped by
// anybody: it was not used, and is kept as unused.
func TestARoundStartedEarlyAndDroppedIsUnused(t *testing.T) {
	_, cfg := newFakeService(t)
	k := newKeeping()
	ctx, cancel := context.WithCancel(environment.Labelled(labelled(), environment.Label{
		Feature: environment.FeatureChat, Detail: environment.DetailEarly, Early: true,
	}))
	if _, err := recording(t, cfg, k).Run(ctx, environment.Request{Prompt: "look first"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Never read, as the runner leaves one it may not want; then dropped.
	time.Sleep(50 * time.Millisecond)
	cancel()

	c := k.wait(t, 1)[0]
	if c.Ended != environment.EndedUnused || c.Detail != environment.DetailEarly {
		t.Errorf("kept %+v, want unused and started early", c)
	}
}

// A call the person stopped is kept as stopped.
func TestAStoppedCallIsKeptAsStopped(t *testing.T) {
	f, cfg := newFakeService(t)
	f.chatHandler = func(w http.ResponseWriter, _ []byte) {
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte(`{"data":{"messages":[{"content":"late"}]}}`))
	}
	k := newKeeping()
	ctx, cancel := context.WithCancel(labelled())
	ch, _ := recording(t, cfg, k).Run(ctx, environment.Request{Prompt: "never mind"})
	time.Sleep(30 * time.Millisecond)
	cancel()
	collect(t, ch)

	c := k.wait(t, 1)[0]
	if c.Ended != environment.EndedStopped {
		t.Errorf("kept %+v, want stopped", c)
	}
}
