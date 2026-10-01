package runner

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/events"
	"github.com/DhanushRamesh/personal-assistant/internal/failure"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// execute : Runs one chat from start to a terminal status.
//
// ctx carries the chat's logging attributes and outlives the run, so that a
// stopped chat can still record why. lifeCtx is cancelled to stop the chat and
// covers the wait for a slot as well as the run itself.
func (r *Runner) execute(ctx, lifeCtx context.Context, t *chat.Chat) {
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-lifeCtx.Done():
		// Stopped before it ever started. It never ran, so no deadline can
		// have passed, and nothing has been said beside it.
		r.finishStopped(ctx, t, nil, nil)
		return
	}

	// The deadline covers the run itself, not the wait for a slot.
	runCtx, stopRun := context.WithTimeout(lifeCtx, r.chatTimeout)
	defer stopRun()

	if err := t.Start(); err != nil {
		r.logger.ErrorContext(ctx, "cannot start chat", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.logger.InfoContext(ctx, "chat started")

	// Composed once. It reads the database and searches memory, and every
	// hop of the tool loop must be given the same one.
	systemPrompt, missed := r.promptFor(ctx, t)

	r.consume(runCtx, ctx, t, systemPrompt)

	// Once there is an answer, not before. Marking them while composing
	// the prompt spent the one telling on turns that then failed.
	r.mentioned(ctx, missed)

	// After the answer is recorded and announced, so that maintaining the
	// conversation's memory is never in front of the person waiting for it. The
	// slot is still held, which keeps this from competing with the next
	// chat for the same environment.
	// Both run after the answer has been delivered, and both hold the slot
	// so they cannot pile up. Naming first, since it is the shorter of the
	// two and is the one somebody is waiting to hear.
	r.title(ctx, t)
	r.condense(ctx, t, systemPrompt)
	r.index(ctx)
}

// consume : Reads the provider's stream and records what it produces.
//
// runCtx bounds the provider's work and is cancelled to stop it. ctx outlives
// it and is used for the final write, because a cancelled context cannot be
// used to record that the chat was cancelled.
func (r *Runner) consume(runCtx, ctx context.Context, t *chat.Chat, systemPrompt string) {
	window := r.history(ctx, t, systemPrompt)
	turns := toProviderTurns(window.Messages)
	prompt := t.Prompt

	// What the tools this turn ran have obliged the answer to mention.
	// Gathered as they run and settled once, at the end, because the
	// model may take several rounds to get to its answer.
	var owed []tool.Owed

	// Whether the model said aloud what it was about to do. Once per turn
	// at most: a chain of five rounds would otherwise interrupt five times
	// to describe work the person did not ask about.
	var spoke bool

	// Every tool that has run in this turn, so a write can insist its own
	// domain was read first. Prefetched listings count: the model was
	// shown them before it was asked anything.
	ran := r.alreadyRead(t)

	// Whether the model has asked for any tool this turn, and whether
	// it has already been sent back once to look at what it has.
	//
	// Not the same question as whether anything ran: memory is
	// prefetched on every turn, so something has always run. This is
	// about what the model chose.
	var reached, pressed bool

	// What has been said beside this turn, so each round's sentence
	// follows the last rather than landing on top of it.
	aloud := &speech{}

	// revealed : The deferred tools the model has asked about, which are
	// described from the next round on. Per chat, not per turn: having
	// been told how to call something once, it is not taken away again
	// mid-chain.
	revealed := map[string]bool{}

	for hop := 0; ; hop++ {
		// The last round is offered nothing. A model that has run out of
		// rounds must answer from what it gathered, and saying what it
		// managed is better than being cut off mid-chain with nothing to
		// show for the work that already ran.
		tools := r.offered(t, revealed)
		if hop >= MaxToolHops-1 {
			if len(tools) > 0 {
				r.logger.WarnContext(ctx, "the tool chain ran long, so the last round is asked without tools",
					slog.Int("hops", hop))
			}
			tools = nil
		}

		// The names of the tools that are not being described, appended
		// per round rather than built into the prompt: the list shrinks
		// as the model asks about them, and one that has just been
		// described must stop appearing under "you have not been given
		// their arguments yet" or it gets asked about twice.
		//
		// Cheap to rebuild -- it reads the registry and nothing else --
		// unlike the prompt itself, which reads the database and
		// searches memory.
		asking := systemPrompt
		if len(tools) > 0 {
			asking = r.asking(systemPrompt, t, revealed)
		}

		stream, err := r.environment.Run(runCtx, environment.Request{
			Prompt:  prompt,
			History: turns,
			Summary: window.Summary,
			Vendor:  t.Model.Vendor,
			Model:   t.Model.ID,
			Tools:   tools,

			SystemPrompt: asking,
		})
		if err != nil {
			r.logger.ErrorContext(ctx, "environment would not start", slog.Any("error", err))
			r.finishWith(ctx, t, aloud, func() error {
				// The cause is kept, not only logged. Asked afterwards
				// what went wrong, the assistant can only answer from
				// what the conversation holds, and this used to hold
				// nothing.
				return t.FailWith(
					r.failureSentence(failure.Sentence(failure.Unreachable)),
					string(failure.Unreachable), err.Error())
			})
			return
		}

		final := r.drain(ctx, t, stream, aloud, &spoke)

		switch {
		case final == nil:
			// The stream closed with no terminal message, which the contract
			// says means the run was stopped rather than finished.
			r.finishStopped(ctx, t, aloud, runCtx.Err())
			return

		case final.Kind == environment.KindError:
			if final.Code != "" && !failure.Known(failure.Code(final.Code)) {
				r.logger.WarnContext(ctx, "environment sent an unknown failure code",
					slog.String("code", final.Code))
			}
			r.finishWith(ctx, t, aloud, func() error {
				return t.FailWith(r.failureSentence(final.Text),
					final.Code, final.Detail)
			})
			return

		case final.Kind != environment.KindToolCalls:
			// An answer reached without touching a single tool, on a
			// turn where tools were offered, is sent back once to
			// look at them.
			//
			// Because the answer that keeps being wrong is "I cannot"
			// and "there is no such thing": no list called John's,
			// no birthday on record, and -- with task_list_add sitting
			// in the same prompt -- "I am not able to create task
			// lists from here, that needs to be done on your device".
			// Each was a claim about what exists or what it can do,
			// made without looking, and each was false.
			//
			// The persona says to check before saying no. It did not
			// hold, five times. This is the same rule with the server
			// behind it.
			//
			// Nothing is forced. Plenty of answers need no tool at
			// all, and after being asked to look the model is free to
			// say the same thing again. What it cannot do is never
			// look.
			if !reached && !pressed && len(tools) > 0 {
				r.logger.InfoContext(ctx, "an answer was sent back to look at the tools")
				pressed = true
				turns = append(turns, asUserTurn(prompt)...)
				prompt = lookFirst
				continue
			}
			r.complete(ctx, t, aloud, tool.Ensure(final.Text,
				persona.AddressFor(r.personaID()), tool.Merged(owed)))
			return
		}
		reached = true

		// Tools were asked for. Run them, remember both halves, and go round
		// again with what they returned. The question is not repeated: it is
		// in the history now, and asking it twice would have the model answer
		// it twice.
		turns = append(turns, asUserTurn(prompt)...)

		// What the model said this round of calls is for, taken off the
		// arguments before anything records or runs them. Said aloud once
		// per turn, so a chain of rounds does not interrupt at every one.
		raw := make([]string, 0, len(final.ToolCalls))
		for _, c := range final.ToolCalls {
			raw = append(raw, c.Name+" "+c.Arguments)
		}
		said := takeSaying(final.ToolCalls)
		r.logger.InfoContext(ctx, "saying on the call",
			slog.String("said", said),
			slog.String("channel", string(t.Channel)),
			slog.Any("raw", raw))
		// Every round says what it is about to do, not only the
		// first. A chain that narrates once and then works in silence
		// for nine seconds reads as having stopped; the gaps in
		// sayAside are what keep several of them from running
		// together.
		if r.sayAside(ctx, t, aloud, said) {
			spoke = true
		}

		ranTurns, ranOwed, reveal := r.runTools(ctx, t, final.ToolCalls, &ran, revealed)
		turns = append(turns, ranTurns...)
		owed = append(owed, ranOwed...)
		for _, name := range reveal {
			revealed[name] = true
		}
		prompt = ""

		if runCtx.Err() != nil {
			// Stopped while the tools were running. What ran, ran, and the
			// transcript already says so.
			r.finishStopped(ctx, t, aloud, runCtx.Err())
			return
		}
	}
}

// failureSentence : What a failure says, in the voice of whoever is
// answering.
//
// The sentence only. The exact error is kept beside it on the message and
// given to the model if they ask what went wrong, and it is not said
// otherwise -- on a speaker least of all. It used to be spoken in full on
// the voice channel, on the reasoning that there is no "more info" on a
// speaker; what that produced was a butler reading out a DNS failure.
// Asking is the way to it, and asking works because the detail is in the
// conversation.
func (r *Runner) failureSentence(sentence string) string {
	return persona.Addressed(strings.TrimSpace(sentence),
		persona.AddressFor(r.personaID()), "")
}

// lookFirst : What the model is told when it answered without looking.
//
// Worded to be obeyed and then dropped. It does not ask for a tool to
// be called; it asks for the list to be read, which is the step that
// was skipped. An instruction to call something would produce a call
// for the sake of one, and a reminder nobody can comply with is worse
// than none.
const lookFirst = "Before that answer goes out: you did not use any tool this turn. " +
	"Read the tools you have been given and check whether one of them answers this. " +
	"If you were about to say you cannot do something, or that something does not exist, " +
	"that is a claim to check against the list rather than against what you remember -- " +
	"the tools change, and what you could not do last week you may be able to do now. " +
	"If a tool fits, use it. If none does, say the same thing again and it will be sent as it is."

// asUserTurn : The question as a turn, or nothing when there is none.
//
// Added to the history the first time round, because from then on the request
// carries no prompt and the question would otherwise be missing from what the
// model reads.
func asUserTurn(prompt string) []environment.Turn {
	if strings.TrimSpace(prompt) == "" {
		return nil
	}
	return []environment.Turn{{Role: environment.RoleUser, Text: prompt}}
}

// drain : Reads a stream to its end, returning its terminal message.
//
// Always read to completion, whatever arrives: the environment blocks on an
// unread send, so abandoning a stream early leaves its goroutine stuck.
func (r *Runner) drain(
	ctx context.Context,
	t *chat.Chat,
	stream <-chan environment.Message,
	aloud *speech,
	spoke *bool,
) *environment.Message {
	var final *environment.Message
	// One per stream. The model narrating twice inside a single round
	// is describing one piece of work, and saying both would be the
	// same thought twice; between rounds is where a second sentence
	// earns its place.
	saidThisRound := false
	for msg := range stream {
		switch msg.Kind {
		case environment.KindUpdate:
			r.announce(t.ID, msg)
			if !saidThisRound && r.sayAside(ctx, t, aloud, msg.Text) {
				*spoke = true
				saidThisRound = true
			}
		case environment.KindFinal, environment.KindError, environment.KindToolCalls:
			m := msg
			final = &m
		default:
			r.logger.WarnContext(ctx, "environment sent an unknown message kind",
				slog.String("kind", string(msg.Kind)))
		}
	}
	return final
}

// history : Records the question and returns what was said before it.
//
// The question is written first and the history read up to it, which is what
// keeps it from reaching the model twice — once as the last thing said and
// again as the prompt. Neither failure is worth abandoning the chat for: an
// unrecorded question costs the next turn its context, and an unread history
// leaves the prompt to make sense on its own, which it usually does.
func (r *Runner) history(ctx context.Context, t *chat.Chat, systemPrompt string) conversation.Window {
	if t.ConversationID == "" {
		return conversation.Window{}
	}

	asked, err := r.messages.Append(ctx, byChat(t, conversation.Said(t.ConversationID, t.Prompt, t.CreatedAt)))
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot record the question", slog.Any("error", err))
	}

	said, err := r.messages.Before(ctx, t.ConversationID, asked.Seq)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation history", slog.Any("error", err))
		return conversation.Window{}
	}

	// A conversation with no summary yet reads as the zero one, which Plan treats
	// as nothing condensed. Failing to read it costs the turn its oldest
	// context, not the turn itself.
	summary, err := r.messages.Summary(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot read conversation summary", slog.Any("error", err))
	}

	return conversation.Plan(said, summary, r.limitsFor(t.Model, r.alongside(t, systemPrompt)))
}

// toProviderTurns : Converts a conversation's messages into the form a provider
// takes.
func toProviderTurns(messages []conversation.Message) []environment.Turn {
	out := make([]environment.Turn, len(messages))
	for i, m := range messages {
		turn := environment.Turn{
			Role: environment.Role(m.Role),
			Text: m.Content,
		}
		for _, c := range m.ToolCalls {
			turn.ToolCalls = append(turn.ToolCalls, environment.ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: c.Arguments,
			})
		}
		for _, r := range m.ToolResults {
			// The outcome travels with the content rather than in a field of
			// its own, because the wire has nowhere else to put it and the
			// model has to be able to tell a success from a failure. A
			// result that reads as plain text is one the model will report
			// as having worked.
			turn.ToolResults = append(turn.ToolResults, environment.ToolResult{
				ID:      r.ID,
				Content: string(r.Outcome) + ": " + r.Content,
			})
		}
		out[i] = turn
	}
	return out
}

// recordOutcome : Stores what the chat ended up saying, so the next turn in
// the conversation can refer to it.
//
// A failure is recorded too, and shown to the person, but is never given back
// to a model: see conversation.Failure. A turn the person stopped is marked as
// stopped, and that mark is given to a model: see conversation.Interruption.
func (r *Runner) recordOutcome(ctx context.Context, t *chat.Chat) {
	if t.ConversationID == "" {
		return
	}

	said := t.FinishedAt
	if said == nil {
		now := time.Now().UTC()
		said = &now
	}

	var written []conversation.Message
	switch {
	case t.Response != "":
		written = append(written, conversation.Answered(t.ConversationID, t.Response, *said))
	case t.Error != "":
		written = append(written, conversation.Failed(t.ConversationID, t.Error, t.ErrorDetail, *said))
	}

	// Whatever it managed to say, a turn the person stopped is marked as
	// stopped. Partial output is kept rather than replaced: what ran, ran,
	// and once a turn can call tools some of it will have left effects
	// behind that the next turn has to reason about.
	if t.Status == chat.StatusCancelled {
		// Why, when something other than the person stopped it. Read
		// here rather than carried on the chat because the entry is
		// still in the active map: it is removed after execute returns,
		// and this runs inside it.
		why, _ := r.cancelReason(t.ID)
		written = append(written, conversation.Interrupted(t.ConversationID, why, *said))
	}

	for _, m := range written {
		if _, err := r.messages.Append(ctx, byChat(t, m)); err != nil {
			r.logger.ErrorContext(ctx, "cannot record the answer", slog.Any("error", err))
		}
	}
}

// complete : Records a chat's result, failing it instead if the result cannot
// be stored.
func (r *Runner) complete(ctx context.Context, t *chat.Chat, aloud *speech, text string) {
	err := t.Complete(text)
	if errors.Is(err, chat.ErrResponseTooLarge) {
		r.logger.ErrorContext(ctx, "response too large to store",
			slog.Int("bytes", len(text)))
		r.finishWith(ctx, t, aloud, func() error {
			return t.Fail(r.failureSentence("The answer was too long for me to keep."))
		})
		return
	}
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot complete chat", slog.Any("error", err))
		return
	}
	if err := r.save(ctx, t); err != nil {
		return
	}
	r.recordOutcome(ctx, t)
	r.settleAside(ctx, t, aloud)
	r.announceOutcome(t)
	r.logger.InfoContext(ctx, "chat completed",
		slog.Duration("took", t.Duration()),
		slog.Int("response_bytes", len(t.Response)))
}

// finishStopped : Records a chat whose stream ended without a result, which
// happens when it was cancelled or outlived its deadline.
//
// The turn's speech is passed so that what it was about to say can be
// abandoned. A cancelled turn is usually one the person interrupted by
// speaking again, and its narration carrying on over the answer to their
// new question is the clearest way to sound like a machine talking to
// itself.
func (r *Runner) finishStopped(ctx context.Context, t *chat.Chat, aloud *speech, runErr error) {
	if errors.Is(runErr, context.DeadlineExceeded) {
		r.logger.WarnContext(ctx, "chat exceeded its deadline",
			slog.Duration("timeout", r.chatTimeout))
		r.finishWith(ctx, t, aloud, func() error { return t.Fail(r.failureSentence(timeoutReason)) })
		return
	}

	// The server going down is a failure: the turn was going to work and
	// the machine took it away. Anything else that stopped a turn is a
	// cancellation -- it did not finish, and nothing went wrong with it.
	// The reason is not lost either way; recordOutcome writes it into the
	// transcript.
	reason, _ := r.cancelReason(t.ID)
	if reason == shutdownReason {
		r.logger.InfoContext(ctx, "chat stopped", slog.String("reason", reason))
		r.finishWith(ctx, t, aloud, func() error { return t.Fail(reason) })
		return
	}

	r.logger.InfoContext(ctx, "chat cancelled", slog.String("reason", reason))
	r.finishWith(ctx, t, aloud, func() error { return t.Cancel() })
}

// finishWith : Applies a terminal transition and stores the result.
func (r *Runner) finishWith(ctx context.Context, t *chat.Chat, aloud *speech, transition func() error) {
	if err := transition(); err != nil {
		r.logger.ErrorContext(ctx, "cannot finish chat", slog.Any("error", err))
		return
	}

	// A turn that did not succeed stops narrating. The asides describe
	// work that was about to be done, they are queued a second and a half
	// apart, and most of a turn's asides are still waiting their turn when
	// it goes wrong. Left running, they announce the work after it has
	// already failed and the apology arrives behind all of them: one
	// failed turn said "creating that list" seven seconds after it had
	// given up on creating the list.
	if aloud != nil && (t.Status == chat.StatusFailed || t.Status == chat.StatusCancelled) {
		aloud.stop()
	}

	_ = r.save(ctx, t)
	r.recordOutcome(ctx, t)
	r.settleAside(ctx, t, aloud)
	r.announceOutcome(t)
}

// announce : Reports where a chat has got to, without storing it.
//
// Progress is transient by nature: it is worth hearing while the answer is
// being produced and worth nothing afterwards. It used to be written to a
// table so a dropped stream could replay it, and that table went with the
// stream.
func (r *Runner) announce(chatID string, msg environment.Message) {
	r.publish(events.Event{
		ChatID: chatID,
		Kind:   events.KindUpdate,
		Text:   msg.Text,
		At:     msg.At,
	})
}

// publish : Announces an event, if there is anywhere to announce it.
func (r *Runner) publish(ev events.Event) {
	if r.publisher == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	r.publisher.Publish(ev)
}

// announceOutcome : Tells listeners how a chat ended, so a stream can close
// rather than waiting for a message that will never come.
func (r *Runner) announceOutcome(t *chat.Chat) {
	ev := events.Event{ChatID: t.ID, At: t.UpdatedAt}
	switch t.Status {
	case chat.StatusCompleted:
		ev.Kind, ev.Text = events.KindFinal, t.Response
	case chat.StatusFailed:
		ev.Kind, ev.Text = events.KindError, t.Error
	case chat.StatusCancelled:
		ev.Kind, ev.Text = events.KindCancelled, ""
	default:
		return
	}
	r.publish(ev)
}

// save : Writes a chat's current state, using a context that outlives the
// run so that a cancelled chat can still record having been cancelled.
func (r *Runner) save(ctx context.Context, t *chat.Chat) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := r.repo.Update(writeCtx, t); err != nil {
		r.logger.ErrorContext(ctx, "cannot store chat",
			slog.String("status", string(t.Status)),
			slog.Any("error", err))
		return err
	}
	return nil
}
