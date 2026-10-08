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

func (c *Client) Assign(context.Context, core.AssignmentRequest) (core.Conversation, error) {
	return core.Conversation{}, pending("conversation assignment")
}

func (c *Client) GetLabels(context.Context, int64) ([]string, error) {
	return nil, pending("conversation labels")
}

func (c *Client) SetLabels(context.Context, core.LabelsRequest) ([]string, error) {
	return nil, pending("conversation labels")
}

func (c *Client) ListInboxes(context.Context) ([]core.Inbox, error) {
	return nil, pending("inboxes")
}

func (c *Client) GetInbox(context.Context, int64) (core.Inbox, error) {
	return core.Inbox{}, pending("inbox")
}

func (c *Client) ListAgents(context.Context) ([]core.Agent, error) {
	return nil, pending("agents")
}

func (c *Client) ListTeams(context.Context) ([]core.Team, error) {
	return nil, pending("teams")
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

func (c *Client) ListTemplates(context.Context, int64) ([]core.Template, error) {
	return nil, pending("message templates")
}
