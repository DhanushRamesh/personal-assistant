package memory

import (
	"context"
	"sort"
	"time"

	"github.com/DhanushRamesh/personal-assistant/internal/conversation"
)

// Append : Stores a message at the end of its conversation.
func (m *Repository) Append(_ context.Context, msg conversation.Message) (conversation.Message, error) {
	if err := msg.Valid(); err != nil {
		return conversation.Message{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.appendSaidErr != nil {
		return conversation.Message{}, m.appendSaidErr
	}
	if _, ok := m.conversations[msg.ConversationID]; !ok {
		return conversation.Message{}, conversation.ErrNoConversation
	}
	if msg.At.IsZero() {
		msg.At = time.Now().UTC()
	}

	msg.Seq = len(m.said[msg.ConversationID]) + 1
	m.said[msg.ConversationID] = append(m.said[msg.ConversationID], msg)
	return msg, nil
}

// Before : Returns a conversation's messages up to but not including seq.
func (m *Repository) Before(_ context.Context, conversationID string, seq int) ([]conversation.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for _, msg := range m.said[conversationID] {
		if seq > 0 && msg.Seq >= seq {
			break
		}
		out = append(out, msg)
	}
	return out, nil
}

// All : Returns everything said in a conversation, oldest first.
func (m *Repository) All(ctx context.Context, conversationID string) ([]conversation.Message, error) {
	return m.Before(ctx, conversationID, 0)
}

// FailAppendingSaid : Makes every Append fail with err, so a caller's
// handling of an unwritable message can be tested.
// ByChat : Returns everything one turn wrote, oldest first.
func (m *Repository) ByChat(_ context.Context, chatID string) ([]conversation.Message, error) {
	if chatID == "" {
		return nil, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for _, said := range m.said {
		for _, msg := range said {
			if msg.ChatID == chatID {
				out = append(out, msg)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (m *Repository) FailAppendingSaid(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendSaidErr = err
}

// Said : Returns what was said in a conversation, for a test to assert on.
func (m *Repository) Said(conversationID string) []conversation.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]conversation.Message(nil), m.said[conversationID]...)
}

// Summary : Returns the conversation's condensed earlier conversation.
func (m *Repository) Summary(_ context.Context, conversationID string) (conversation.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.conversations[conversationID]; !ok {
		return conversation.Summary{}, conversation.ErrNoConversation
	}
	return m.summaries[conversationID], nil
}

// SetSummary : Replaces the conversation's condensed earlier conversation.
func (m *Repository) SetSummary(_ context.Context, conversationID string, s conversation.Summary) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.conversations[conversationID]; !ok {
		return conversation.ErrNoConversation
	}
	m.summaries[conversationID] = s
	return nil
}

// SaidSince : What one person has said across every conversation since a
// time, oldest first.
//
// The in-memory twin of the stored query. Conversations are walked
// rather than indexed, which is right for a store that exists to make
// tests quick and holds tens of messages, not thousands.
func (m *Repository) SaidSince(_ context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error) {
	if userID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for id, msgs := range m.said {
		if c, ok := m.conversations[id]; !ok || c.UserID != userID {
			continue
		}
		for _, msg := range msgs {
			if msg.Role == conversation.User && !msg.At.Before(since) {
				out = append(out, msg)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// CalledSince : Every tool the assistant ran for one person since a
// time, oldest first. The in-memory twin of the stored query.
// LastSpoke : When the assistant last said anything to one person.
func (m *Repository) LastSpoke(_ context.Context, userID string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var last time.Time
	for id, msgs := range m.said {
		if c, ok := m.conversations[id]; !ok || c.UserID != userID {
			continue
		}
		for _, msg := range msgs {
			if msg.Role == conversation.Assistant && msg.At.After(last) {
				last = msg.At
			}
		}
	}
	return last, nil
}

func (m *Repository) CalledSince(_ context.Context, userID string, since time.Time, limit int) ([]conversation.Message, error) {
	if userID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 2000
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []conversation.Message
	for id, msgs := range m.said {
		if c, ok := m.conversations[id]; !ok || c.UserID != userID {
			continue
		}
		for _, msg := range msgs {
			if len(msg.ToolCalls) > 0 && !msg.At.Before(since) {
				out = append(out, msg)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
