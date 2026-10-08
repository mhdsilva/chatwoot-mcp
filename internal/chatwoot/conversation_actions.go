package chatwoot

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"chatwoot-mcp/internal/core"
)

// SetStatus sets a conversation status with the documented toggle_status
// endpoint and returns the state Chatwoot reports in the response payload.
func (c *Client) SetStatus(ctx context.Context, req core.StatusRequest) (core.Conversation, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(req.ConversationID, 10) + "/toggle_status"
	resource := "conversation " + strconv.FormatInt(req.ConversationID, 10) + " status"

	payload := map[string]any{"status": req.Status}
	if req.SnoozedUntil != nil {
		payload["snoozed_until"] = req.SnoozedUntil.UTC().Format(time.RFC3339)
	}

	var envelope struct {
		Payload struct {
			Success        bool   `json:"success"`
			ConversationID int64  `json:"conversation_id"`
			CurrentStatus  string `json:"current_status"`
			SnoozedUntil   string `json:"snoozed_until"`
		} `json:"payload"`
	}
	if err := c.call(ctx, http.MethodPost, path, nil, payload, &envelope, resource); err != nil {
		return core.Conversation{}, err
	}

	conversationID := envelope.Payload.ConversationID
	if conversationID <= 0 {
		conversationID = req.ConversationID
	}
	return core.Conversation{
		ID:           conversationID,
		Status:       envelope.Payload.CurrentStatus,
		SnoozedUntil: envelope.Payload.SnoozedUntil,
	}, nil
}

// SetPriority sets a conversation priority with the documented toggle_priority
// endpoint. That endpoint answers with an empty 200, so the stored value is
// read back; if the read fails the accepted value is reported instead of
// failing a mutation that already succeeded.
func (c *Client) SetPriority(ctx context.Context, req core.PriorityRequest) (core.Conversation, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(req.ConversationID, 10) + "/toggle_priority"
	resource := "conversation " + strconv.FormatInt(req.ConversationID, 10) + " priority"

	if err := c.call(ctx, http.MethodPost, path, nil, map[string]any{"priority": req.Priority}, nil, resource); err != nil {
		return core.Conversation{}, err
	}

	if conv, err := c.getConversationMeta(ctx, req.ConversationID); err == nil {
		if conv.Priority == "" {
			conv.Priority = req.Priority
		}
		return conv, nil
	}
	return core.Conversation{ID: req.ConversationID, Priority: req.Priority}, nil
}
