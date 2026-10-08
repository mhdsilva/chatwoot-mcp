// Package service contact actions: contact details, partial contact updates
// and new conversation creation. An update sends only the fields the caller
// provided, so omitted fields are preserved. A conversation is created only
// when the channel supports initiation, after validating the contact and inbox
// against the configured account.
package service

import (
	"context"
	"fmt"
	"strings"

	"chatwoot-mcp/internal/core"
)

// CreateConversationResult reports the new conversation's identity and channel.
type CreateConversationResult struct {
	ConversationID int64  `json:"conversation_id"`
	InboxID        int64  `json:"inbox_id"`
	ChannelType    string `json:"channel_type"`
	Status         string `json:"status"`
}

// GetContact reads one contact by explicit id.
func (s *service) GetContact(ctx context.Context, contactID int64) (core.ContactDetail, error) {
	if contactID <= 0 {
		return core.ContactDetail{}, invalidInput("contact id must be a positive integer")
	}
	contact, err := s.api.GetContact(ctx, contactID)
	if err != nil {
		return core.ContactDetail{}, fromAPI(err)
	}
	return contact, nil
}

// UpdateContact updates only the provided fields. At least one field is
// required, and each provided field must be non-blank.
func (s *service) UpdateContact(ctx context.Context, contactID int64, name, email, phone *string) (core.ContactDetail, error) {
	if contactID <= 0 {
		return core.ContactDetail{}, invalidInput("contact id must be a positive integer")
	}
	if name == nil && email == nil && phone == nil {
		return core.ContactDetail{}, invalidInput("provide at least one of name, email or phone to update")
	}

	update := core.ContactUpdate{ContactID: contactID}
	if name != nil {
		value, err := requiredField("name", *name)
		if err != nil {
			return core.ContactDetail{}, err
		}
		update.Name = &value
	}
	if email != nil {
		value, err := requiredField("email", *email)
		if err != nil {
			return core.ContactDetail{}, err
		}
		update.Email = &value
	}
	if phone != nil {
		value, err := requiredField("phone", *phone)
		if err != nil {
			return core.ContactDetail{}, err
		}
		update.Phone = &value
	}

	if _, err := s.api.GetContact(ctx, contactID); err != nil {
		return core.ContactDetail{}, fromAPI(err)
	}
	contact, err := s.api.UpdateContact(ctx, update)
	if err != nil {
		return core.ContactDetail{}, fromAPI(err)
	}
	return contact, nil
}

// CreateConversation creates a conversation only when the inbox channel
// supports initiation, after validating the contact and inbox.
func (s *service) CreateConversation(ctx context.Context, inboxID, contactID int64, sourceID string) (CreateConversationResult, error) {
	if inboxID <= 0 {
		return CreateConversationResult{}, invalidInput("inbox id must be a positive integer")
	}
	if contactID <= 0 {
		return CreateConversationResult{}, invalidInput("contact id must be a positive integer")
	}

	if _, err := s.api.GetContact(ctx, contactID); err != nil {
		return CreateConversationResult{}, fromAPI(err)
	}
	inbox, err := s.api.GetInbox(ctx, inboxID)
	if err != nil {
		return CreateConversationResult{}, fromAPI(err)
	}
	if !conversationInitiationAllowed(inbox.ChannelType, inbox.Medium) {
		return CreateConversationResult{}, &Error{
			Code:    CodeChannelUnsupported,
			Message: fmt.Sprintf("inbox %d is a %s channel; conversations cannot be initiated on this channel", inboxID, channelLabel(inbox.ChannelType)),
		}
	}

	conv, err := s.api.CreateConversation(ctx, core.ConversationCreateRequest{InboxID: inboxID, ContactID: contactID, SourceID: sourceID})
	if err != nil {
		if isDeliveryUnknown(err) {
			return CreateConversationResult{}, &Error{
				Code:     CodeDeliveryUnknown,
				Message:  fmt.Sprintf("conversation creation for contact %d is unknown; check the contact's conversations before retrying", contactID),
				APIError: apiErrorOf(err),
			}
		}
		return CreateConversationResult{}, fromAPI(err)
	}

	channelType := conv.ChannelType
	if channelType == "" {
		channelType = inbox.ChannelType
	}
	resultInbox := conv.InboxID
	if resultInbox == 0 {
		resultInbox = inboxID
	}
	return CreateConversationResult{
		ConversationID: conv.ID,
		InboxID:        resultInbox,
		ChannelType:    channelType,
		Status:         conv.Status,
	}, nil
}

func requiredField(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", invalidInput(name + " must not be blank")
	}
	return trimmed, nil
}
