package runner

import (
	"context"
	"log/slog"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/environment"
	"github.com/DhanushRamesh/personal-assistant/internal/persona"
	"github.com/DhanushRamesh/personal-assistant/internal/prompt"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// MaxToolHops : How many rounds of tool calls one question may take.
//
// A model that keeps calling tools has to be stopped by something. The last
// round is asked again with no tools offered, so the answer is composed from
// what was actually gathered rather than cut off: the person gets a reply
// either way, and the model has to say what it managed rather than what it
// intended.
const MaxToolHops = 5

// MaxRounds : How many rounds one question may take altogether,
// correcting rounds included.
//
// MaxToolHops bounds the rounds that got something done. This bounds
// the rest, so a chain that learns nothing from being corrected still
// ends: without it, a model that answers without looking every single
// time would be sent back for ever.
//
// Comfortably above MaxToolHops, because the corrections are the
// server's own doing -- being sent back to look, being told how to call
// a deferred tool, being refused for writing before reading -- and a
// turn should be able to absorb several of them and still do the work
// it was asked for.
const MaxRounds = 9

// offered : The tools this chat may reach, in the shape an environment takes.
//
// The first of the two gates. A tool missing from here is never described to
// the model, so a model cannot call what it has not been told exists. The
// registry checks again when one is actually called, because this is a
// prompt and a prompt is not a boundary.
func (r *Runner) offered(t *chat.Chat, revealed map[string]bool) []environment.ToolSpec {
	if r.tools == nil {
		return nil
	}

	address := persona.AddressFor(r.personaID())
	// The hot ones and whatever has been asked about, not everything.
	// Describing all of them was 72% of a request and the endpoint
	// caches nothing, so the rest are named in the prompt instead and
	// described when the model asks.
	reachable := r.tools.Offered(t.Channel, revealed)
	out := make([]environment.ToolSpec, 0, len(reachable))
	for _, x := range reachable {
		schema, err := tool.Narrated(x.Params, address).MarshalJSON()
		if err != nil {
			// Unreachable: the schema is a typed structure of strings. A tool
			// whose schema will not render is left out rather than offered
			// without one, since a tool with no arguments described is a tool
			// called with guessed arguments.
			r.logger.Error("a tool's schema will not render, so it is not offered",
				slog.String("tool", x.Name), slog.Any("error", err))
			continue
		}
		out = append(out, environment.ToolSpec{
			Name:        x.Name,
			Description: tool.NarratedDescription(x.Description(), address),
			Parameters:  schema,
		})
	}
	return out
}

// withheld : What to say when a tool was called before it was
// described.
//
// Short, and that is the point. The first version returned the tool's
// whole description and schema here, which was both redundant and
// expensive: revealing it puts the real thing in the next request's
// tool list, so this was a second copy -- about fifteen hundred
// tokens of it -- and a tool result is a stored message, so the copy
// was then re-sent with every later turn of that conversation.
//
// Saying it is now available is enough. The model finds it described
// properly where tools are described.
func withheld(tools *tool.Registry, name string) tool.Result {
	if _, ok := tools.Get(name); !ok {
		return tool.Failed("There is no tool called " + name + ".")
	}
	return tool.Result{
		Outcome: conversation.OutcomePartial,
		Content: name + " was not run: you had not been given its arguments, so the ones " +
			"in that call were guessed. It is described to you now -- call it again, " +
			"with the arguments it actually takes.",
		Reveal: []string{name},
	}
}

// asking : The system prompt for one round, with the names of the tools
// that round is not describing.
//
// Unchanged when there are none, so nothing is said about a mechanism
// that is not in use.
func (r *Runner) asking(systemPrompt string, t *chat.Chat, revealed map[string]bool) string {
	if r.tools == nil {
		return systemPrompt
	}
	catalogue := r.tools.Catalogue(t.Channel, revealed)
	if catalogue == "" {
		return systemPrompt
	}
	return prompt.Block(systemPrompt, catalogue)
}

// writing : What a chat tried to change, and what it managed.
//
// Counted for the whole chat rather than for one round, because a write
// refused and then done properly in the next round is a change made:
// that is the correcting machinery working, not a turn that failed.
type writing struct {
	// tried : Calls to a tool that creates, changes or removes
	// something, whether or not the call was allowed to run.
	tried int
	// took : Those that ran and did not fail. A write that ran and
	// found the value already correct counts: the state is what was
	// asked for, which is all the person cares about.
	took int
}

// claimable : Whether the answer may report that something changed.
func (w writing) claimable() bool { return w.tried == 0 || w.took > 0 }

// runTools : Runs what the model asked for and records both halves.
//
// The call and the answer are both written to the conversation before the
// model is asked again. A chain cut in the middle -- by a restart, a
// deadline, or the person saying stop -- then still reads as what was done
// rather than as a question nobody answered.
func (r *Runner) runTools(
	ctx context.Context,
	t *chat.Chat,
	calls []environment.ToolCall,
	seen *[]string,
	revealed map[string]bool,
	wrote *writing,
) ([]environment.Turn, []tool.Owed, []string) {
	asked := make([]conversation.ToolCall, 0, len(calls))
	for _, c := range calls {
		asked = append(asked, conversation.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments})
	}
	call := conversation.CalledTools(t.ConversationID, asked, time.Now().UTC())
	r.remember(ctx, t, call)

	caller := r.callerFor(ctx, t)

	got := make([]conversation.ToolResult, 0, len(calls))
	ran := make([]tool.Result, 0, len(calls))
	for _, c := range calls {
		started := time.Now()

		// Whether this call was going to change something. Counted
		// before the branch below, so a write refused for never having
		// been described still counts as having been tried.
		x, known := r.tools.Get(c.Name)
		if known && x.Writes {
			wrote.tried++
		}

		// A tool the model was not given, called anyway. Being left
		// out of the request does not stop it: the endpoint forwards
		// the call, and what arrives is built from a name and a guess
		// at the arguments.
		//
		// So it is not run. The arguments are handed over instead --
		// the same words tool_describe would have given -- and it is
		// described from the next round on, which costs the round the
		// model should have spent asking. Not running it also matters
		// for a write: a guess that happens to validate would act.
		if r.tools.Withheld(t.Channel, revealed, c.Name) {
			result := withheld(r.tools, c.Name)
			r.logger.InfoContext(ctx, "a tool was called before it was described",
				slog.String("tool", c.Name))
			ran = append(ran, result)
			got = append(got, conversation.ToolResult{
				ID: c.ID, Name: c.Name, Outcome: result.Outcome,
				Content: result.Content, TookMS: time.Since(started).Milliseconds(),
			})
			continue
		}

		result := r.tools.Call(ctx, c.Name, tool.Invocation{
			Caller: caller,
			Args:   []byte(c.Arguments),
			Ran:    *seen,
		})

		// Recorded whatever it returned. A listing that failed did not
		// show the model anything, but one that ran did, and a write may
		// lean on it however the model phrased its question.
		if result.Outcome != conversation.OutcomeFailed {
			*seen = append(*seen, c.Name)
			if known && x.Writes {
				wrote.took++
			}
		}

		// A listing the server ran while refusing a write counts as
		// read, or the model is handed what is there and then refused
		// again for not having fetched it itself.
		*seen = append(*seen, result.Read...)

		r.logger.InfoContext(ctx, "tool ran",
			slog.String("tool", c.Name),
			slog.String("outcome", string(result.Outcome)),
			slog.Duration("took", time.Since(started)))

		ran = append(ran, result)
		got = append(got, conversation.ToolResult{
			ID:      c.ID,
			Name:    c.Name,
			Outcome: result.Outcome,
			Content: result.Content,
			TookMS:  time.Since(started).Milliseconds(),
		})
	}

	results := conversation.ToolsReturned(t.ConversationID, got, time.Now().UTC())
	r.remember(ctx, t, results)

	// What tool_describe handed over the arguments for. It has to be
	// described on the next request or the model cannot call it.
	var reveal []string
	for _, x := range ran {
		reveal = append(reveal, x.Reveal...)
	}

	return toProviderTurns([]conversation.Message{call, results}), tool.Owing(ran), reveal
}

// callerFor : Who a tool is acting for.
//
// The user comes from the conversation rather than from the chat, which does
// not carry one. A tool given no user acts for nobody and every ownership
// check refuses it, which is the safe direction: a tool that cannot tell
// whose data it is looking at should not be looking at it.
func (r *Runner) callerFor(ctx context.Context, t *chat.Chat) tool.Caller {
	caller := tool.Caller{
		ClientID:       t.ClientID,
		ConversationID: t.ConversationID,
		Channel:        t.Channel,
	}

	if t.ConversationID == "" || r.repo == nil {
		return caller
	}
	c, err := r.repo.GetConversation(ctx, t.ConversationID)
	if err != nil {
		r.logger.ErrorContext(ctx, "cannot tell whose conversation this is",
			slog.String("conversation_id", t.ConversationID), slog.Any("error", err))
		return caller
	}
	caller.UserID = c.UserID
	return caller
}

// remember : Writes a message to the conversation, logging a failure rather
// than abandoning the turn.
//
// A tool that ran and was not recorded is worse than one that did not run:
// the effects are in the world and the transcript denies them. It is still
// not a reason to fail the answer, since failing it would leave the person
// with nothing and the effects still there.
func (r *Runner) remember(ctx context.Context, t *chat.Chat, m conversation.Message) {
	if t.ConversationID == "" {
		return
	}

	m = byChat(t, m)
	if _, err := r.messages.Append(ctx, m); err != nil {
		r.logger.ErrorContext(ctx, "cannot record what the tools did",
			slog.String("role", string(m.Role)), slog.Any("error", err))
	}
}

// byChat : The message, stamped with the turn that wrote it.
//
// Every message the runner stores goes through this. Stamping at each call
// site instead would work until somebody added a fourth one, and a missing
// stamp does not fail: it produces an answer whose timeline is silently
// short.
func byChat(t *chat.Chat, m conversation.Message) conversation.Message {
	m.ChatID = t.ID
	return m
}
