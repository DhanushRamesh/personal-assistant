package runner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/runner"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// scripted : A provider that plays a fixed sequence of rounds and then
// answers.
//
// What the other test providers cannot do is ask for a tool, which is
// the whole of what is being tested here: the interesting failures all
// happen between a call and the answer that follows it.
type scripted struct {
	// rounds : What to send, one per request, in order. The last is
	// repeated if the chain goes round more times than it has entries.
	rounds []environment.Message
	at     int
}

func (p *scripted) Name() string { return "scripted" }

func (p *scripted) Run(_ context.Context, req environment.Request) (<-chan environment.Message, error) {
	ch := make(chan environment.Message, 1)
	// Naming and condensing run after the answer and must not consume
	// the script.
	if req.Purpose != environment.PurposeChat {
		ch <- environment.Final("an answer")
		close(ch)
		return ch, nil
	}

	at := p.at
	if at >= len(p.rounds) {
		at = len(p.rounds) - 1
	}
	p.at++
	ch <- p.rounds[at]
	close(ch)
	return ch, nil
}

// calling : One round asking for a write, with no arguments to get wrong.
func calling(name string) environment.Message {
	return environment.ToolCalls([]environment.ToolCall{
		{ID: "call-" + name, Name: name, Arguments: `{"saying":"moving the film"}`},
	})
}

// writer : A registry whose one tool writes, and either works or does
// not.
func writer(works bool) *tool.Registry {
	r, err := tool.NewRegistry(tool.Tool{
		Name:     "calendar_update",
		Domain:   "calendar",
		Writes:   true,
		Purpose:  "Change an event.",
		UseWhen:  "They ask for an event to be moved or renamed.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			if works {
				return tool.OK("Moved it.")
			}
			return tool.Failed("There is no event with that identifier.")
		},
	})
	if err != nil {
		panic(err)
	}
	return r
}

// answered : The last thing the assistant said in the conversation.
func answered(t *testing.T, h *harness) string {
	t.Helper()
	said := h.repo.Said(h.conversation(t))
	for i := len(said) - 1; i >= 0; i-- {
		if said[i].Role == conversation.Assistant && said[i].Kind == conversation.Chat {
			return said[i].Content
		}
	}
	t.Fatalf("the assistant said nothing: %v", contents(said))
	return ""
}

// denied : The sentence the server adds to an answer that may not claim
// a change.
const denied = "not actually made that change"

// TestAnAnswerCannotClaimAWriteThatFailed : The bug this exists for.
//
// Handed a tool result saying the call was not run, the assistant
// described the change as done -- "Meesaya Murukku 2 at 6:10 PM on
// Sunday the 4th, sir" -- and the person spent two more turns insisting
// it had not happened.
func TestAnAnswerCannotClaimAWriteThatFailed(t *testing.T) {
	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}, runner.Options{Tools: writer(false)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if answer := answered(t, h); !strings.Contains(answer, denied) {
		t.Errorf("a write that failed was reported as done: %q", answer)
	}
}

// TestAnAnswerKeepsAWriteThatWorked : The denial must not fire on the
// ordinary case, which is every successful change the assistant makes.
func TestAnAnswerKeepsAWriteThatWorked(t *testing.T) {
	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}, runner.Options{Tools: writer(true)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a write that worked was denied: %q", answer)
	}
}

// TestAWriteCorrectedAndThenDoneIsAChange : A write refused in one
// round and done properly in the next is a change made. The accounting
// is for the whole chat, not for one round, or the server would call
// its own corrections failures.
func TestAWriteCorrectedAndThenDoneIsAChange(t *testing.T) {
	refuse := true
	r, err := tool.NewRegistry(tool.Tool{
		Name:     "calendar_update",
		Domain:   "calendar",
		Writes:   true,
		Purpose:  "Change an event.",
		UseWhen:  "They ask for an event to be moved.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			if refuse {
				refuse = false
				return tool.Failed("Read the diary first.")
			}
			return tool.OK("Moved it.")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}, runner.Options{Tools: r})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a write that was corrected and then worked was denied: %q", answer)
	}
}

// TestAnAnswerWithNoWriteIsLeftAlone : Most turns change nothing and
// claim nothing. They must read exactly as the model wrote them.
func TestAnAnswerWithNoWriteIsLeftAlone(t *testing.T) {
	h := newHarness(t, &scripted{rounds: []environment.Message{
		environment.Final("It is twenty past four."),
	}}, runner.Options{})

	tk := h.submit(t, "what is the time")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a turn that wrote nothing was denied: %q", answer)
	}
}

// TestAnAbandonedChangeIsSentBackOnce : The repair, where the denial
// is only the safety net.
//
// Told "nothing has been changed, here is what is actually there, call
// it again with an identifier from this", the model answered "Done,
// sir. This conversation is now called name test." It had the listing
// and the instruction and simply stopped.
func TestAnAbandonedChangeIsSentBackOnce(t *testing.T) {
	refuse := true
	var runs int
	r, err := tool.NewRegistry(tool.Tool{
		Name:     "calendar_update",
		Domain:   "calendar",
		Writes:   true,
		Purpose:  "Change an event.",
		UseWhen:  "They want one moved.",
		Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
		Params:   tool.Schema{Properties: map[string]tool.Property{}},
		Run: func(context.Context, tool.Invocation) tool.Result {
			runs++
			if refuse {
				refuse = false
				return tool.Failed("Read the diary first.")
			}
			return tool.OK("Moved it.")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Call, be refused, give up and claim success -- then, having been
	// sent back, call again and get it right.
	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
		calling("calendar_update"),
		environment.Final("Moved it to six ten on Sunday."),
	}}, runner.Options{Tools: r})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if runs != 2 {
		t.Errorf("the write ran %d times, want 2: it should have been sent back once", runs)
	}
	if answer := answered(t, h); strings.Contains(answer, denied) {
		t.Errorf("a change that went through after being sent back was denied: %q", answer)
	}
}

// TestItIsSentBackOnlyOnceForAnAbandonedChange : A model that gives up
// twice has made its case, and the denial carries the answer from
// there.
func TestItIsSentBackOnlyOnceForAnAbandonedChange(t *testing.T) {
	h := newHarness(t, &scripted{rounds: []environment.Message{
		calling("calendar_update"),
		environment.Final("Moved it."),
		environment.Final("Moved it."),
		environment.Final("Moved it."),
	}}, runner.Options{Tools: writer(false)})

	tk := h.submit(t, "move the movie to ten past six")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	if answer := answered(t, h); !strings.Contains(answer, denied) {
		t.Errorf("a write that never landed was reported as done: %q", answer)
	}
}
