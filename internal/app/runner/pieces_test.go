package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/world/events"
)

// speaking : A provider that writes its answer in pieces before
// finishing it, which is what a real one does.
type speaking struct {
	rounds []environment.Message
	mu     sync.Mutex
	at     int
}

func (p *speaking) Name() string { return "speaking" }

func (p *speaking) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}

	p.mu.Lock()
	at := p.at
	// The send-back's round is begun beside the one in front of it
	// and is usually thrown away, so it takes no place in the
	// script: a real provider answers what it is asked and keeps no
	// queue.
	if lookingFirst(req) {
		at = len(p.rounds) - 1
	} else {
		if at >= len(p.rounds) {
			at = len(p.rounds) - 1
		}
		p.at++
	}
	p.mu.Unlock()

	round := p.rounds[at]
	// On its own goroutine, like a real provider: an answer is longer
	// than any buffer and the caller reads while it is written.
	go func() {
		defer close(ch)
		if round.Kind == environment.KindFinal {
			// Written a word at a time, like the real thing.
			for _, word := range strings.SplitAfter(round.Text, " ") {
				if word != "" {
					ch <- environment.Partial(word)
				}
			}
		}
		ch <- round
	}()
	return ch, nil
}

// overheard : The pieces of an answer that actually reached a
// listener.
func overheard(t *testing.T, h *harness, chatID string) func() []string {
	t.Helper()
	ch, stop := h.bus.Subscribe(chatID)
	t.Cleanup(stop)

	var mu sync.Mutex
	var got []string
	go func() {
		for ev := range ch {
			if ev.Kind != events.KindPartial {
				continue
			}
			mu.Lock()
			got = append(got, ev.Text)
			mu.Unlock()
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

// TestAnAnswerThatCanBeSentBackIsNotSpoken : The bug this exists
// for.
//
// Streaming went in and the assistant started saying everything
// twice. A model that answers without touching a tool is sent back
// once to look at them -- but by then the answer it gave had already
// been spoken, and the round that replaced it was heard as a repeat.
//
// The pieces of a round that may still be sent back are read -- that
// is how the runner knows the round is prose rather than a tool call
// -- and not passed on.
func TestAnAnswerThatCanBeSentBackIsNotSpoken(t *testing.T) {
	p := &speaking{rounds: []environment.Message{
		environment.Final("There is no such thing, sir."),
		environment.Final("Nothing of the kind, sir."),
	}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelVoice, "move the movie to ten past six")
	heard := overheard(t, h, tk.ID)
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	said := strings.Join(heard(), "")
	if strings.Contains(said, "There is no such thing") {
		t.Errorf("an answer that was then sent back had already been spoken: %q", said)
	}
	if !strings.Contains(said, "Nothing of the kind") {
		t.Errorf("the answer that stands was never spoken: %q", said)
	}
}

// TestTheAnswerAfterToolsIsSpoken : The ordinary voice turn, and the
// one streaming was built for. Once a tool has been asked for the
// answer cannot be sent back to look at them, so it goes out in
// pieces.
func TestTheAnswerAfterToolsIsSpoken(t *testing.T) {
	p := &speaking{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelVoice, "move the movie to ten past six")
	heard := overheard(t, h, tk.ID)
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if said := strings.Join(heard(), ""); !strings.Contains(said, "Moved it") {
		t.Errorf("the answer after a tool ran was never spoken: %q", said)
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

// counting : A provider that records how many rounds were asked for
// and how far apart, so the overlap can be seen.
type counting struct {
	mu     sync.Mutex
	at     int
	begun  []time.Time
	rounds []environment.Message
	pause  time.Duration
}

func (p *counting) Name() string { return "counting" }

func (p *counting) Run(ctx context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}

	p.mu.Lock()
	at := p.at
	if lookingFirst(req) {
		at = len(p.rounds) - 1
	} else {
		if at >= len(p.rounds) {
			at = len(p.rounds) - 1
		}
		p.at++
	}
	p.begun = append(p.begun, time.Now())
	p.mu.Unlock()

	round := p.rounds[at]
	go func() {
		defer close(ch)
		if round.Kind == environment.KindFinal {
			// A word, then a pause, then the rest. The pause is where
			// the round after this one has its chance to start.
			words := strings.SplitAfter(round.Text, " ")
			ch <- environment.Partial(words[0])
			select {
			case <-time.After(p.pause):
			case <-ctx.Done():
				return
			}
			for _, w := range words[1:] {
				if w != "" {
					ch <- environment.Partial(w)
				}
			}
		}
		ch <- round
	}()
	return ch, nil
}

func (p *counting) started() []time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.begun...)
}

// TestTheSendBackStartsBeforeItIsNeeded : The whole point of the
// overlap.
//
// The send-back is right about one turn in four and the other three
// cost three to five seconds for an answer barely different from the
// one thrown away. It cannot be prevented -- moving its caution into
// the prompt was tried twice and made tool choice worse both times --
// but a streamed round shows text before it shows a tool call, so the
// first fragment of prose is proof the round will be sent back, well
// before it finishes saying so.
func TestTheSendBackStartsBeforeItIsNeeded(t *testing.T) {
	const pause = 300 * time.Millisecond
	p := &counting{pause: pause, rounds: []environment.Message{
		environment.Final("There is no such thing, sir."),
		environment.Final("Nothing of the kind, sir."),
	}}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submitOn(t, chat.ChannelVoice, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	begun := p.started()
	if len(begun) < 2 {
		t.Fatalf("only %d rounds were asked for", len(begun))
	}
	// The second must have started while the first was still being
	// written, which is during the pause rather than after it.
	if gap := begun[1].Sub(begun[0]); gap >= pause {
		t.Errorf("the second round waited for the first to finish: %s apart, "+
			"and the first took at least %s", gap, pause)
	}
}

// TestARoundThrownAwayChangesNothing : The cost of beginning the
// send-back at once is a round spent on the three turns in four
// where the guard changes nothing. The cost it must never be is a
// tool run twice.
//
// A discarded round is discarded whole: its stream is dropped
// without its tool calls being read, so nothing it asked for
// happens.
func TestARoundThrownAwayChangesNothing(t *testing.T) {
	var ran int
	var mu sync.Mutex
	counted, err := tool.NewRegistry(tool.Tool{
		Name:     "calendar_update",
		Domain:   "calendar",
		Writes:   true,
		Purpose:  "Change an event.",
		UseWhen:  "They ask for an event to be moved.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			mu.Lock()
			ran++
			mu.Unlock()
			return tool.OK("Moved it.")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &counting{pause: 20 * time.Millisecond, rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}
	h := newHarness(t, p, runner.Options{Tools: counted})

	tk := h.submitOn(t, chat.ChannelVoice, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	mu.Lock()
	times := ran
	mu.Unlock()
	if times != 1 {
		t.Errorf("the write ran %d times, want 1", times)
	}
	if answer := answered(t, h); !strings.Contains(answer, "Moved it") {
		t.Errorf("the answer was lost: %q", answer)
	}
	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a write that worked was denied: %q", answer)
	}
}

// lookingFirst : Whether this request is the send-back's round.
//
// The runner starts that round at the same moment as the one before
// it, so a provider that plays a fixed script would have the script
// eaten by a round that may be thrown away. A real provider answers
// whatever it is asked and keeps no place in a queue; these say so
// by recognising the question.
func lookingFirst(req environment.Request) bool {
	return strings.Contains(req.Prompt, "you did not use any tool this turn")
}
