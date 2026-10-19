package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"personal-assistant/internal/domain/persona"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/platform/logging"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/thinking/environment"
)

// AsideFor : How long saying one sentence beside a turn may take.
//
// Detached from the turn's own context, which is cancelled the moment the
// answer is ready. Tied to it, the sentence would be cut off by the very
// thing it was covering the wait for.
const AsideFor = 30 * time.Second

// AsideAfter : How long a turn may take before anything is said
// beside it.
//
// The aside exists to cover a wait. It was being said whether or not
// there was one: measured on 3 October 2026, the median turn spent
// 4.5 seconds after its tool had already returned, and the worst
// spent 12 -- four sentences of narration in front of an answer that
// had been ready for ten milliseconds. Insurance against a slow turn
// had become a tax on every turn.
//
// So nothing is said until the turn has actually been slow. Under
// this it answers in silence, which is what fast should sound like.
const AsideAfter = 1200 * time.Millisecond

// AsideGap : The least quiet between one thing being said beside a
// turn and the next.
//
// A minimum, not a pause added on top. Rounds are about three seconds
// apart on their own, so most of the time this costs nothing -- it is
// there for the round that comes back fast, where two sentences would
// otherwise arrive as one run-on and sound like a fault.
//
// Shortened with AnswerGap on 3 October 2026. Both were set when the
// narration was unconditional and nobody had measured what it cost;
// with asides said only on a turn that is actually slow, a second and
// a half between two of them is a second and a half of a wait that is
// already too long.
const AsideGap = 400 * time.Millisecond

// AnswerGap : The least quiet between the last thing said beside a
// turn and the answer itself.
//
// Longer than the gap between asides, so the answer is audibly a
// different kind of thing from the narration in front of it. Without
// it, "looking at your inbox" and "you have ten messages from Amazon"
// run together and the person cannot tell which part was the answer.
// A test holds that invariant: this has to be the longest beat in the
// turn, or the answer sounds like one more piece of narration.
//
// Two seconds was chosen for that and paid on every spoken turn that
// narrated anything. Eight hundred milliseconds separates them
// audibly and is over a second nobody is waiting through.
const AnswerGap = 800 * time.Millisecond

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

	// abandoned : That nothing further should be said beside this turn.
	//
	// Atomic rather than under the mutex, because the mutex is held for
	// as long as a sentence takes to play. A caller abandoning the turn
	// would otherwise wait out the very sentence it is trying to
	// prevent the next one of.
	abandoned atomic.Bool
}

// stop : Abandons whatever has not been said yet.
//
// For a turn that has failed. The asides describe work that was about to
// happen, and once it is not going to, saying them is worse than silence:
// the person hears the thing being promised after it has already gone
// wrong, and the apology arrives last.
//
// A sentence already playing is left to finish. Cutting speech off
// mid-word sounds like a fault in its own right, and it is one sentence,
// not the queue behind it.
func (s *speech) stop() { s.abandoned.Store(true) }

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
		// Saying what is being done is a courtesy, and a panic
		// in a courtesy should not end the turn it is covering,
		// let alone the server.
		logging.Surviving(said, r.logger, "saying what is being done", func() {

			// Nothing at all until the turn has been slow enough to need
			// covering. A turn that finishes inside this says nothing
			// beside itself, because there was no silence to fill.
			select {
			case <-time.After(AsideAfter):
			case <-said.Done():
				return
			}

			// One at a time, in the order the rounds happened. Held for
			// as long as the sentence takes to play, so the next round
			// queues behind it rather than over it.
			if s.abandoned.Load() {
				return
			}

			s.mu.Lock()
			defer s.mu.Unlock()

			if wait := s.after(AsideGap); wait > 0 {
				select {
				case <-time.After(wait):
				case <-said.Done():
					return
				}
			}

			// Checked again, after the queue and after the gap. Most of an
			// aside's life is spent waiting for the one in front of it, and
			// the turn usually fails during that wait rather than before it.
			if s.abandoned.Load() {
				r.logger.InfoContext(said, "did not say what was being done, the turn had failed",
					slog.String("text", text))
				return
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
		})
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
// narration : What a round says it is about to do, addressed as the
// persona addresses them; empty when it said nothing.
func (r *Runner) narration(calls []environment.ToolCall) string {
	said := takeSaying(calls)
	if said == "" {
		return ""
	}
	return persona.Addressed(said, persona.AddressFor(r.personaID()), said)
}

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
func (r *Runner) alreadyRead(note chat.Recalled) []string {
	out := make([]string, 0, len(note.Tools))
	for _, x := range note.Tools {
		out = append(out, x.Name)
	}
	return out
}
