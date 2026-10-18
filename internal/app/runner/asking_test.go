package runner_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// asksBack : A provider that answers with a question, then with
// whatever it is told to, recording every prompt it was given.
type asksBack struct {
	// first : What it says to a question it has not been pushed on.
	// again : What it says when merely told to look. given : what it
	// says once it has been handed the subject.
	first, again, given string

	mu      sync.Mutex
	at      int
	deep    bool
	prompts []string
}

func (p *asksBack) Name() string { return "asks back" }

func (p *asksBack) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}
	p.mu.Lock()
	if req.Prompt != "" {
		p.prompts = append(p.prompts, req.Prompt)
	}
	if req.Prompt == "do a deep research" {
		p.deep = true
	}
	pushed := p.deep
	p.at++
	p.mu.Unlock()

	// Answers by what it was told, which is how a real round behaves:
	// handed the subject it answers, told merely to look it asks
	// again.
	switch {
	case strings.Contains(req.Prompt, "question back to them"):
		ch <- environment.Final(p.given)
	case strings.Contains(req.Prompt, "Before that answer goes out") || req.Prompt == "":
		if pushed {
			ch <- environment.Final(p.again)
		} else {
			ch <- environment.Final(p.first)
		}
	default:
		ch <- environment.Final(p.first)
	}
	close(ch)
	return ch, nil
}

func (p *asksBack) said() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.prompts...)
}

// TestAnAnswerThatIsOnlyAQuestionIsSentBackWithItsSubject : Owner, 18
// October 2026, after asking about a pair of earbuds and then saying
// "do a deep research": *"A deep research into what, sir?"* -- twice,
// the second time after the send-back had already told it to look.
//
// The persona forbids it in so many words and the gateway will not let
// a round be required to call a tool, so the server hands over the
// subject instead of asking for it to be worked out.
func TestAnAnswerThatIsOnlyAQuestionIsSentBackWithItsSubject(t *testing.T) {
	p := &asksBack{
		first: "The Realme Buds T310 have dual microphones, sir.",
		again: "A deep research into what, sir?",
		given: "They use a six-millimetre driver, sir.",
	}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	first := h.submit(t, "look online about the realme buds")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)

	second := h.submit(t, "do a deep research")
	done := h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	var told string
	for _, said := range p.said() {
		if strings.Contains(said, "question back to them") {
			told = said
		}
	}
	if told == "" {
		t.Fatalf("a question back was sent out unchallenged. prompts: %q", p.said())
	}
	// The subject is given, not pointed at.
	if !strings.Contains(told, "dual microphones") {
		t.Errorf("the subject was not handed over:\n%s", told)
	}
	if strings.Contains(done.Response, "research into what") {
		t.Errorf("the question still reached them: %q", done.Response)
	}
}

// TestAnAnswerThatSaysSomethingAndThenAsksIsLeftAlone : An answer that
// answered has answered. persona.replying allows three questions at
// the end of one, and all three follow having looked at something.
func TestAnAnswerThatSaysSomethingAndThenAsksIsLeftAlone(t *testing.T) {
	p := &asksBack{
		first: "There is no list called that, sir. Shall I make one?",
		again: "There is no list called that, sir. Shall I make one?",
		given: "There is no list called that, sir. Shall I make one?",
	}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	tk := h.submit(t, "what is on John's list")
	done := h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	for _, said := range p.said() {
		if strings.Contains(said, "question back to them") {
			t.Errorf("an answer that answered was sent back: %q", said)
		}
	}
	if !strings.Contains(done.Response, "Shall I make one") {
		t.Errorf("the offer was lost: %q", done.Response)
	}
}

// TestAQuestionIsSentBackOnlyOnce : Bounded, or an obstinate round
// becomes a loop somebody is waiting through.
func TestAQuestionIsSentBackOnlyOnce(t *testing.T) {
	p := &asksBack{
		first: "Something about the earbuds, sir.",
		again: "About what, sir?",
		given: "About which one, sir?",
	}
	h := newHarness(t, p, runner.Options{Tools: writer(true)})

	first := h.submit(t, "look online about the realme buds")
	h.await(t, first.ID, chat.StatusCompleted, chat.StatusFailed)
	second := h.submit(t, "do a deep research")
	h.await(t, second.ID, chat.StatusCompleted, chat.StatusFailed)

	var sent int
	for _, said := range p.said() {
		if strings.Contains(said, "question back to them") {
			sent++
		}
	}
	if sent > 1 {
		t.Errorf("sent back %d times, want at most once", sent)
	}
}
