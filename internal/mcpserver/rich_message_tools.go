package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/service"
)

type sendAttachmentInput struct {
	ConversationID int64  `json:"conversation_id" jsonschema:"explicit positive conversation id to send the attachment to"`
	Path           string `json:"path" jsonschema:"absolute local file path; a remote URL or a directory is rejected"`
	Content        string `json:"content,omitempty" jsonschema:"optional caption stored with the attachment"`
}

type listMessageTemplatesInput struct {
	InboxID int64 `json:"inbox_id" jsonschema:"explicit positive WhatsApp inbox id whose approved templates are listed"`
}

type sendTemplateInput struct {
	ConversationID  int64          `json:"conversation_id" jsonschema:"explicit positive WhatsApp conversation id to send the template to"`
	TemplateName    string         `json:"template_name" jsonschema:"approved template name"`
	Language        string         `json:"language" jsonschema:"template language code, for example en_US or pt_BR"`
	Category        string         `json:"category" jsonschema:"template category: UTILITY, MARKETING, SHIPPING_UPDATE, TICKET_UPDATE or ISSUE_RESOLUTION"`
	ProcessedParams map[string]any `json:"processed_params,omitempty" jsonschema:"template parameter values keyed by component, such as {body: {1: Ana}}"`
	Content         string         `json:"content,omitempty" jsonschema:"optional rendered transcript; defaults to the template name"`
	ContentMode     string         `json:"content_mode,omitempty" jsonschema:"raw_template or rendered; omit for rendered"`
}

type templateSummary struct {
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Category string `json:"category,omitempty"`
	Status   string `json:"status,omitempty"`
	Body     string `json:"body,omitempty"`
}

type templatesOutput struct {
	Templates []templateSummary `json:"templates"`
	Truncated bool              `json:"truncated"`
}

// registerRichMessageTools adds local attachments and WhatsApp templates. Each
// tool performs a single outbound attempt and never retries.
func registerRichMessageTools(server *mcp.Server, svc service.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "send_attachment",
		Description: "Send one local file to an explicit conversation id. The path must be an absolute local file; remote URLs and directories are rejected. " +
			"Size and MIME are checked before upload and channel limits are enforced. One attempt per call; delivery accepted_by_api is not customer receipt.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendAttachmentInput) (*mcp.CallToolResult, result[service.AttachmentResult], error) {
		sent, err := svc.SendAttachment(ctx, in.ConversationID, in.Path, in.Content)
		if err != nil {
			return fail[service.AttachmentResult](err)
		}
		message, textTruncated := truncateMessage(sent.Message)
		sent.Message = message
		return ok(sent, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_message_templates",
		Description: "List the approved message templates cached for an explicit WhatsApp inbox id. A non-WhatsApp inbox is refused with a specific error.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listMessageTemplatesInput) (*mcp.CallToolResult, result[templatesOutput], error) {
		templates, err := svc.ListMessageTemplates(ctx, in.InboxID)
		if err != nil {
			return fail[templatesOutput](err)
		}
		items, truncated := boundList(templates)
		out := make([]templateSummary, len(items))
		textTruncated := false
		for i, template := range items {
			name, cutName := truncateUTF8(template.Name, MaxTextBytes)
			language, cutLanguage := truncateUTF8(template.Language, MaxTextBytes)
			category, cutCategory := truncateUTF8(template.Category, MaxTextBytes)
			status, cutStatus := truncateUTF8(template.Status, MaxTextBytes)
			body, cutBody := truncateUTF8(template.Body, MaxMessageContentBytes)
			textTruncated = textTruncated || cutName || cutLanguage || cutCategory || cutStatus || cutBody
			out[i] = templateSummary{Name: name, Language: language, Category: category, Status: status, Body: body}
		}
		return ok(templatesOutput{Templates: out, Truncated: truncated}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_template",
		Description: "Send one approved WhatsApp template to an explicit conversation id, including outside the messaging window. " +
			"Only WhatsApp channels are supported; parameters are validated and the template must be approved. One attempt per call; no automatic retry.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sendTemplateInput) (*mcp.CallToolResult, result[service.SendResult], error) {
		sent, err := svc.SendTemplate(ctx, in.ConversationID, service.TemplateInput{
			Name:            in.TemplateName,
			Category:        in.Category,
			Language:        in.Language,
			ContentMode:     in.ContentMode,
			Content:         in.Content,
			ProcessedParams: in.ProcessedParams,
		})
		if err != nil {
			return fail[service.SendResult](err)
		}
		message, textTruncated := truncateMessage(sent.Message)
		sent.Message = message
		return ok(sent, textTruncated)
	})
}
