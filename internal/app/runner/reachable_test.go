package runner_test

import (
	"context"
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
			guard:   "an answer that names its own machinery",
			said:    "can you do that",
			answer:  "I have no tools for that, sir.",
			channel: chat.ChannelVoice,
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

// TestTheMachineryGuardCannotReachATypedAnswer : The gap this file was
// written to find, left standing because closing it is a trade.
//
// On 19 October the assistant answered him "Understood, sir. I will
// search before claiming that anything has not happened... I will read
// both lists of tools before saying I cannot do something" -- the word
// "tools" twice, which persona.NamesMachinery bans outright. The guard
// did not fire, and could not have.
//
// Why. The guard carries !published, because an answer already read
// aloud cannot be taken back. published is set when an answer is
// streamed rather than held, and an answer is held only while
// returnable says a send-back is still possible. returnable counts two
// reasons -- look-first and an abandoned change -- and not the other
// two, the machinery check and the length check. So on the round after
// a send-back, on a typed channel where the answer streams, published
// is true and both of those are out of reach. That is exactly the
// round that produced the answer above.
//
// Spoken answers are held whole, so the guard reaches them; the row in
// TestEveryGuardIsReachableFromARealTurn proves it on voice.
//
// Closing it means holding typed answers too, which stops them
// appearing as they are written. That is a visible change to how the
// app feels and is the owner's to make, so this test states the gap
// rather than hiding it.
func TestTheMachineryGuardCannotReachATypedAnswer(t *testing.T) {
	recorder := &recordingProvider{answer: "I have no tools for that, sir."}
	h := newHarness(t, recorder, runner.Options{Tools: guarding()})

	tk := h.submitOn(t, chat.ChannelDirect, "can you do that")
	h.await(t, tk.ID, chat.StatusCompleted, chat.StatusFailed)

	for _, p := range recorder.prompts {
		if strings.Contains(p, "it names your own machinery") {
			t.Fatal("the machinery guard now reaches a typed answer; " +
				"delete this test and move the typed row into the table above")
		}
	}

	// And the answer went out with the banned word in it.
	got, err := h.repo.Get(context.Background(), tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(got.Response, "tools") {
		t.Errorf("the answer no longer carries the word, so this gap may be closed: %q", got.Response)
	}
}
