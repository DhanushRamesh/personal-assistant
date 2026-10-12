package runner

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/app/tool"
	"personal-assistant/internal/base/platform/failure"
	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
	"personal-assistant/internal/base/thinking/environment"
	"personal-assistant/internal/base/world/events"
	"personal-assistant/internal/domain/persona"
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
		r.changed(ctx, t.ConversationID)
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
	r.changed(ctx, t.ConversationID)

	// Composed once. It reads the database and searches memory, and every
	// hop of the tool loop must be given the same one.
	systemPrompt, missed := r.promptFor(ctx, t)

	r.consume(runCtx, ctx, t, systemPrompt)
	r.changed(ctx, t.ConversationID)

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

	// The clock goes after everything that was said, not up in the
	// system prompt beside the manner.
	//
	// Measured against his own 140-message conversation on 11 October
	// 2026, asked the time four ways: from the system prompt, whether
	// placed first or last in it, it answered between 12:55 and 12:57
	// pm every single time while it was 8:04 pm -- a time it had said
	// itself, hours earlier, in that conversation. Given here instead,
	// after the history and before the question, it was right four
	// times out of four. The wording did not change; only where it
	// sits relative to the times it has to beat.
	//
	// Forty invented turns over one hour did not reproduce any of it.
	// Both positions passed. Only his real conversation failed.
	turns = append(turns, r.clock()...)

	prompt := t.Prompt

	// What the tools this turn ran have obliged the answer to mention.
	// Gathered as they run and settled once, at the end, because the
	// model may take several rounds to get to its answer.
	var owed []tool.Owed

	// What this turn tried to change and what it managed, so an answer
	// cannot report a change the server knows did not happen.
	var wrote writing

	// Whether the model said aloud what it was about to do. Once per turn
	// at most: a chain of five rounds would otherwise interrupt five times
	// to describe work the person did not ask about.
	var spoke bool

	// Every tool that has run in this turn, so a write can insist its own
	// domain was read first. Prefetched listings count: the model was
	// shown them before it was asked anything.
	ran := r.alreadyRead(t)

	// Whether the model has already been sent back once for giving up
	// on a change it was asked to make.
	var nudged bool
	// Whether the answer has already been sent back for naming its
	// machinery, so it is asked once and not in a circle.
	var plainly bool
	// Whether any of the answer has already reached them.
	//
	// returnable enumerates the reasons a round may still be sent
	// back, and a round that cannot be is read aloud as it arrives.
	// The machinery check added a reason it did not know about, so
	// an answer was spoken and then sent away to be written again --
	// and the owner heard one answer and read another. His words:
	// "I got the response as 'I am afraid..' but before that jarvis
	// was speaking."
	//
	// The warning was already in drain, two lines from where this
	// broke it: "what a listener has heard cannot be unheard."
	//
	// So the check now applies only where it can still change what
	// they get. On a round already spoken the word reaches them,
	// which is what happened before the check existed, and is better
	// than hearing one answer and reading a different one.
	var published bool

	// A round begun before the one in front of it had finished, and
	// the one that has been accepted for the next turn of the loop.
	//
	// Two of them rather than one, because a round begun early is
	// only ever right for the send-back. If the round it was begun
	// beside goes on to call a tool, the history changes underneath
	// it and what it holds is the answer to a question nobody is
	// asking any more. So it is dropped at the top of every turn
	// unless it has been accepted. See ahead.
	var waiting, accepted <-chan environment.Message
	var stopWaiting, stopAccepted context.CancelFunc
	// Which of the chat's calls each of them is, for its timeline.
	waitingCall, acceptedCall := -1, -1
	drop := func() {
		if stopWaiting != nil {
			stopWaiting()
		}
		t.Ended(waitingCall, chat.CallUnused, time.Now())
		waiting, stopWaiting, waitingCall = nil, nil, -1
	}
	defer func() {
		drop()
		if stopAccepted != nil {
			stopAccepted()
		}
		t.Ended(acceptedCall, chat.CallUnused, time.Now())
	}()

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

	// revealed : The deferred tools this conversation has been told how
	// to call, which are described in full rather than named.
	//
	// Read back out of the history rather than started empty. Started
	// empty it lasted one chat, and a chat is one thing the person
	// said: asked four times about the same cinema booking, the model
	// called tool_describe for calendar_update four separate times,
	// each one preceded by a guessed call that had to be refused and
	// each one costing a round somebody was waiting through. The
	// result it was handed said "callable from here on", and that was
	// true for about forty seconds.
	revealed := r.alreadyDescribed(window.Messages)

	// worked : Rounds that got something done, as against rounds the
	// server spent correcting the model.
	//
	// Only these are counted against the budget. A turn asking for one
	// task list was refused for answering without looking, spent a round
	// being told how to call a deferred tool, was refused again for
	// writing before reading -- and then, on the round where it finally
	// had what it needed, the tools were taken away because four rounds
	// had passed. Every guard was right and the work still did not
	// happen.
	//
	// A correction is the server teaching, not the model wandering, and
	// charging the model for being taught is how a turn runs out of
	// rounds without ever having had one.
	worked := 0

	for hop := 0; ; hop++ {
		// Rounds are still bounded, in case a chain learns nothing and
		// goes round for ever: the correcting kind are bounded by this
		// and the useful kind by MaxToolHops.
		if hop >= MaxRounds {
			r.logger.WarnContext(ctx, "the tool chain went round too many times",
				slog.Int("rounds", hop), slog.Int("worked", worked))
			worked = MaxToolHops
		}

		// The last round is offered nothing. A model that has run out of
		// rounds must answer from what it gathered, and saying what it
		// managed is better than being cut off mid-chain with nothing to
		// show for the work that already ran.
		tools := r.offered(t, revealed)
		if worked >= MaxToolHops-1 {
			if len(tools) > 0 {
				r.logger.WarnContext(ctx, "the tool chain ran long, so the last round is asked without tools",
					slog.Int("hops", hop), slog.Int("worked", worked))
				// And told why, or it has nothing it can say. The round
				// before this one often ends in a tool result that asks
				// for another call -- read this, then write again -- and
				// a model handed that instruction and no tools has been
				// told to do something and given no way to do it. Asked
				// like that it has returned nothing at all, and an empty
				// reply is a failed turn.
				prompt = lastRound
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

		// The round begun early, if the one before it turned out to
		// need it. Taken as though it had just been asked for.
		stream, err := accepted, error(nil)
		spent, call := stopAccepted, acceptedCall
		accepted, stopAccepted, acceptedCall = nil, nil, -1
		// Anything begun early and not accepted is stale: the round
		// beside it called a tool, so the history it was asked
		// against no longer exists.
		drop()
		if stream == nil {
			call = t.Asking(time.Now(), false)
			stream, err = r.environment.Run(runCtx, environment.Request{
				Prompt:  prompt,
				History: turns,
				Summary: window.Summary,
				Vendor:  t.Model.Vendor,
				Model:   t.Model.ID,
				Tools:   tools,

				SystemPrompt: asking,
			})
		}
		if err != nil {
			t.Ended(call, chat.CallFailed, time.Now())
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

		// Whether this round's answer could still be sent back, which
		// decides both whether it may be spoken and whether the round
		// after it is begun now.
		sendable := returnable(reached, pressed, nudged, wrote, tools)
		if sendable {
			waiting, stopWaiting, waitingCall = r.ahead(runCtx, t, window, turns, prompt, asking, tools)
		}
		// Whether any of this turn's answer has already reached
		// them. A round read aloud cannot be taken back, so nothing
		// after this point may send it away to be rewritten.
		if !sendable {
			published = true
		}
		final := r.drain(ctx, t, stream, call, aloud, &spoke, !sendable, nil)
		if spent != nil {
			// Finished with. This only lets its context go.
			spent()
		}

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
				// What was thrown away, which nothing recorded.
				//
				// Owner, having heard something before the answer:
				// "before saying it, it said 'I cannot answer'."
				// The turn's own log had the aside a tool says while
				// it runs, and the answer this discards, and no way
				// to tell which of the two reached him -- its text
				// was gone and the two moments were a tenth of a
				// second apart.
				r.logger.InfoContext(ctx, "an answer was sent back to look at the tools",
					slog.Bool("already_running", waiting != nil),
					slog.Bool("spoken", published),
					slog.String("thrown_away", first(final.Text, 120)))
				pressed = true
				// Begun while this answer was still being written, if
				// it was. This is the only place it is right to use.
				accepted, stopAccepted, acceptedCall = waiting, stopWaiting, waitingCall
				waiting, stopWaiting, waitingCall = nil, nil, -1
				turns = append(turns, asUserTurn(prompt)...)
				prompt = lookFirst
				continue
			}
			// Asked to change something, changed nothing, and now
			// answering. Sent back once, with the refusal and
			// whatever it was handed still in front of it.
			//
			// The denial below is the safety net; this is the repair.
			// Told "nothing has been changed, here is what is
			// actually there, call it again with an identifier from
			// this", the model answered "Done, sir. This conversation
			// is now called name test." The instruction was fifteen
			// lines long and the last thing it read; it simply
			// stopped. One more round costs a second and usually
			// finishes the job the person asked for.
			if !wrote.claimable() && !nudged && len(tools) > 0 {
				r.logger.InfoContext(ctx, "an abandoned change was sent back",
					slog.Int("writes_tried", wrote.tried))
				nudged = true
				turns = append(turns, asUserTurn(prompt)...)
				prompt = finishIt
				continue
			}

			// Named its own machinery, so it is asked once to say
			// it as a person would.
			//
			// persona.machinery forbids the word outright and the
			// manner has forbidden it in three other ways since it
			// was written. Measured: the ban went live on 11
			// October at 23:25 and on the 12th at 22:27 he was
			// told "There is no tool I have that controls whether
			// you shower", with the rule in the running binary.
			//
			// The eval that passed it asked the model with no
			// tools offered. A real turn carries sixty of them and
			// the model is deep in thinking about them, which is
			// exactly when it reaches for the word. An instruction
			// has to beat that; a check afterwards does not have
			// to.
			//
			// One round, the same way an abandoned change is sent
			// back above, and only on the answers that need it.
			//
			// The first version of what it is told turned answers
			// into refusals. It said "say the same thing as a
			// limit of your own -- I am afraid I cannot do that,
			// sir -- in one sentence", which is right for a
			// refusal that named a tool and wrong for everything
			// else. "I understand, sir. Before I say I cannot do
			// something, I will check both the tools I have..."
			// came back as "I am afraid I cannot do that, sir."
			// Owner: "before this jarvis recorded a long para, but
			// only this is on the reply."
			if !plainly && !published && persona.NamesMachinery(final.Text) && len(tools) > 0 {
				r.logger.InfoContext(ctx, "the answer named its own machinery",
					slog.String("said", first(final.Text, 90)))
				plainly = true
				turns = append(turns, asUserTurn(prompt)...)
				prompt = sayItPlainly
				continue
			}

			address := persona.AddressFor(r.personaID())
			answer := tool.Ensure(final.Text, address, tool.Merged(owed))
			// A turn that reached for a write and never landed one
			// may not end as though it had. The model has been seen
			// to describe the change it was refused.
			if !wrote.claimable() {
				r.logger.WarnContext(ctx, "the answer is denied a change that did not happen",
					slog.Int("writes_tried", wrote.tried))
				answer = tool.Unchanged(answer, address)
			}
			r.complete(ctx, t, aloud, answer)
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
		//
		// Never onto a voice stream. Home Assistant speaks
		// everything written to it, so a narration put there is
		// recited whether or not anybody asked for one -- which is
		// how "starting a new conversation" came to be said in front
		// of "a new conversation has been created, sir". Aloud, the
		// narration belongs to the aside player, which is off by
		// default and which settleAside sequences against the
		// answer. On a screen it is a status line and is never
		// spoken, so it goes through as before.
		if said != "" && t.Channel != chat.ChannelVoice {
			r.announce(t.ID, environment.Update(said))
		}
		if r.sayAside(ctx, t, aloud, said) {
			spoke = true
		}

		// Whether this round got anything done, measured by what
		// actually ran rather than by what was asked for. runTools adds
		// a tool to ran only when it did not fail, so a round whose
		// calls were all refused adds nothing and is not charged.
		before := len(ran)

		ranTurns, ranOwed, reveal, read := r.runTools(ctx, t, final.ToolCalls, &ran, revealed, &wrote)
		if did(ran[before:]) {
			worked++
		}
		// Counted as read only after the round has been judged. A
		// listing the server fetched while refusing a write is the
		// server correcting the model, and charging the turn for it
		// is the starvation the budget was changed to avoid.
		ran = append(ran, read...)
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

// did : Whether a round got something done.
//
// Anything that ran and was not tool_describe. Describing a tool is the
// deferring mechanism costing a round, not the turn making progress --
// the model knows no more about the person afterwards than it did
// before, and charging the turn for it is charging it for a saving made
// somewhere else.
func did(ran []string) bool {
	for _, name := range ran {
		if name != tool.DescribeName {
			return true
		}
	}
	return false
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
	"Read the list of tools that exist but have not been described to you as well -- " +
	"those are usable, they are simply waiting to be asked about, and a tool you have " +
	"not been handed the arguments for is not a tool you do not have. " +
	"If one of them fits, call tool_describe with its name and then call it. " +
	"If you were about to say you cannot do something, or that something does not exist, " +
	"that is a claim to check against both lists rather than against what you remember -- " +
	"the tools change, and what you could not do last week you may be able to do now. " +
	"If a tool fits, use it. If none does, say the same thing again and it will be sent as it is."

// sayItPlainly : What the model is told when it named its machinery.
//
// Not a reminder of the rule. It has the rule and broke it. This
// says what to write instead, which is the thing it was missing.
const sayItPlainly = "Before that answer goes out: it names your own machinery, which they " +
	"never hear about. Say it again without that, and change nothing else. " +
	"Everything you were telling them stays -- every fact, every figure, the whole of the " +
	"answer and its length. The only thing being taken out is the part about what you are " +
	"built from: the word \"tool\", the name of one, and what you do or do not have. " +
	"An answer stays an answer. Only if it was already a refusal is one sentence enough, " +
	"and then say it as a limit of your own -- \"I am afraid I cannot do that, sir\". " +
	"What you must not do is turn an answer into a refusal because this was said to you."

// first : The opening of a string, for a log line.
func first(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// finishIt : What the model is told when it gave up on a change.
//
// Not an accusation and not a new instruction: the instruction was in
// the tool result it already has. This only says that the thing it was
// asked to do has not happened, and that saying it has is the one
// answer it may not give.
const finishIt = "Before that answer goes out: you asked for something to be changed and " +
	"nothing was changed. The tool result above says what to do about it -- read it again and " +
	"do that now. If you genuinely cannot, say plainly that it did not happen and why. " +
	"What you must not do is describe the change as though it were made."

// lastRound : What the model is told when its tools are taken away.
//
// It is not being asked to try again. It is being asked to report,
// which is a thing it can do with nothing: say what was found, say
// what was not done, and leave the person knowing where it got to.
//
// Worded to stop the one failure this round actually has. Handed a
// tool result saying "read this and then call that again" and then
// offered no tools, it has returned an empty reply -- which the person
// sees as the assistant breaking rather than as it running out of
// room.
const lastRound = "This turn has used all the rounds of tools it is allowed, so there are none for this one. " +
	"Do not ask for a tool and do not say you are about to do something: nothing further will run. " +
	"Answer now from what you already found. " +
	"If part of what they asked was done, say what was done. " +
	"If something was not done, say plainly that you did not get to it and what is left, " +
	"in the ordinary way you would tell somebody you ran out of time -- not as an error, and without " +
	"describing rounds, tools or limits, which are yours and not theirs. " +
	"Say something: an empty answer reaches them as the assistant having broken."

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
	// call : Which of the chat's calls the stream answers, stamped as it
	// is heard and when it ends.
	call int,
	aloud *speech,
	spoke *bool,
	// speak : Whether this round's pieces may reach the person. A
	// round whose answer could still be sent back must be read in
	// silence.
	speak bool,
	// began : Told the first time text arrives rather than a tool
	// call, which is the earliest anything knows which sort of round
	// this is. Nil when nobody is waiting on that.
	began func(),
) *environment.Message {
	var final *environment.Message
	// One per stream. The model narrating twice inside a single round
	// is describing one piece of work, and saying both would be the
	// same thought twice; between rounds is where a second sentence
	// earns its place.
	saidThisRound := false
	for msg := range stream {
		t.Heard(call, time.Now())
		switch msg.Kind {
		case environment.KindUpdate:
			r.announce(t.ID, msg)
			if !saidThisRound && r.sayAside(ctx, t, aloud, msg.Text) {
				*spoke = true
				saidThisRound = true
			}
		case environment.KindPartial:
			// Text rather than a tool call, which is the earliest
			// anything can know which sort of round this is. Told
			// once, and only the first time.
			if began != nil {
				began()
				began = nil
			}
			if !speak {
				// This round's answer may still be sent back, and
				// what a listener has heard cannot be unheard. The
				// pieces are still read, because reading them is how
				// the round is recognised at all.
				continue
			}
			// When the person stops waiting, which is not when the
			// turn ends. Stamped here rather than worked out later,
			// because the answer is written down once and whole and
			// the moment it started is not in it. Stored with the
			// chat at completion, so it costs no write of its own.
			t.Begun(msg.At)
			// Straight through, and never spoken beside the turn.
			// This is the answer arriving in pieces, not a sentence
			// about the work: narrating it would read the answer out
			// twice, once in fragments and once whole.
			r.publish(events.Event{
				ChatID: t.ID,
				Kind:   events.KindPartial,
				Text:   msg.Text,
				At:     msg.At,
			})
		case environment.KindFinal, environment.KindError, environment.KindToolCalls:
			m := msg
			final = &m
			t.Ended(call, callEnd(msg.Kind), time.Now())
		default:
			r.logger.WarnContext(ctx, "environment sent an unknown message kind",
				slog.String("kind", string(msg.Kind)))
		}
	}
	t.Ended(call, chat.CallStopped, time.Now())
	return final
}

// callEnd : How a call ended, from the message that ended it.
func callEnd(kind environment.Kind) chat.CallEnd {
	switch kind {
	case environment.KindToolCalls:
		return chat.CallTools
	case environment.KindError:
		return chat.CallFailed
	default:
		return chat.CallAnswered
	}
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

// alreadyDescribed : The deferred tools this conversation has already
// been told how to call.
//
// Two things reveal a tool, and both leave a tool call in the history:
// tool_describe names them in its arguments, and a tool called before
// it was described is described afterwards so the next round can call
// it properly. So the history is the record, and no state has to be
// kept anywhere for it.
//
// Read from the window rather than the whole conversation, which
// bounds it for free. A tool last mentioned far enough back to have
// been condensed away is not what this conversation is doing any more,
// and the request stops carrying its description.
func (r *Runner) alreadyDescribed(messages []conversation.Message) map[string]bool {
	revealed := map[string]bool{}
	if r.tools == nil {
		return revealed
	}

	add := func(name string) {
		// Through the registry, so a name the model invented does not
		// become an entry that nothing will ever match.
		if _, ok := r.tools.Get(name); ok {
			revealed[name] = true
		}
	}
	for _, m := range messages {
		for _, c := range m.ToolCalls {
			if c.Name == tool.DescribeName {
				for _, name := range tool.Described(c.Arguments) {
					add(name)
				}
				continue
			}
			add(c.Name)
		}
	}
	return revealed
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

	// A turn that is over stops narrating, however it ended. The
	// asides describe work that was about to be done, they are queued
	// a second and a half apart, and most of a turn's asides are
	// still waiting their turn when it finishes.
	//
	// It was only failures that stopped them, because left running
	// they announce work after it has already gone wrong and the
	// apology arrives behind all of them: one failed turn said
	// "creating that list" seven seconds after giving up on it. A
	// turn that succeeded is the same waste without the insult --
	// narration of work that is done, in front of the answer to it,
	// which the answer then waits out.
	if aloud != nil {
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

// returnable : Whether an answer given on this round could still be sent
// back for another one.
//
// The two guards below both end in another round, and each may fire
// once. Everything they test is known before the model is asked: a
// round that answers without tool calls leaves the write tally exactly
// as it found it.
//
// It is asked before the call because of what it decides -- whether the
// answer may be spoken as it is written. A listener cannot unhear the
// answer that was sent back, and would hear the one that replaced it as
// the same thing said twice.
func returnable(reached, pressed, nudged bool, wrote writing, tools []environment.ToolSpec) bool {
	if len(tools) == 0 {
		return false
	}
	return (!reached && !pressed) || (!wrote.claimable() && !nudged)
}

// ahead : Starts the round the send-back would have asked for, at
// the same moment as the round in front of it.
//
// The send-back is right about one turn in four -- 90 of 363
// measured -- and the other three cost three to five seconds for an
// answer barely different from the one thrown away. On a plain
// "good morning" it doubled the turn to seven and a half seconds.
//
// It cannot be prevented: the caution it applies was moved into the
// prompt twice and made the tool suite worse both times, because it
// fires on something that cannot be known in advance. So it is paid
// for in parallel instead of in series.
//
// Waiting for the first fragment of prose was tried first, on the
// grounds that a streamed tool call arrives before any text and so
// the first text is proof of a send-back. True, and useless: on a
// short answer the gateway delivers the whole thing in one burst,
// and the first fragment arrived three tenths of a millisecond
// before the round ended. There was no window to start anything in.
//
// So it starts at once, whenever the send-back could fire at all.
// That spends a round on the three turns in four where the guard
// changes nothing. The owner's order was accuracy first, latency
// second, cost not a concern, and this is the shape of that: the
// turn costs the longer of two rounds instead of the sum.
//
// What is started here is exactly what the next turn of the loop
// would have built, so the loop takes it as though it had just asked
// for it. Started in silence: it may be thrown away, and what a
// listener has heard cannot be unheard.
func (r *Runner) ahead(
	runCtx context.Context,
	t *chat.Chat,
	window conversation.Window,
	turns []environment.Turn,
	prompt, asking string,
	tools []environment.ToolSpec,
) (<-chan environment.Message, context.CancelFunc, int) {
	ctx, cancel := context.WithCancel(runCtx)
	call := t.Asking(time.Now(), true)
	stream, err := r.environment.Run(ctx, environment.Request{
		Prompt:  lookFirst,
		History: append(append([]environment.Turn{}, turns...), asUserTurn(prompt)...),
		Summary: window.Summary,
		Vendor:  t.Model.Vendor,
		Model:   t.Model.ID,
		Tools:   tools,

		SystemPrompt: asking,
	})
	if err != nil {
		// Nothing is lost. The loop asks for the same thing itself a
		// moment later and reports the failure then, in the one place
		// that knows how to fail a chat properly.
		cancel()
		t.Ended(call, chat.CallFailed, time.Now())
		return nil, nil, -1
	}
	return stream, cancel, call
}
