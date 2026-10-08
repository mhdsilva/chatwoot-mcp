package core

import "time"

// MessageRequest is a single outgoing message creation request. Content alone
// sends a customer reply; Private turns it into an internal note; Attachment
// sends one local file; Template sends a preapproved channel template. Callers
// must set explicit identifiers and never rely on a read to trigger a write.
type MessageRequest struct {
	ConversationID int64
	Content        string
	Private        bool
	Attachment     *Attachment
	Template       *TemplateRequest
}

// Attachment is a local file already read and validated by the service. The
// API client uploads Data as multipart form data and never fetches a remote
// URL, so a tool argument cannot make the server reach the network.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// TemplateRequest carries a preapproved channel template and its parameters.
// ProcessedParams is passed through to the channel unchanged; the service
// validates its shape before it reaches the client.
type TemplateRequest struct {
	Name            string
	Category        string
	Language        string
	ContentMode     string
	ProcessedParams map[string]any
}

// StatusRequest sets a conversation status. SnoozedUntil is only meaningful
// when Status is "snoozed"; the service rejects it for other statuses.
type StatusRequest struct {
	ConversationID int64
	Status         string
	SnoozedUntil   *time.Time
}

// PriorityRequest sets a conversation priority.
type PriorityRequest struct {
	ConversationID int64
	Priority       string
}

// AssignmentRequest assigns a conversation to an agent, a team, or both. At
// least one identifier must be positive.
type AssignmentRequest struct {
	ConversationID int64
	AgentID        int64
	TeamID         int64
}

// LabelsRequest replaces the complete label set of a conversation. Chatwoot's
// label endpoint overwrites the existing set, so callers must compute the full
// desired set from the current one.
type LabelsRequest struct {
	ConversationID int64
	Labels         []string
}

// ContactUpdate is a partial contact update. A nil field is left unchanged; a
// pointer to an empty string clears the field.
type ContactUpdate struct {
	ContactID int64
	Name      *string
	Email     *string
	Phone     *string
}

// ConversationCreateRequest starts a new conversation for an existing contact
// in an existing inbox. Both resources must belong to the configured account.
type ConversationCreateRequest struct {
	InboxID   int64
	ContactID int64
	SourceID  string
}

// Inbox is the account-visible projection of a channel. ChannelType is the
// Chatwoot channel class name, such as "Channel::Whatsapp".
type Inbox struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
}

// Agent is an account user that can be assigned conversations.
type Agent struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	Role         string `json:"role,omitempty"`
	Availability string `json:"availability_status,omitempty"`
}

// Team is an account team that can be assigned conversations.
type Team struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ContactDetail is the full contact projection used by get_contact and as the
// result of an update.
type ContactDetail struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Identifier string `json:"identifier,omitempty"`
	Blocked    bool   `json:"blocked"`
}

// Template is one approved message template exposed by a WhatsApp inbox. Body
// is the template body text when the provider exposes it, bounded by the
// adapter before it reaches a client.
type Template struct {
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Category string `json:"category,omitempty"`
	Status   string `json:"status,omitempty"`
	Body     string `json:"body,omitempty"`
}
