package chatwoot

import (
	"context"

	"chatwoot-mcp/internal/core"
)

// Pending operations are declared here so the concrete client satisfies
// core.API from the contract phase on. Each function is replaced by its real
// implementation in the phase that delivers the feature; the concrete client
// must never answer a write with a silent success.
func pending(resource string) *Error {
	return &Error{Kind: KindRequest, Resource: resource, Message: "operation is not implemented in this build"}
}

func (c *Client) GetContact(context.Context, int64) (core.ContactDetail, error) {
	return core.ContactDetail{}, pending("contact")
}

func (c *Client) UpdateContact(context.Context, core.ContactUpdate) (core.ContactDetail, error) {
	return core.ContactDetail{}, pending("contact")
}

func (c *Client) CreateConversation(context.Context, core.ConversationCreateRequest) (core.Conversation, error) {
	return core.Conversation{}, pending("conversation")
}
