// Package service conversation actions: internal notes and conversation state.
// A note is an outgoing private message that Chatwoot keeps internal; it is
// never a customer reply. Status and priority mutations return the state the
// API reported, and every mutation requires an explicit conversation id.
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chatwoot-mcp/internal/core"
)

var validStatuses = map[string]bool{
	"open":     true,
	"pending":  true,
	"resolved": true,
	"snoozed":  true,
}

var validPriorities = map[string]bool{
	"none":   true,
	"low":    true,
	"medium": true,
	"high":   true,
	"urgent": true,
}

// NoteResult reports an internal note accepted by Chatwoot. Private is always
// true; Delivery records API acceptance, not customer delivery.
type NoteResult struct {
	ConversationID int64        `json:"conversation_id"`
	Message        core.Message `json:"message"`
	Private        bool         `json:"private"`
	Delivery       string       `json:"delivery"`
}

// StatusResult reports the conversation status Chatwoot returned.
type StatusResult struct {
	ConversationID int64  `json:"conversation_id"`
	Status         string `json:"status"`
	SnoozedUntil   string `json:"snoozed_until,omitempty"`
}

// PriorityResult reports the conversation priority Chatwoot returned.
type PriorityResult struct {
	ConversationID int64  `json:"conversation_id"`
	Priority       string `json:"priority"`
}

// AddPrivateNote reads the conversation to confirm the id, then creates one
// outgoing private message. It does not require can_reply because an internal
// note is never delivered to the customer, and it never retries.
func (s *service) AddPrivateNote(ctx context.Context, conversationID int64, content string) (NoteResult, error) {
	if conversationID <= 0 {
		return NoteResult{}, invalidInput("conversation id must be a positive integer")
	}
	if strings.TrimSpace(content) == "" {
		return NoteResult{}, invalidInput("note content must not be empty")
	}

	conv, err := s.api.GetConversation(ctx, conversationID)
	if err != nil {
		return NoteResult{}, fromAPI(err)
	}
	if conv.ID != conversationID {
		return NoteResult{}, &Error{
			Code:    CodeConversationMismatch,
			Message: fmt.Sprintf("requested conversation %d but the API returned conversation %d", conversationID, conv.ID),
		}
	}

	msg, err := s.api.CreateMessage(ctx, core.MessageRequest{ConversationID: conversationID, Content: content, Private: true})
	if err != nil {
		if isDeliveryUnknown(err) {
			return NoteResult{}, &Error{
				Code:     CodeDeliveryUnknown,
				Message:  fmt.Sprintf("conversation %d note result is unknown; check the conversation before retrying", conversationID),
				APIError: apiErrorOf(err),
			}
		}
		return NoteResult{}, fromAPI(err)
	}
	return NoteResult{ConversationID: conversationID, Message: msg, Private: true, Delivery: DeliveryAcceptedByAPI}, nil
}

// SetConversationStatus sets one of the accepted statuses. snoozed_until is
// only accepted together with snoozed and must be an RFC 3339 timestamp.
func (s *service) SetConversationStatus(ctx context.Context, conversationID int64, status, snoozedUntil string) (StatusResult, error) {
	if conversationID <= 0 {
		return StatusResult{}, invalidInput("conversation id must be a positive integer")
	}
	status = strings.TrimSpace(status)
	if !validStatuses[status] {
		return StatusResult{}, invalidInput("status must be one of open, pending, resolved or snoozed")
	}

	snoozedUntil = strings.TrimSpace(snoozedUntil)
	var snooze *time.Time
	if snoozedUntil != "" {
		if status != "snoozed" {
			return StatusResult{}, invalidInput("snoozed_until is only valid with status snoozed")
		}
		parsed, err := time.Parse(time.RFC3339, snoozedUntil)
		if err != nil {
			return StatusResult{}, invalidInput("snoozed_until must be an RFC 3339 timestamp")
		}
		snooze = &parsed
	}

	conv, err := s.api.SetStatus(ctx, core.StatusRequest{ConversationID: conversationID, Status: status, SnoozedUntil: snooze})
	if err != nil {
		return StatusResult{}, fromAPI(err)
	}
	return StatusResult{ConversationID: conversationID, Status: conv.Status, SnoozedUntil: conv.SnoozedUntil}, nil
}

// SetPriority sets one of the accepted priorities and reports the resulting
// value.
func (s *service) SetPriority(ctx context.Context, conversationID int64, priority string) (PriorityResult, error) {
	if conversationID <= 0 {
		return PriorityResult{}, invalidInput("conversation id must be a positive integer")
	}
	priority = strings.TrimSpace(strings.ToLower(priority))
	if !validPriorities[priority] {
		return PriorityResult{}, invalidInput("priority must be one of none, low, medium, high or urgent")
	}

	conv, err := s.api.SetPriority(ctx, core.PriorityRequest{ConversationID: conversationID, Priority: priority})
	if err != nil {
		return PriorityResult{}, fromAPI(err)
	}
	return PriorityResult{ConversationID: conversationID, Priority: conv.Priority}, nil
}
