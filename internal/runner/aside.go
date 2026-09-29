package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
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

// sayAside : Says what the model announced it was about to do, aloud, while
// the turn is still running.
//
// Spoken turns only. Typed, the same words already arrive as a progress
// event and reading them aloud to nobody is noise.
//
// Nothing here may fail or delay the turn. It runs detached and its errors
// are logged: an assistant that answers without having said what it was
// doing is the assistant there was before this.
func (r *Runner) sayAside(ctx context.Context, t *chat.Chat, text string) bool {
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

		if err := r.aside.Say(said, text); err != nil {
			r.logger.WarnContext(said, "cannot say what is being done",
				slog.Any("error", err))
			return
		}
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
func (r *Runner) settleAside(ctx context.Context, t *chat.Chat) {
	if r.aside == nil || t.Channel != chat.ChannelVoice {
		return
	}

	wait, done := context.WithTimeout(context.WithoutCancel(ctx), AsideFor)
	defer done()

	started := time.Now()
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
