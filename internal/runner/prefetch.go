package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
	"github.com/DhanushRamesh/personal-assistant/internal/tool"
)

// prefetch : Runs the tools that ask to go first, and returns them as
// turns the model reads before it answers.
//
// One generic thing, for every domain, forever. The alternative was a
// block of bespoke code in the system prompt per domain -- there were
// three of them for reminders alone -- which does not survive a fifth
// domain, let alone a twentieth. A new domain now sets Prefetch on its
// listing and nothing here changes.
//
// The result is given to the model as a call it made and an answer it
// got, in this turn. That matters: a tool result from this turn is
// exactly what the rule about not answering from memory asks for, so
// the model is holding a fresh, legitimate reading of every domain
// before it reads the question.
//
// Failures are dropped rather than reported. A prefetch nobody asked
// for that cannot run must not fail a turn, and the model still has
// the tool if it wants to try itself.
func (r *Runner) prefetch(ctx context.Context, t *chat.Chat, note *chat.Recalled) string {
	if r.tools == nil {
		return ""
	}
	wanted := r.tools.Prefetched(t.Channel)
	if len(wanted) == 0 {
		return ""
	}

	caller := r.callerFor(ctx, t)
	if caller.UserID == "" {
		return ""
	}

	var b strings.Builder

	for _, x := range wanted {
		started := time.Now()

		result, cached := r.prefetched(caller.UserID, x)
		if !cached {
			result = r.tools.Call(ctx, x.Name, tool.Invocation{
				Caller: caller,
				Args:   json.RawMessage("{}"),
			})
			r.keep(caller.UserID, x, result)
		}
		if result.Outcome == conversation.OutcomeFailed {
			r.logger.WarnContext(ctx, "a prefetched tool would not run",
				slog.String("tool", x.Name), slog.String("why", result.Content))
			continue
		}

		took := time.Since(started)
		r.logger.InfoContext(ctx, "prefetched",
			slog.String("tool", x.Name),
			slog.Bool("cached", cached),
			slog.Duration("took", took))

		if note != nil {
			note.Tools = append(note.Tools, chat.RecalledTool{
				Name:    x.Name,
				Content: strings.TrimSpace(result.Content),
				TookMS:  took.Milliseconds(),
				Cached:  cached,
			})
		}

		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		if framing := strings.TrimSpace(x.WhenUnasked); framing != "" {
			b.WriteString(framing + "\n\n")
		}
		b.WriteString(x.Name + " returned, just now:\n" + strings.TrimSpace(result.Content))
	}

	if b.Len() == 0 {
		return ""
	}

	// A block in the prompt rather than tool results on the wire, and
	// the difference is measured. As results the model read them as
	// things to report: told somebody was tired it answered with their
	// tablets, in two conversations out of three, with the warning
	// sitting in the prompt several thousand tokens away. Keeping the
	// warning against the listing is what makes it hold -- which is
	// how the hand-written version worked before this replaced it.
	//
	// Nothing is written to the conversation. These were not asked for
	// and should not fill somebody's transcript. They are written to the
	// chat's record of what it was shown, which is a different thing: not
	// part of what was said, only part of how the answer came about.
	return b.String()
}

// held : A prefetched answer and when it was taken.
type held struct {
	result tool.Result
	at     time.Time
}

// prefetched : A still-fresh answer for this tool, if one was kept.
//
// Zero freshness means none is ever reused, which is the default:
// accuracy before latency. Only a tool that says otherwise is cached.
func (r *Runner) prefetched(userID string, x tool.Tool) (tool.Result, bool) {
	if x.Fresh <= 0 {
		return tool.Result{}, false
	}

	r.freshMu.Lock()
	defer r.freshMu.Unlock()

	kept, ok := r.fresh[userID+"\x00"+x.Name]
	if !ok || r.now().Sub(kept.at) > x.Fresh {
		return tool.Result{}, false
	}
	return kept.result, true
}

// keep : Holds an answer for as long as its tool allows.
func (r *Runner) keep(userID string, x tool.Tool, result tool.Result) {
	if x.Fresh <= 0 {
		return
	}

	r.freshMu.Lock()
	defer r.freshMu.Unlock()

	if r.fresh == nil {
		r.fresh = map[string]held{}
	}
	r.fresh[userID+"\x00"+x.Name] = held{result: result, at: r.now()}
}
