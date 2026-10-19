// Package asking : Putting a prompt to the assistant, whichever way it
// arrives.
//
// Home Assistant asks over POST /api/chat and the desktop client over its
// live WebSocket. Both do the same five things in the same order, and they
// live here so neither does them differently: resolve the conversation,
// stop whatever is still running in it, store the new chat, listen for its
// events, then submit it. Listening before submitting is the point of the
// order: an answer arriving at once is heard rather than falling into the
// gap between the two.
package asking

import (
	"context"
	"errors"
	"log/slog"

	"personal-assistant/internal/base/records/chat"
	"personal-assistant/internal/base/records/store"
	"personal-assistant/internal/base/world/events"
)

// Runner : The part of the chat runner asking needs.
type Runner interface {
	// Submit : Starts running a stored chat in the background.
	Submit(t *chat.Chat) error
	// CancelBecause : Stops a queued or running chat, saying why for the
	// transcript, and reports whether one was found.
	CancelBecause(id, reason string) bool
}

// Superseded : Why a chat stopped for a new prompt, as the transcript says
// it. A reason of its own, so the turn is kept for the next one to read --
// a correction makes sense only beside what it corrects -- where a turn the
// person stopped is forgotten.
const Superseded = "The person asked something new before this finished."

// Subscriber : Somewhere to listen for a chat's events as they happen.
type Subscriber interface {
	Subscribe(chatID string) (<-chan events.Event, func())
}

// Asker : The thing a prompt is put to. It is the shape both ways in hold.
type Asker interface {
	Ask(ctx context.Context, r Request) (*Started, error)
}

// Request : One prompt, and who is asking.
type Request struct {
	UserID   string
	ClientID string
	// Channel and Model : The asking client's, copied onto the chat so a
	// chat recovered after a restart runs as it was accepted.
	Channel chat.Channel
	Model   chat.Model
	// ActiveConversationID : Where the client is, used when ConversationID
	// is empty.
	ActiveConversationID string
	// ConversationID : The conversation to ask in, when the caller names one.
	ConversationID string
	Prompt         string
	// Where : What the person has in front of them in the client right
	// now, in plain text, given to the model beside the prompt. Optional;
	// trimmed and cut to chat.MaxWhereBytes here, so every way in is held
	// to the same.
	Where string
	// ReplyTo : The words of the assistant's message the person chose to
	// reply to; empty when none was chosen. Cut to chat.MaxReplyToBytes.
	ReplyTo string
}

// Started : A chat that has been accepted and is running.
type Started struct {
	Chat           *chat.Chat
	ConversationID string
	// Events : The chat's events, subscribed before it was submitted.
	Events <-chan events.Event
	// Stop : Ends the subscription. The chat runs on regardless.
	Stop func()
}

var (
	// ErrNoSuchConversation : The conversation named does not exist, or is
	// somebody else's; the two are answered alike.
	ErrNoSuchConversation = errors.New("asking: no such conversation")
	// ErrNotAccepting : The runner would not take the chat.
	ErrNotAccepting = errors.New("asking: not accepting work")
	// ErrEmptyPrompt and ErrPromptTooLong : The prompt itself was refused.
	ErrEmptyPrompt   = chat.ErrEmptyPrompt
	ErrPromptTooLong = chat.ErrPromptTooLong
)

// Service : Asks through a repository, a runner and an event bus.
type Service struct {
	logger *slog.Logger
	repo   store.Repository
	runner Runner
	bus    Subscriber
}

// New : Builds the service.
func New(logger *slog.Logger, repo store.Repository, runner Runner, bus Subscriber) *Service {
	return &Service{logger: logger, repo: repo, runner: runner, bus: bus}
}

// Ask : Puts the prompt to the assistant and returns the running chat.
func (s *Service) Ask(ctx context.Context, r Request) (*Started, error) {
	asked := r.ConversationID
	if asked != "" && !s.ownedBy(ctx, r.UserID, asked) {
		return nil, ErrNoSuchConversation
	}
	if asked == "" {
		asked = r.ActiveConversationID
	}
	conversationID, err := chat.ActiveConversation(ctx, s.repo, r.UserID, r.ClientID, asked)
	if err != nil {
		return nil, err
	}

	// Asking again means the previous answer is no longer wanted, and two
	// cannot be listened to at once.
	s.supersede(ctx, conversationID)

	t, err := chat.New(conversationID, r.Channel, r.Prompt)
	if err != nil {
		return nil, err
	}
	t.Model = r.Model
	t.Where = chat.CutWhere(r.Where)
	t.ReplyTo = chat.CutReplyTo(r.ReplyTo)
	// Which client asked: a tool that acts on the client itself, such as
	// switching the conversation it talks in, has no other way to know.
	t.ClientID = r.ClientID
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}

	live, unsubscribe := s.bus.Subscribe(t.ID)
	if err := s.runner.Submit(t); err != nil {
		unsubscribe()
		s.logger.ErrorContext(ctx, "cannot submit chat", slog.Any("error", err))
		if failErr := t.Fail("That could not be started."); failErr == nil {
			_ = s.repo.Update(ctx, t)
		}
		return nil, ErrNotAccepting
	}
	return &Started{Chat: t, ConversationID: conversationID, Events: live, Stop: unsubscribe}, nil
}

// Owns : Whether a chat exists and was asked in one of the user's
// conversations, for a caller that wants to cancel it.
func (s *Service) Owns(ctx context.Context, userID, chatID string) bool {
	t, err := s.repo.Get(ctx, chatID)
	return err == nil && s.ownedBy(ctx, userID, t.ConversationID)
}

// Failure : The code and detail a failed chat was recorded with, for a
// caller reporting how it ended.
func (s *Service) Failure(ctx context.Context, chatID string) (code, detail string) {
	t, err := s.repo.Get(ctx, chatID)
	if err != nil {
		return "", ""
	}
	return t.ErrorCode, t.ErrorDetail
}

// ownedBy : Whether a conversation exists and belongs to the user. One that
// is somebody else's is answered as missing, as everywhere else.
func (s *Service) ownedBy(ctx context.Context, userID, conversationID string) bool {
	if !chat.ValidConversationID(conversationID) {
		return false
	}
	conversation, err := s.repo.GetConversation(ctx, conversationID)
	return err == nil && conversation.UserID == userID
}

// supersede : Stops whatever is still running in a conversation. A failure
// is logged rather than refused: the new prompt matters more than tidying
// the old one, and a chat left running still ends on its own.
func (s *Service) supersede(ctx context.Context, conversationID string) {
	unfinished, err := s.repo.Unfinished(ctx, conversationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "cannot find chats to supersede", slog.Any("error", err))
		return
	}
	for _, id := range unfinished {
		if s.runner.CancelBecause(id, Superseded) {
			s.logger.InfoContext(ctx, "superseded by a new prompt", slog.String("chat_id", id))
			continue
		}
		// Nothing is working on it, which happens to a chat left behind by
		// a process that stopped. Stop it here instead.
		t, err := s.repo.Get(ctx, id)
		if err != nil || t.Status.IsTerminal() {
			continue
		}
		if err := t.Cancel(); err == nil {
			_ = s.repo.Update(ctx, t)
		}
	}
}
