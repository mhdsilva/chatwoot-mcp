package core

import "context"

// Store owns local credentials. Its implementation must protect the token on disk.
type Store interface {
	Load(context.Context) (Settings, error)
	Save(context.Context, Settings) error
}

// API is the Chatwoot Application API surface required by the first release.
type API interface {
	Check(context.Context) (Identity, error)
	ListConversations(context.Context, ListOptions) (Page[Conversation], error)
	GetConversation(context.Context, int64) (Conversation, error)
	SearchContacts(context.Context, string, int) (Page[Contact], error)
	ContactConversations(context.Context, int64, int) (Page[Conversation], error)
	CreateMessage(context.Context, int64, string) (Message, error)
}
