package chat

import "time"

// Recalled : What was put in front of the model for one chat, and why.
//
// Not part of the conversation: nobody said it. It is how the answer came to
// be what it is, written once before the model is asked and read only when
// somebody wants to know why it answered as it did.
type Recalled struct {
	// Always : The memories that go into every prompt.
	Always []RecalledNote `json:"always,omitempty"`
	// Notes : The memories found by searching, nearest first.
	Notes []RecalledNote `json:"notes,omitempty"`
	// Exchanges : The past exchanges found by searching, nearest first.
	Exchanges []RecalledExchange `json:"exchanges,omitempty"`
	// Tools : The listings read before the question was read, in the order
	// they were read.
	Tools []RecalledTool `json:"tools,omitempty"`
	// TookMS : How long searching took, in milliseconds.
	TookMS int64 `json:"took_ms,omitempty"`
	// ByWords : Whether words were compared rather than meaning, which
	// happens when there is no embedding server.
	ByWords bool `json:"by_words,omitempty"`
}

// Empty : Whether nothing was recalled at all.
func (r Recalled) Empty() bool {
	return len(r.Always) == 0 && len(r.Notes) == 0 &&
		len(r.Exchanges) == 0 && len(r.Tools) == 0
}

// RecalledNote : One memory that was offered.
type RecalledNote struct {
	// ID : Which memory.
	ID string `json:"id"`
	// Text : What it said, as the model was shown it.
	Text string `json:"text"`
	// Score : How near it was, or zero for one that is always offered.
	Score float64 `json:"score,omitempty"`
}

// RecalledExchange : One past exchange that was offered.
type RecalledExchange struct {
	// MessageID : Which exchange.
	MessageID string `json:"message_id"`
	// ConversationID : Where it was said.
	ConversationID string `json:"conversation_id"`
	// Text : What was said, as the model was shown it.
	Text string `json:"text"`
	// Score : How near it was.
	Score float64 `json:"score"`
	// At : When it was said.
	At time.Time `json:"at"`
}

// RecalledTool : One listing read before the question, and what it said.
//
// A prefetched listing goes into the prompt rather than onto the wire as a
// tool result, so it leaves no tool call behind. This is what records that
// it ran, for the timeline that shows how an answer was made.
type RecalledTool struct {
	// Name : Which tool.
	Name string `json:"name"`
	// Content : What it returned, as the model was shown it.
	Content string `json:"content"`
	// TookMS : How long the call took, in milliseconds.
	TookMS int64 `json:"took_ms,omitempty"`
	// Cached : Whether an earlier answer was reused instead of calling.
	Cached bool `json:"cached,omitempty"`
}
