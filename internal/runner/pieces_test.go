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

// narrating : A provider that says what it is about to do and then
// calls a tool, which is the shape of every turn that reads something.
type narrating struct {
	mu sync.Mutex
	at int
}

func (p *narrating) Name() string { return "narrating" }

func (p *narrating) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}
	p.mu.Lock()
	first := p.at == 0
	p.at++
	p.mu.Unlock()

	if first {
		ch <- environment.ToolCalls([]environment.ToolCall{{
			ID: "call-1", Name: "calendar_update",
			Arguments: `{"saying":"starting a new conversation"}`,
		}})
	} else {
		ch <- environment.Final("A new conversation has been created, sir.")
	}
	close(ch)
	return ch, nil
}

// TestAVoiceTurnIsNotToldWhatIsBeingDone : The owner, hearing
// "starting a new conversation" in front of "a new conversation has
// been created, sir": "no need for aside now, turn it off."
//
// Home Assistant speaks everything written to the stream, so a
// narration put there is recited whether or not anybody asked for
// one. Aloud it belongs to the aside player, which is off.
func TestAVoiceTurnIsNotToldWhatIsBeingDone(t *testing.T) {
	h := newHarness(t, &narrating{}, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelVoice, "switch to a new conversation")
	heard := h.listen(t, tk.ID)
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if got := heard(); len(got) > 0 {
		t.Errorf("a voice turn was narrated, and Home Assistant speaks it: %q", got)
	}
	if answer := answered(t, h); !strings.Contains(answer, "created") {
		t.Errorf("the answer did not survive: %q", answer)
	}
}

// TestAScreenIsStillToldWhatIsBeingDone : The other half. A progress
// line on a screen is never spoken, and a turn that works in silence
// for nine seconds reads as having stopped.
func TestAScreenIsStillToldWhatIsBeingDone(t *testing.T) {
	h := newHarness(t, &narrating{}, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelDirect, "switch to a new conversation")
	heard := h.listen(t, tk.ID)
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	var found bool
	for _, got := range heard() {
		if strings.Contains(got, "starting a new conversation") {
			found = true
		}
	}
	if !found {
		t.Errorf("a screen was told nothing about what the turn was doing: %q", heard())
	}
}
