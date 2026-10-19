package chat

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound : Nothing exists with the given identifier.
	ErrNotFound = errors.New("chat: not found")
	// ErrNotOwned : It exists but belongs to a different user.
	//
	// Distinguished from ErrNotFound inside the server so that a mistake is
	// diagnosable; at the edge both are answered the same way, because
	// telling one user that another's conversation exists reveals more than
	// it should.
	ErrNotOwned = errors.New("chat: belongs to another user")
)

// ConversationSummary : A conversation with the chats belonging to it.
type ConversationSummary struct {
	Conversation Conversation
	Chats        []Summary
}

// Summary : A chat without its response body.
//
// Listing chats and checking on one both read far more often than they need
// the answer itself, and a response can run to megabytes.
type Summary struct {
	ID             string
	ConversationID string
	Prompt         string
	// ReplyTo : The words of the message the chat replies to, when one was chosen.
	ReplyTo    string
	Channel    Channel
	Status     Status
	Error      string
	ErrorCode  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Filter : Narrows a listing of chats.
type Filter struct {
	// Status : Restricts the listing to one status. Empty means any.
	Status Status
	// ConversationID : Restricts the listing to one conversation. Empty means
	// any.
	ConversationID string
	// UserID : Restricts the listing to one user's chats. Empty means any.
	UserID string
	// Limit : The greatest number of chats to return. Zero selects
	// DefaultListLimit.
	Limit int
}

const (
	// DefaultListLimit : How many chats a listing returns when no limit is
	// given.
	DefaultListLimit = 50
	// MaxListLimit : The largest listing that will be returned, whatever is
	// asked for.
	MaxListLimit = 200
)

// Chats : Stores and retrieves chats and the messages produced while
// running them.
//
// Implementations are safe for concurrent use.
type Chats interface {

	// Create : Stores a new chat. It reports an error if one already exists
	// with the same identifier.
	Create(ctx context.Context, t *Chat) error

	// Get : Returns the chat with the given identifier, including its
	// response. It reports ErrNotFound if there is none.
	Get(ctx context.Context, id string) (*Chat, error)

	// Update : Writes a chat's current state over the stored one. It reports
	// ErrNotFound if the chat has since been removed.
	Update(ctx context.Context, t *Chat) error

	// SetRecalled : Records what memory put in front of the model for a
	// chat. Its own method rather than part of Update, which writes a fixed
	// list of columns and is called from the lifecycle.
	SetRecalled(ctx context.Context, id string, r *Recalled) error

	// List : Returns chats in reverse order of creation, newest first,
	// without their responses.
	List(ctx context.Context, f Filter) ([]Summary, error)

	// Unfinished : Returns the identifiers of a conversation's chats that
	// have not reached a terminal status, oldest first.
	Unfinished(ctx context.Context, conversationID string) ([]string, error)

	// FailRunning : Marks every chat still recorded as running as failed,
	// with the given explanation, and reports how many were changed.
	//
	// It is called at startup. A process that stopped mid-chat leaves rows
	// reading running that nothing will ever move, because whatever was
	// working on them is gone.
	FailRunning(ctx context.Context, reason string) (int64, error)
}

// Conversations : The exchanges chats belong to.
//
// Separate from [Chats] because a caller that only names and archives
// conversations has no business being handed the means to write one.
type Conversations interface {

	// SetActiveConversation : Makes a conversation the one a prompt from this
	// client lands in. It reports ErrNotFound if either does not exist, and
	// ErrNotOwned if the conversation belongs to another user.
	SetActiveConversation(ctx context.Context, userID, clientID, conversationID string) error

	// CreateConversation : Stores a new conversation.
	CreateConversation(ctx context.Context, c *Conversation) error

	// RenameConversation : Changes a conversation's title. It reports ErrNotFound if
	// there is no such conversation, and ErrNotOwned if it belongs to somebody
	// else.
	RenameConversation(ctx context.Context, userID, conversationID, title string) error

	// SetConversationArchived : Puts a conversation away or brings it back. It reports
	// ErrNotFound if there is no such conversation, and ErrNotOwned if it belongs
	// to somebody else.
	//
	// Archiving clears it from any client that was pointed at it, so that a
	// prompt does not land in a conversation the user has put away.
	SetConversationArchived(ctx context.Context, userID, conversationID string, archived bool) error

	// DeleteConversation : Removes a conversation and everything said in it. It reports
	// ErrNotFound if there is no such conversation, and ErrNotOwned if it belongs
	// to somebody else.
	//
	// The chats and the transcript go with it, by the cascade on their
	// foreign keys. Nothing here can be undone.
	DeleteConversation(ctx context.Context, userID, conversationID string) error

	// ForgetChat : Removes a chat and everything it wrote into its
	// conversation, as if it had never been asked. For a turn the person
	// stopped before anything was done or said (owner, 17 October 2026:
	// *"these kinds of stopped ones need not go in the conversation history,
	// in the UI nor the server .. just delete them from the database too"*).
	// No chat is not an error.
	ForgetChat(ctx context.Context, chatID string) error

	// GetConversation : Returns a conversation. It reports ErrNotFound if
	// there is none.
	GetConversation(ctx context.Context, id string) (*Conversation, error)

	// ListConversations : Returns a user's conversations, most recently used
	// first. They belong to the person, so every one of their clients sees
	// all of them.
	ListConversations(ctx context.Context, userID string, limit int) ([]Conversation, error)

	// ListArchivedConversations : Returns a user's archived conversations, most
	// recently used first.
	ListArchivedConversations(ctx context.Context, userID string, limit int) ([]Conversation, error)

	// ConversationTitles : The titles of those of ids that are the user's,
	// by id, read at once. One that is not theirs, or is gone, is absent.
	ConversationTitles(ctx context.Context, userID string, ids []string) (map[string]string, error)
}

// Settings : Values the server keeps for itself, by name.
type Settings interface {

	// Setting : Reads a setting belonging to the assistant itself, rather
	// than to a user, a client or a conversation. One never written yields
	// the empty string and no error.
	Setting(ctx context.Context, name string) (string, error)

	// SetSetting : Writes one, replacing whatever was there.
	SetSetting(ctx context.Context, name, value string) error
}
