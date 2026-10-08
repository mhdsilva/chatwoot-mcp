package core

import "context"

// Store owns local credentials. Its implementation must protect the token on disk.
type Store interface {
	Load(context.Context) (Settings, error)
	Save(context.Context, Settings) error
}

// API is the Chatwoot Application API surface required by the complete
// service. Read methods never mutate state; every mutating method takes
// explicit identifiers. A concrete implementation must satisfy this interface
// in full, so a missing method is a compile-time failure.
type API interface {
	Check(context.Context) (Identity, error)
	ListConversations(context.Context, ListOptions) (Page[Conversation], error)
	GetConversation(context.Context, int64) (Conversation, error)
	SearchContacts(context.Context, string, int) (Page[Contact], error)
	ContactConversations(context.Context, int64, int) (Page[Conversation], error)

	CreateMessage(context.Context, MessageRequest) (Message, error)
	SetStatus(context.Context, StatusRequest) (Conversation, error)
	SetPriority(context.Context, PriorityRequest) (Conversation, error)
	Assign(context.Context, AssignmentRequest) (Conversation, error)
	GetLabels(context.Context, int64) ([]string, error)
	SetLabels(context.Context, LabelsRequest) ([]string, error)

	ListInboxes(context.Context) ([]Inbox, error)
	GetInbox(context.Context, int64) (Inbox, error)
	ListAgents(context.Context) ([]Agent, error)
	ListTeams(context.Context) ([]Team, error)

	GetContact(context.Context, int64) (ContactDetail, error)
	UpdateContact(context.Context, ContactUpdate) (ContactDetail, error)
	CreateConversation(context.Context, ConversationCreateRequest) (Conversation, error)

	ListTemplates(context.Context, int64) ([]Template, error)
}
