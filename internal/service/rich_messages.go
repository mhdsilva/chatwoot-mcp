// Package service rich messages: local attachments and channel templates.
// Attachments must be explicit local files; remote URLs and directories are
// refused. Size and MIME are checked before upload, and the channel support
// matrix decides whether the file may be sent at all. Templates are only sent
// on WhatsApp channels and are validated before the single POST.
package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"chatwoot-mcp/internal/core"
)

var validTemplateCategories = map[string]bool{
	"UTILITY":          true,
	"MARKETING":        true,
	"SHIPPING_UPDATE":  true,
	"TICKET_UPDATE":    true,
	"ISSUE_RESOLUTION": true,
}

// TemplateInput is a validated request to send one approved channel template.
type TemplateInput struct {
	Name            string
	Category        string
	Language        string
	ContentMode     string
	Content         string
	ProcessedParams map[string]any
}

// AttachmentResult reports an attachment message accepted by Chatwoot.
type AttachmentResult struct {
	ConversationID int64        `json:"conversation_id"`
	Message        core.Message `json:"message"`
	Filename       string       `json:"filename"`
	ContentType    string       `json:"content_type"`
	Size           int64        `json:"size"`
	Delivery       string       `json:"delivery"`
}

// SendAttachment reads one explicit local file, checks it against the channel
// matrix and sends it as a single non-retrying multipart POST.
func (s *service) SendAttachment(ctx context.Context, conversationID int64, path, content string) (AttachmentResult, error) {
	if conversationID <= 0 {
		return AttachmentResult{}, invalidInput("conversation id must be a positive integer")
	}
	attachment, err := readAttachment(path)
	if err != nil {
		return AttachmentResult{}, err
	}

	conv, err := s.api.GetConversation(ctx, conversationID)
	if err != nil {
		return AttachmentResult{}, fromAPI(err)
	}
	if conv.ID != conversationID {
		return AttachmentResult{}, &Error{
			Code:    CodeConversationMismatch,
			Message: fmt.Sprintf("requested conversation %d but the API returned conversation %d", conversationID, conv.ID),
		}
	}
	if !conv.CanReply {
		return AttachmentResult{}, &Error{
			Code:    CodeCannotReply,
			Message: fmt.Sprintf("conversation %d cannot accept a reply", conversationID),
		}
	}

	channelType, medium, err := s.channelOf(ctx, conv)
	if err != nil {
		return AttachmentResult{}, err
	}
	if ok, reason := attachmentAllowed(channelType, medium, attachment.ContentType, int64(len(attachment.Data))); !ok {
		return AttachmentResult{}, &Error{Code: CodeChannelUnsupported, Message: reason}
	}

	msg, err := s.api.CreateMessage(ctx, core.MessageRequest{ConversationID: conversationID, Content: content, Attachment: attachment})
	if err != nil {
		if isDeliveryUnknown(err) {
			return AttachmentResult{}, &Error{
				Code:     CodeDeliveryUnknown,
				Message:  fmt.Sprintf("conversation %d attachment delivery is unknown; check the conversation before retrying", conversationID),
				APIError: apiErrorOf(err),
			}
		}
		return AttachmentResult{}, fromAPI(err)
	}
	return AttachmentResult{
		ConversationID: conversationID,
		Message:        msg,
		Filename:       attachment.Filename,
		ContentType:    attachment.ContentType,
		Size:           int64(len(attachment.Data)),
		Delivery:       DeliveryAcceptedByAPI,
	}, nil
}

// ListMessageTemplates lists the approved templates cached for one WhatsApp
// inbox. Other channels are refused with a specific error.
func (s *service) ListMessageTemplates(ctx context.Context, inboxID int64) ([]core.Template, error) {
	if inboxID <= 0 {
		return nil, invalidInput("inbox id must be a positive integer")
	}
	inbox, err := s.api.GetInbox(ctx, inboxID)
	if err != nil {
		return nil, fromAPI(err)
	}
	if !isWhatsApp(inbox.ChannelType, inbox.Medium) {
		return nil, &Error{
			Code:    CodeChannelUnsupported,
			Message: fmt.Sprintf("inbox %d is a %s channel; message templates are only available on WhatsApp inboxes", inboxID, channelLabel(inbox.ChannelType)),
		}
	}
	templates, err := s.api.ListTemplates(ctx, inboxID)
	if err != nil {
		return nil, fromAPI(err)
	}
	return templates, nil
}

// SendTemplate sends one approved template on a WhatsApp conversation. It does
// not require can_reply, because a template may be sent outside the WhatsApp
// messaging window. It never retries.
func (s *service) SendTemplate(ctx context.Context, conversationID int64, in TemplateInput) (SendResult, error) {
	if conversationID <= 0 {
		return SendResult{}, invalidInput("conversation id must be a positive integer")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Language = strings.TrimSpace(in.Language)
	in.Category = strings.ToUpper(strings.TrimSpace(in.Category))
	in.ContentMode = strings.TrimSpace(in.ContentMode)
	if in.Name == "" {
		return SendResult{}, invalidInput("template name must not be empty")
	}
	if in.Language == "" {
		return SendResult{}, invalidInput("template language must not be empty")
	}
	if !validTemplateCategories[in.Category] {
		return SendResult{}, invalidInput("template category must be one of UTILITY, MARKETING, SHIPPING_UPDATE, TICKET_UPDATE or ISSUE_RESOLUTION")
	}
	if in.ContentMode != "" && in.ContentMode != "raw_template" && in.ContentMode != "rendered" {
		return SendResult{}, invalidInput("content_mode must be raw_template or rendered")
	}
	if in.ProcessedParams == nil {
		in.ProcessedParams = map[string]any{}
	}

	conv, err := s.api.GetConversation(ctx, conversationID)
	if err != nil {
		return SendResult{}, fromAPI(err)
	}
	if conv.ID != conversationID {
		return SendResult{}, &Error{
			Code:    CodeConversationMismatch,
			Message: fmt.Sprintf("requested conversation %d but the API returned conversation %d", conversationID, conv.ID),
		}
	}
	channelType, medium, err := s.channelOf(ctx, conv)
	if err != nil {
		return SendResult{}, err
	}
	if !isWhatsApp(channelType, medium) {
		return SendResult{}, &Error{
			Code:    CodeChannelUnsupported,
			Message: fmt.Sprintf("conversation %d is a %s channel; message templates are only available on WhatsApp conversations", conversationID, channelLabel(channelType)),
		}
	}

	if templates, listErr := s.api.ListTemplates(ctx, conv.InboxID); listErr == nil && len(templates) > 0 {
		if !templateApproved(templates, in.Name, in.Language) {
			return SendResult{}, &Error{
				Code:    CodeTemplateNotFound,
				Message: fmt.Sprintf("template %q (%s) is not among the approved templates for this inbox", in.Name, in.Language),
			}
		}
	}

	content := in.Content
	if strings.TrimSpace(content) == "" {
		content = in.Name
	}
	template := &core.TemplateRequest{
		Name:            in.Name,
		Category:        in.Category,
		Language:        in.Language,
		ContentMode:     in.ContentMode,
		ProcessedParams: in.ProcessedParams,
	}
	msg, err := s.api.CreateMessage(ctx, core.MessageRequest{ConversationID: conversationID, Content: content, Template: template})
	if err != nil {
		if isDeliveryUnknown(err) {
			return SendResult{}, &Error{
				Code:     CodeDeliveryUnknown,
				Message:  fmt.Sprintf("conversation %d template delivery is unknown; check the conversation before retrying", conversationID),
				APIError: apiErrorOf(err),
			}
		}
		return SendResult{}, fromAPI(err)
	}
	return SendResult{ConversationID: conversationID, Message: msg, Delivery: DeliveryAcceptedByAPI}, nil
}

// channelOf resolves the channel type and medium for a conversation. The
// conversation carries the channel type; a Twilio inbox needs one extra read
// to distinguish SMS from WhatsApp.
func (s *service) channelOf(ctx context.Context, conv core.Conversation) (string, string, error) {
	channelType := conv.ChannelType
	if channelType != "" && channelType != channelTwilioSms {
		return channelType, "", nil
	}
	inbox, err := s.api.GetInbox(ctx, conv.InboxID)
	if err != nil {
		return "", "", fromAPI(err)
	}
	return inbox.ChannelType, inbox.Medium, nil
}

func templateApproved(templates []core.Template, name, language string) bool {
	for _, template := range templates {
		if template.Name != name {
			continue
		}
		if language == "" || template.Language == "" || strings.EqualFold(template.Language, language) {
			return true
		}
	}
	return false
}

// readAttachment validates and reads one explicit local file. It rejects
// remote URLs, relative paths and directories, and checks the size before
// reading the bytes into memory.
func readAttachment(path string) (*core.Attachment, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, invalidInput("attachment path must not be empty")
	}
	if strings.Contains(trimmed, "://") {
		return nil, invalidInput("attachment path must be a local file, not a remote URL")
	}
	if !filepath.IsAbs(trimmed) {
		return nil, invalidInput("attachment path must be an absolute local file path")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return nil, invalidInput("attachment file cannot be read")
	}
	if info.IsDir() {
		return nil, invalidInput("attachment path must be a file, not a directory")
	}
	if info.Size() <= 0 {
		return nil, invalidInput("attachment file is empty")
	}
	if info.Size() > MaxAttachmentBytes {
		return nil, &Error{Code: CodeAttachmentTooLarge, Message: fmt.Sprintf("attachment exceeds the %d byte limit", MaxAttachmentBytes)}
	}

	file, err := os.Open(trimmed)
	if err != nil {
		return nil, invalidInput("attachment file cannot be opened")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxAttachmentBytes+1))
	if err != nil {
		return nil, invalidInput("attachment file cannot be read")
	}
	if len(data) > MaxAttachmentBytes {
		return nil, &Error{Code: CodeAttachmentTooLarge, Message: fmt.Sprintf("attachment exceeds the %d byte limit", MaxAttachmentBytes)}
	}

	return &core.Attachment{
		Filename:    filepath.Base(trimmed),
		ContentType: http.DetectContentType(data),
		Data:        data,
	}, nil
}
