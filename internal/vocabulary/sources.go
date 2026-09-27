package vocabulary

import (
	"context"
	"fmt"

	"github.com/DhanushRamesh/personal-assistant/internal/chat"
	"github.com/DhanushRamesh/personal-assistant/internal/memory"
	"github.com/DhanushRamesh/personal-assistant/internal/remind"
)

// Memories : The curated things remembered about a person.
type Memories interface {
	All(ctx context.Context, userID string, tier memory.Tier) ([]memory.Memory, error)
}

// Conversations : What a person's exchanges have been named.
type Conversations interface {
	ListConversations(ctx context.Context, userID string, limit int) ([]chat.Conversation, error)
}

// Reminders : What a person has asked to be reminded of.
type Reminders interface {
	List(ctx context.Context, userID string, states ...remind.Status) ([]remind.Reminder, error)
}

// Sources : Everywhere a person's own words are written down.
//
// Any of them may be nil, which contributes nothing rather than failing: a
// prompt built from two of the three is still better than the hand-written
// list alone.
type Sources struct {
	Memories      Memories
	Conversations Conversations
	Reminders     Reminders
}

// Gather : The phrases a person's own words appear in, most trusted first.
//
// Memories before conversation titles, and conversation titles before
// reminders, because that is the order of how far each can be trusted. A
// memory was written deliberately, and a conversation was named by a model
// that had read the whole exchange. A reminder title may be exactly the
// mishearing this exists to prevent: "Kitla BMRs" is stored alongside
// "GitLab Merge Requests", one of them a record of the fault.
//
// Order decides only what survives the budget, so the doubtful ones are
// the first to be dropped -- and correcting a wrongly heard reminder takes
// its words out altogether.
func (s Sources) Gather(ctx context.Context, userID string) ([]string, error) {
	var said []string

	if s.Memories != nil {
		for _, tier := range []memory.Tier{memory.TierAlways, memory.TierRecall} {
			held, err := s.Memories.All(ctx, userID, tier)
			if err != nil {
				return nil, fmt.Errorf("reading memories: %w", err)
			}
			for i := range held {
				said = append(said, held[i].Subject, held[i].Body)
			}
		}
	}

	if s.Conversations != nil {
		named, err := s.Conversations.ListConversations(ctx, userID, chat.MaxListLimit)
		if err != nil {
			return nil, fmt.Errorf("reading conversations: %w", err)
		}
		for i := range named {
			said = append(said, named[i].Title)
		}
	}

	if s.Reminders != nil {
		due, err := s.Reminders.List(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("reading reminders: %w", err)
		}
		for i := range due {
			said = append(said, due[i].Title, due[i].Body)
		}
	}

	return said, nil
}
