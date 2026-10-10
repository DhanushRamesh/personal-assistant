package runner

import (
	"context"
	"log/slog"
)

// Changes : Somewhere to say that a conversation has something new in it,
// so that a client showing it can fetch it again rather than wait to be
// asked. It must not block.
type Changes interface {
	ConversationsChanged(userID, conversationID string)
}

// changed : Says that a conversation has something new in it, for its
// owner's clients.
func (r *Runner) changed(ctx context.Context, conversationID string) {
	if r.changes == nil {
		return
	}
	c, err := r.repo.GetConversation(ctx, conversationID)
	if err != nil {
		r.logger.WarnContext(ctx, "cannot say the conversation changed",
			slog.String("conversation_id", conversationID), slog.Any("error", err))
		return
	}
	r.changes.ConversationsChanged(c.UserID, c.ID)
}
