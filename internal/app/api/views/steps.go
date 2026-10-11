package views

import (
	"sort"
	"time"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/conversation"
)

// Step kinds, in the order they can happen.
const (
	// StepAsked : What the person said.
	StepAsked = "asked"
	// StepRecalled : What memory put in front of the model.
	StepRecalled = "recalled"
	// StepReadFirst : A listing read before the question, unasked.
	StepReadFirst = "read_first"
	// StepModelAsked : A request to the model went out.
	StepModelAsked = "model_asked"
	// StepModelReplied : The model's reply to it ended: with the answer,
	// asking for tools, failing, or unused because it was begun early.
	StepModelReplied = "model_replied"
	// StepToolCall : A tool the model asked for.
	StepToolCall = "tool_call"
	// StepToolResult : What that tool returned.
	StepToolResult = "tool_result"
	// StepBegan : When the first piece of the reply went out, on a turn
	// whose reply was streamed. The moment the person stopped waiting.
	StepBegan = "began"
	// StepAnswered : The reply.
	StepAnswered = "answered"
	// StepFailed : Why there was no reply.
	StepFailed = "failed"
)

// Timeline : How one answer was made.
type Timeline struct {
	ChatID         string     `json:"chat_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	// TookMS : How long the whole answer took, once it had started.
	TookMS int64 `json:"took_ms,omitempty"`
	// Complete : Whether the whole timeline is here. False for a chat from
	// before messages recorded which turn wrote them, whose steps cannot be
	// told from another turn's.
	Complete bool   `json:"complete"`
	Steps    []Step `json:"steps"`
}

// Step : One thing that happened while the answer was made.
type Step struct {
	Kind string     `json:"kind"`
	At   *time.Time `json:"at,omitempty"`
	// OffsetMS : Milliseconds after the answer started, where that is known.
	OffsetMS *int64 `json:"offset_ms,omitempty"`
	// Text : What was said, for asked, answered and failed.
	Text string `json:"text,omitempty"`
	// Detail : What actually went wrong, for failed.
	Detail string `json:"detail,omitempty"`
	// Name : Which tool, for a call and its result.
	Name string `json:"name,omitempty"`
	// Arguments : What the model asked for, as it wrote them.
	Arguments string `json:"arguments,omitempty"`
	// Outcome : ok, failed or partial.
	Outcome string `json:"outcome,omitempty"`
	// Content : What the tool returned, or exactly what went wrong.
	Content string `json:"content,omitempty"`
	// TookMS : How long this step took, where that is known.
	TookMS int64 `json:"took_ms,omitempty"`
	// Cached : Whether a kept answer was reused, for a read_first step.
	Cached bool `json:"cached,omitempty"`
	// Recalled : What memory offered, for the recalled step.
	Recalled *chat.Recalled `json:"recalled,omitempty"`
	// ByWords : Whether memory matched wording rather than meaning.
	ByWords bool `json:"by_words,omitempty"`
	// FirstMS : How long the model took to send anything back, for
	// model_replied.
	FirstMS *int64 `json:"first_ms,omitempty"`
	// Early : Whether the request was begun before the one in front of it
	// had finished, for model_asked and model_replied.
	Early bool `json:"early,omitempty"`
}

// OfTimeline : The timeline of one chat, from the chat and what it wrote.
//
// Messages are the record of what happened and are trusted for the order.
// The chat supplies what has no message of its own: what was recalled,
// which is not something anybody said, and the failure, which is recorded
// on the chat rather than always in the conversation.
func OfTimeline(t *chat.Chat, wrote []conversation.Message) Timeline {
	out := Timeline{
		ChatID:         t.ID,
		ConversationID: t.ConversationID,
		Status:         string(t.Status),
		StartedAt:      t.StartedAt,
		FinishedAt:     t.FinishedAt,
		Complete:       len(wrote) > 0 || t.ConversationID == "",
		Steps:          []Step{},
	}
	if t.StartedAt != nil && t.FinishedAt != nil {
		out.TookMS = t.FinishedAt.Sub(*t.StartedAt).Milliseconds()
	}

	at := func(when time.Time) (*time.Time, *int64) {
		w := when
		if t.StartedAt == nil {
			return &w, nil
		}
		ms := when.Sub(*t.StartedAt).Milliseconds()
		return &w, &ms
	}

	// Listings read before the question come first: they are taken before
	// memory is searched and before anything is written down. They are
	// steps of their own rather than a detail of the recall step, because
	// what they are is a tool call that happened.
	if t.Recalled != nil {
		for _, x := range t.Recalled.Tools {
			step := Step{
				Kind: StepReadFirst, Name: x.Name,
				Content: x.Content, TookMS: x.TookMS, Cached: x.Cached,
				Outcome: string(conversation.OutcomeOK),
			}
			if t.StartedAt != nil {
				step.At, step.OffsetMS = at(*t.StartedAt)
			}
			out.Steps = append(out.Steps, step)
		}
	}

	// What was recalled happens before anything is written down, so it is
	// placed first whatever the messages say.
	if t.Recalled != nil && !t.Recalled.Empty() {
		step := Step{
			Kind:     StepRecalled,
			Recalled: t.Recalled,
			TookMS:   t.Recalled.TookMS,
			ByWords:  t.Recalled.ByWords,
		}
		if t.StartedAt != nil {
			step.At, step.OffsetMS = at(*t.StartedAt)
		}
		out.Steps = append(out.Steps, step)
	}

	for _, m := range wrote {
		when, offset := at(m.At)

		switch {
		case len(m.ToolCalls) > 0:
			for _, c := range m.ToolCalls {
				out.Steps = append(out.Steps, Step{
					Kind: StepToolCall, At: when, OffsetMS: offset,
					Name: c.Name, Arguments: c.Arguments,
				})
			}

		case len(m.ToolResults) > 0:
			for _, res := range m.ToolResults {
				out.Steps = append(out.Steps, Step{
					Kind: StepToolResult, At: when, OffsetMS: offset,
					Name: res.Name, Outcome: string(res.Outcome),
					Content: res.Content, TookMS: res.TookMS,
				})
			}

		case m.Role == conversation.User:
			// The question goes first, ahead of the recall it caused.
			out.Steps = append([]Step{{
				Kind: StepAsked, At: when, OffsetMS: offset, Text: m.Content,
			}}, out.Steps...)

		case m.Kind == conversation.Failure:
			out.Steps = append(out.Steps, Step{
				Kind: StepFailed, At: when, OffsetMS: offset,
				Text: m.Content, Detail: m.Detail,
			})

		default:
			// What the person actually waited for, in front of the
			// answer it belongs to. The answer's own time is when
			// the last word of it was written, by which point they
			// had been listening for several seconds.
			if t.AnswerBeganAt != nil {
				began, beganOffset := at(*t.AnswerBeganAt)
				out.Steps = append(out.Steps, Step{
					Kind: StepBegan, At: began, OffsetMS: beganOffset,
				})
			}
			out.Steps = append(out.Steps, Step{
				Kind: StepAnswered, At: when, OffsetMS: offset, Text: m.Content,
			})
		}
	}

	out.Steps = withCalls(out.Steps, t.Calls, at)
	return out
}

// lead : Whether a step belongs ahead of everything timed: the question
// and what was read before it.
func lead(kind string) bool {
	return kind == StepAsked || kind == StepReadFirst || kind == StepRecalled
}

// withCalls : The steps with the requests made to the model placed among
// them by time. The question and what was read before it stay first, as
// OfTimeline puts them.
func withCalls(steps []Step, calls []chat.Call, at func(time.Time) (*time.Time, *int64)) []Step {
	if len(calls) == 0 {
		return steps
	}
	var timed []Step
	for _, c := range calls {
		when, offset := at(c.AskedAt)
		timed = append(timed, Step{Kind: StepModelAsked, At: when, OffsetMS: offset, Early: c.Early})
		if c.DoneAt == nil {
			continue
		}
		reply := Step{Kind: StepModelReplied, Outcome: string(c.Ended), Early: c.Early,
			TookMS: c.DoneAt.Sub(c.AskedAt).Milliseconds()}
		reply.At, reply.OffsetMS = at(*c.DoneAt)
		if c.HeardAt != nil {
			first := c.HeardAt.Sub(c.AskedAt).Milliseconds()
			reply.FirstMS = &first
		}
		timed = append(timed, reply)
	}

	n := 0
	for n < len(steps) && lead(steps[n].Kind) {
		n++
	}
	out := append([]Step{}, steps[:n]...)
	rest := steps[n:]
	// Merged in order: a message written at the same moment as a reply
	// is what that reply produced, so the reply goes first.
	sort.SliceStable(timed, func(i, j int) bool { return timed[i].At.Before(*timed[j].At) })
	for len(rest) > 0 || len(timed) > 0 {
		if len(timed) > 0 && (len(rest) == 0 || rest[0].At == nil || !timed[0].At.After(*rest[0].At)) {
			out = append(out, timed[0])
			timed = timed[1:]
			continue
		}
		out = append(out, rest[0])
		rest = rest[1:]
	}
	return out
}
