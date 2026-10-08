package chatwoot

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"chatwoot-mcp/internal/core"
)

type contactDetailWire struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
	Identifier  string `json:"identifier"`
	Blocked     bool   `json:"blocked"`
}

func (c contactDetailWire) toCore() core.ContactDetail {
	return core.ContactDetail{
		ID:         c.ID,
		Name:       c.Name,
		Email:      c.Email,
		Phone:      c.PhoneNumber,
		Identifier: c.Identifier,
		Blocked:    c.Blocked,
	}
}

// GetContact reads one contact so callers can confirm it belongs to the
// configured account before a mutation.
func (c *Client) GetContact(ctx context.Context, id int64) (core.ContactDetail, error) {
	resource := "contact " + strconv.FormatInt(id, 10)
	var envelope struct {
		Payload contactDetailWire `json:"payload"`
	}
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/contacts/"+strconv.FormatInt(id, 10), nil, nil, &envelope, resource); err != nil {
		return core.ContactDetail{}, err
	}
	return envelope.Payload.toCore(), nil
}

// UpdateContact updates only the fields present in req, so an omitted field is
// left unchanged rather than cleared.
func (c *Client) UpdateContact(ctx context.Context, req core.ContactUpdate) (core.ContactDetail, error) {
	resource := "contact " + strconv.FormatInt(req.ContactID, 10)
	payload := map[string]any{}
	if req.Name != nil {
		payload["name"] = *req.Name
	}
	if req.Email != nil {
		payload["email"] = *req.Email
	}
	if req.Phone != nil {
		payload["phone_number"] = *req.Phone
	}

	var envelope struct {
		Payload contactDetailWire `json:"payload"`
	}
	path := c.accountPath() + "/contacts/" + strconv.FormatInt(req.ContactID, 10)
	if err := c.call(ctx, http.MethodPut, path, nil, payload, &envelope, resource); err != nil {
		return core.ContactDetail{}, err
	}
	return envelope.Payload.toCore(), nil
}

// CreateConversation starts a conversation for an existing contact in an
// existing inbox. source_id is optional; Chatwoot generates one per channel
// when it is omitted.
func (c *Client) CreateConversation(ctx context.Context, req core.ConversationCreateRequest) (core.Conversation, error) {
	payload := map[string]any{"inbox_id": req.InboxID, "contact_id": req.ContactID}
	if sourceID := strings.TrimSpace(req.SourceID); sourceID != "" {
		payload["source_id"] = sourceID
	}

	var conv conversationWire
	if err := c.call(ctx, http.MethodPost, c.accountPath()+"/conversations", nil, payload, &conv, "conversation"); err != nil {
		return core.Conversation{}, err
	}
	return conv.toCore(), nil
}
