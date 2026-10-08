// Package store : The whole of what the server keeps, as one thing.
//
// The five interfaces it joins live with the types they are about:
// chats and conversations with the chat, people with the user,
// devices with the client. They were one interface of twenty-six
// methods, which meant a handler that renamed a conversation was
// handed the means to create a user.
//
// This exists for the two implementations, which really do store all
// of it, and for wiring. A caller should ask for the narrowest of
// them it can: chat.Conversations if it only names conversations,
// user.Users if it only reads people.
package store

import (
	"personal-assistant/internal/base/chat"
	"personal-assistant/internal/base/client"
	"personal-assistant/internal/base/user"
)

// Repository : Everything the server stores about chats and who has
// them. Implementations are safe for concurrent use.
type Repository interface {
	chat.Chats
	chat.Conversations
	chat.Settings
	user.Users
	client.Clients
}
