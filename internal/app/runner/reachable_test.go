package runner_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"personal-assistant/internal/app/runner"
	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
)

// This file answers one question about every guard in the runner: does
// it fire on a real turn.
//
// Not whether it works when called -- each already has a test for that,
// and each of those passed on 19 October while the fault it guards
// against happened anyway. Five faults in one day were all the same
// shape: a correct guard that nothing reached.
//
//   - the in-passing list said "never a yes" and had "okay" on it, so
//     an offer answered with "ok" was never sent back to look
//   - NamesMachinery bans the word "tools" and the answer said it twice
//   - the charging rate was computed, behind a door the question
//     never opened
//   - the recall warning was in the running binary while a sleep note
//     answered a question about a jailbreak developer
//   - lookFirst's instruction reached a round begun before the answer
//     existed, 109 times of 109
//
// From outside, machinery that never reaches the moment is the same as
// no machinery. So each guard here is driven from a submitted turn and
// has to show itself in what the server sent the model next.

// guarding : A registry with one read and one write, which is what the
// guards need to be reachable at all -- most of them require tools to
// have been offered.
func guarding() *tool.Registry {
	r, err := tool.NewRegistry(
		tool.Tool{
			Name: "task_list", Domain: "task", Lists: true,
			Purpose: "What is on a list.", UseWhen: "They ask what is on it.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.OK("Milk, bread.")
			},
		},
		tool.Tool{
			Name: "task_lists", Domain: "task", Lists: true,
			Purpose: "The lists there are.", UseWhen: "They ask which lists exist.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.OK("Shopping, Jarvis Improvements.")
			},
		},
		tool.Tool{
			Name: "task_add", Domain: "task", Writes: true,
			Purpose: "Add to a list.", UseWhen: "They ask for something to be added.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				return tool.Failed("That list is not there, sir.")
			},
		},
	)
	if err != nil {
		panic(err)
	}
	return r
}

// TestEveryGuardIsReachableFromARealTurn : Each guard, tripped by a
// submitted turn, shown by the instruction the server sent next.
//
// A guard that stops being reachable fails here. That is the whole
// point: its own unit test would still pass.
func TestEveryGuardIsReachableFromARealTurn(t *testing.T) {
	for _, c := range []struct {
		guard   string
		said    string
		answer  string
		channel chat.Channel
		calls   []string
		// before : What the assistant said last, where the guard needs it.
		before string
		// shows : Words from the instruction the guard sends back with.
		shows string
	}{
		{
			guard: "look first, when no tool was called",
			said:  "open Spotify",
			shows: "you did not use any tool this turn",
		},
		{
			guard:  "an answer that is only a question back",
			said:   "do a deep research",
			answer: "Into what, sir?",
			before: "The Realme Buds T310 have dual microphones, sir.",
			shows:  "it is a question back to them and nothing else",
		},
		{
			guard: "a change asked for and abandoned",
			said:  "add milk to the shopping list",
			calls: []string{"task_add"},
			shows: "you asked for something to be changed",
		},
		{
			guard:   "a spoken answer that ran long",
			said:    "how was the traffic",
			answer:  "One. Two. Three. Four.",
			channel: chat.ChannelVoice,
			shows:   "they are listening to it",
		},
		{
			guard: "a written answer that ran as prose",
			said:  "why not the pasted prompt is not good?",
			answer: "It rejected most of the changes because they were rewording only. " +
				"Moving a thing from a check to a sentence does not sharpen it. " +
				"The three pieces it kept are worth lifting. " +
				"They add something the current one does not have. " +
				"The four-way distinction is one of them.",
			channel: chat.ChannelDirect,
			shows:   "it has a paragraph of four or more sentences in it",
		},
		{
			guard:   "an answer that names its own machinery, spoken",
			said:    "can you do that",
			answer:  "I have no tools for that, sir.",
			channel: chat.ChannelVoice,
			shows:   "it names your own machinery",
		},
		{
			// Typed, which was out of reach until 19 October 2026: a
			// written answer is streamed, streamed is published, and
			// published is what the shape guards will not touch. This
			// is the row the test stating that gap said to move here
			// once it could fire. It fired.
			guard:   "an answer that names its own machinery, typed",
			said:    "can you do that",
			answer:  "I have no tools for that, sir.",
			channel: chat.ChannelDirect,
			shows:   "it names your own machinery",
		},
	} {
		t.Run(c.guard, func(t *testing.T) {
			recorder := &recordingProvider{answer: c.answer, calls: c.calls}
			h := newHarness(t, recorder, runner.Options{Tools: guarding()})

			if c.before != "" {
				if _, err := h.repo.Append(context.Background(), conversation.Message{
					ID:             conversation.NewMessageID(),
					ConversationID: h.conversation(t),
					Kind:           conversation.Chat,
					Role:           conversation.Assistant,
					Content:        c.before,
					At:             time.Now().UTC(),
				}); err != nil {
					t.Fatalf("Append: %v", err)
				}
			}

			channel := c.channel
			if channel == "" {
				channel = chat.ChannelDirect
			}
			tk := h.submitOn(t, channel, c.said)
			h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

			var sent []string
			for _, p := range recorder.prompts {
				sent = append(sent, p)
			}
			for _, p := range sent {
				if strings.Contains(p, c.shows) {
					return
				}
			}
			t.Errorf("the guard never fired. %d prompts went out, none carrying %q:\n%s",
				len(sent), c.shows, strings.Join(shorten(sent), "\n---\n"))
		})
	}
}

// TestWhatIsSaidInPassingStopsTheLookFirstGuard : The other direction.
//
// In-passing is the one guard whose job is to stop another from
// firing, so it is reachable only by its absence. "Okay" after a
// question is not in passing -- that was the 19 October fault -- and
// "thanks" is.
func TestWhatIsSaidInPassingStopsTheLookFirstGuard(t *testing.T) {
	for _, c := range []struct {
		said     string
		before   string
		sentBack bool
	}{
		{said: "thanks", before: "It is twenty past four, sir.", sentBack: false},
		{said: "okay", before: "It is twenty past four, sir.", sentBack: false},
		{said: "okay", before: "Shall I read the numbers out?", sentBack: true},
		{said: "open Spotify", before: "It is twenty past four, sir.", sentBack: true},
	} {
		t.Run(c.said+" after "+shortest(c.before), func(t *testing.T) {
			recorder := &recordingProvider{}
			h := newHarness(t, recorder, runner.Options{Tools: guarding()})

			if _, err := h.repo.Append(context.Background(), conversation.Message{
				ID:             conversation.NewMessageID(),
				ConversationID: h.conversation(t),
				Kind:           conversation.Chat,
				Role:           conversation.Assistant,
				Content:        c.before,
				At:             time.Now().UTC(),
			}); err != nil {
				t.Fatalf("Append: %v", err)
			}

			tk := h.submit(t, c.said)
			h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

			back := len(recorder.prompts) > 1
			if back != c.sentBack {
				t.Errorf("%q after %q: sent back = %v, want %v (%d prompts)",
					c.said, c.before, back, c.sentBack, len(recorder.prompts))
			}
		})
	}
}

func shorten(all []string) []string {
	out := make([]string, 0, len(all))
	for _, one := range all {
		if len(one) > 120 {
			one = one[:119] + "…"
		}
		out = append(out, one)
	}
	return out
}

func shortest(s string) string {
	if len(s) > 24 {
		return s[:23] + "…"
	}
	return s
}

// counting : A registry whose tools count their own calls, so a retry
// can be seen.
type ranTimes struct {
	reads, writes, refusals int
}

func (c *ranTimes) registry(t *testing.T) *tool.Registry {
	t.Helper()
	r, err := tool.NewRegistry(
		tool.Tool{
			Name: "task_list", Domain: "task", Lists: true,
			Purpose: "What is on a list.", UseWhen: "They ask what is on it.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				c.reads++
				return tool.Broke("I could not read your lists, sir.", errors.New("dial tcp: i/o timeout"))
			},
		},
		tool.Tool{
			Name: "task_find", Domain: "task", Lists: true,
			Purpose: "Find a task.", UseWhen: "They name one.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				c.refusals++
				return tool.Failed("There is no task called that, sir.")
			},
		},
		tool.Tool{
			Name: "task_add", Domain: "task", Writes: true,
			Purpose: "Add to a list.", UseWhen: "They ask for something added.",
			Channels: []chat.Channel{chat.ChannelVoice, chat.ChannelDirect},
			Params:   tool.Schema{Properties: map[string]tool.Property{}},
			Run: func(context.Context, tool.Invocation) tool.Result {
				c.writes++
				return tool.Broke("I could not add that, sir.", errors.New("dial tcp: i/o timeout"))
			},
		},
	)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return r
}

// TestWhatBrokeIsRunOnceMore : A read that broke underneath did not
// happen, so running it again is not running it twice.
//
// Three rules, and each is the reason for the next. Only a failure
// with a fault behind it: a tool saying no on purpose says the same no
// the second time and costs a second call. Only a read: a write that
// failed halfway may have applied half of itself. And once only, or a
// service that is down is asked twice for every tool in every round.
func TestWhatBrokeIsRunOnceMore(t *testing.T) {
	// A write is held by retryable itself rather than through a turn:
	// reaching one takes a read of its domain first, which is a
	// different guard, and a test that trips that one proves nothing
	// about this one.
	broke := tool.Broke("I could not add that, sir.", errors.New("dial tcp: i/o timeout"))
	if runner.Retryable(broke, tool.Tool{Name: "task_add", Writes: true}, true) {
		t.Error("a write that broke would be run again; half of it may already have applied")
	}
	if !runner.Retryable(broke, tool.Tool{Name: "task_list"}, true) {
		t.Error("a read that broke would not be run again")
	}
	if runner.Retryable(tool.Failed("There is no task called that, sir."), tool.Tool{Name: "task_find"}, true) {
		t.Error("a tool saying no on purpose would be asked again for the same no")
	}

	for _, c := range []struct {
		what string
		// call : What the model asks for, in order. A write needs a
		// read of its own domain first or it is refused before it
		// runs at all, which is a different guard and not this one.
		call  []string
		count func(*ranTimes) int
		want  int
	}{
		{"a read that broke", []string{"task_list"}, func(c *ranTimes) int { return c.reads }, 2},
		{"a read that said no", []string{"task_find"}, func(c *ranTimes) int { return c.refusals }, 1},
	} {
		t.Run(c.what, func(t *testing.T) {
			ran := &ranTimes{}
			recorder := &recordingProvider{calls: c.call}
			h := newHarness(t, recorder, runner.Options{Tools: ran.registry(t)})

			tk := h.submit(t, "what is on the shopping list")
			h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

			if got := c.count(ran); got != c.want {
				t.Errorf("%s ran %d times, want %d", c.what, got, c.want)
			}
		})
	}
}
