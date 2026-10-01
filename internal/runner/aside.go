package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// AsideFor : How long saying one sentence beside a turn may take.
//
// Detached from the turn's own context, which is cancelled the moment the
// answer is ready. Tied to it, the sentence would be cut off by the very
// thing it was covering the wait for.
const AsideFor = 30 * time.Second

// AsideGap : The least quiet between one thing being said beside a
// turn and the next.
//
// A minimum, not a pause added on top. Rounds are about three seconds
// apart on their own, so most of the time this costs nothing -- it is
// there for the round that comes back fast, where two sentences would
// otherwise arrive as one run-on and sound like a fault.
const AsideGap = 1500 * time.Millisecond

// AnswerGap : The least quiet between the last thing said beside a
// turn and the answer itself.
//
// Longer than the gap between asides, so the answer is audibly a
// different kind of thing from the narration in front of it. Without
// it, "looking at your inbox" and "you have ten messages from Amazon"
// run together and the person cannot tell which part was the answer.
const AnswerGap = 2 * time.Second

// speech : What has already been said beside one turn.
//
// Per turn rather than on the Runner, which answers several at once.
// The mutex is what makes the gaps mean anything: asides are said in
// their own goroutines so they never delay the turn, and without
// serialising them two rounds finishing close together would speak
// over each other rather than after each other.
type speech struct {
	mu sync.Mutex
	// done : When the last aside finished being said, zero if none has.
	done time.Time
}

// after : How long to wait before the next thing may be said. Called
// holding the lock.
func (s *speech) after(gap time.Duration) time.Duration {
	if s.done.IsZero() {
		return 0
	}
	return gap - time.Since(s.done)
}

// sayAside : Says what the model announced it was about to do, aloud, while
// the turn is still running.
//
// Spoken turns only. Typed, the same words already arrive as a progress
// event and reading them aloud to nobody is noise.
//
// Nothing here may fail or delay the turn. It runs detached and its errors
// are logged: an assistant that answers without having said what it was
// doing is the assistant there was before this.
func (r *Runner) sayAside(ctx context.Context, t *chat.Chat, s *speech, text string) bool {
	if r.aside == nil || t.Channel != chat.ChannelVoice {
		return false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	go func() {
		said, done := context.WithTimeout(context.WithoutCancel(ctx), AsideFor)
		defer done()

		// One at a time, in the order the rounds happened. Held for
		// as long as the sentence takes to play, so the next round
		// queues behind it rather than over it.
		s.mu.Lock()
		defer s.mu.Unlock()

		if wait := s.after(AsideGap); wait > 0 {
			select {
			case <-time.After(wait):
			case <-said.Done():
				return
			}
		}

		if err := r.aside.Say(said, text); err != nil {
			r.logger.WarnContext(said, "cannot say what is being done",
				slog.Any("error", err))
			return
		}
		// Waited out here rather than left running, so that the next
		// aside's gap is measured from when this one stopped being
		// audible and not from when it was handed over.
		if err := r.aside.Settled(said); err != nil {
			r.logger.WarnContext(said, "cannot tell when what was said finished",
				slog.Any("error", err))
		}
		s.done = time.Now()

		r.logger.InfoContext(said, "said what is being done",
			slog.String("text", text))
	}()
	return true
}

// settleAside : Holds an answer back until what was said before it has
// finished being said.
//
// The aside and the answer go through different players and nothing
// sequences them, so an answer released immediately begins over the top of
// an aside still playing. The answer waits instead: what the assistant is
// doing is said, and then what it found.
//
// Called on spoken turns only, and cheap when nothing is playing: one
// reading of the player's state, which is on this machine. Nothing here may
// fail a turn -- a player that cannot be read is treated as finished.
// A nil speech means the caller does not know what was said beside
// this turn -- a timeout or a cancellation, reached from outside the
// loop. The answer still waits for the player, it just has no gap to
// measure from.
func (r *Runner) settleAside(ctx context.Context, t *chat.Chat, s *speech) {
	if r.aside == nil || t.Channel != chat.ChannelVoice {
		return
	}

	wait, done := context.WithTimeout(context.WithoutCancel(ctx), AsideFor)
	defer done()

	started := time.Now()

	// Behind whatever is still queued, then the beat before an answer.
	var gap time.Duration
	if s != nil {
		s.mu.Lock()
		gap = s.after(AnswerGap)
		s.mu.Unlock()
	}
	if gap > 0 {
		select {
		case <-time.After(gap):
		case <-wait.Done():
			return
		}
	}

	if err := r.aside.Settled(wait); err != nil {
		r.logger.WarnContext(wait, "cannot tell whether what was being said has finished",
			slog.Any("error", err))
		return
	}
	if waited := time.Since(started); waited > asideNoticed {
		r.logger.InfoContext(wait, "held the answer back until the aside finished",
			slog.Duration("waited", waited))
	}
}

// asideNoticed : How long a wait has to be before it is worth a log line.
const asideNoticed = 250 * time.Millisecond

// takeSaying : The sentence the model said this round of calls was for.
//
// Read without changing the calls. What the model sent is what the
// timeline should show, and stripping the argument here left the trace
// reading "{}" for a call that in fact carried a sentence. The registry
// takes it off on the way into the tool, which is the only place that
// must not see it.
//
// One sentence covers the round: a model asking for three tools at once
// is doing one thing, and describing each call separately would say three
// things about it.
func takeSaying(calls []environment.ToolCall) string {
	for i := range calls {
		if one, _ := tool.TakeSaying(json.RawMessage(calls[i].Arguments)); strings.TrimSpace(one) != "" {
			return strings.TrimSpace(one)
		}
	}
	return ""
}

// alreadyRead : The tools that have run before the model was asked
// anything, by name.
//
// A prefetched listing was put in front of the model as though it had
// called it, so a write in that domain has already had its read and must
// not be made to ask twice.
func (r *Runner) alreadyRead(t *chat.Chat) []string {
	if r.tools == nil {
		return nil
	}
	wanted := r.tools.Prefetched(t.Channel)
	out := make([]string, 0, len(wanted))
	for _, x := range wanted {
		out = append(out, x.Name)
	}
	return out
}
