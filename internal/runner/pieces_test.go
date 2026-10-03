package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
)

// watched : A scripted provider that records whether each round was
// allowed to put the answer through in pieces.
type watched struct {
	rounds []environment.Message
	mu     sync.Mutex
	at     int
	pieces []bool
}

func (p *watched) Name() string { return "watched" }

func (p *watched) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}

	p.mu.Lock()
	at := p.at
	if at >= len(p.rounds) {
		at = len(p.rounds) - 1
	}
	p.at++
	p.pieces = append(p.pieces, req.Pieces)
	p.mu.Unlock()

	ch <- p.rounds[at]
	close(ch)
	return ch, nil
}

func (p *watched) allowed() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.pieces...)
}

// TestAnAnswerThatCanBeSentBackIsNotStreamed : The bug this exists for.
//
// Streaming went in, and the assistant started saying everything twice.
// A model that answers without touching a tool is sent back once to
// look at them -- but by then the answer it gave had already been
// spoken, and the round that replaced it was heard as a repeat.
//
// So the first round, which can still be sent back, is not streamed,
// and the one after it is.
func TestAnAnswerThatCanBeSentBackIsNotStreamed(t *testing.T) {
	p := &watched{rounds: []environment.Message{
		environment.Final("There is no such thing, sir."),
		environment.Final("There is no such thing, sir."),
	}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	got := p.allowed()
	if len(got) < 2 {
		t.Fatalf("the answer was not sent back: %d rounds", len(got))
	}
	if got[0] {
		t.Error("an answer that could still be sent back was spoken as it was written")
	}
	if !got[1] {
		t.Error("the answer that stands was not streamed")
	}
}

// TestTheAnswerAfterToolsIsStreamed : The ordinary voice turn, and the
// one the streaming was built for. Once a tool has been asked for the
// answer cannot be sent back to look at them, so it goes out in pieces.
func TestTheAnswerAfterToolsIsStreamed(t *testing.T) {
	p := &watched{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	got := p.allowed()
	if len(got) < 2 {
		t.Fatalf("the tool round did not happen: %d rounds", len(got))
	}
	if !got[1] {
		t.Error("the answer after a tool ran was held back instead of streamed")
	}
	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a write that worked was denied: %q", answer)
	}
}
