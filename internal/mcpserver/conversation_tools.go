package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/service"
)

type addPrivateNoteInput struct {
	ConversationID int64  `json:"conversation_id" jsonschema:"explicit positive conversation id to add the note to"`
	Content        string `json:"content" jsonschema:"internal note text; stored privately and never sent to the customer"`
}

type setConversationStatusInput struct {
	ConversationID int64  `json:"conversation_id" jsonschema:"explicit positive conversation id to update"`
	Status         string `json:"status" jsonschema:"new status: open, pending, resolved or snoozed"`
	SnoozedUntil   string `json:"snoozed_until,omitempty" jsonschema:"RFC 3339 time to snooze until; only valid with status snoozed"`
}

type setPriorityInput struct {
	ConversationID int64  `json:"conversation_id" jsonschema:"explicit positive conversation id to update"`
	Priority       string `json:"priority" jsonschema:"new priority: none, low, medium, high or urgent"`
}

type statusOutput struct {
	ConversationID int64  `json:"conversation_id"`
	Status         string `json:"status"`
	SnoozedUntil   string `json:"snoozed_until,omitempty"`
}

type priorityOutput struct {
	ConversationID int64  `json:"conversation_id"`
	Priority       string `json:"priority"`
}

// registerConversationTools adds notes and conversation state to the server.
// The descriptions separate an internal note from a customer reply so an agent
// cannot confuse the two.
func registerConversationTools(server *mcp.Server, svc service.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "add_private_note",
		Description: "Add an internal private note to an explicit conversation id. " +
			"The note is outgoing with private=true and is never sent to the customer; it is not a customer reply. " +
			"Use send_reply for customer-facing text.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addPrivateNoteInput) (*mcp.CallToolResult, result[service.NoteResult], error) {
		note, err := svc.AddPrivateNote(ctx, in.ConversationID, in.Content)
		if err != nil {
			return fail[service.NoteResult](err)
		}
		message, textTruncated := truncateMessage(note.Message)
		note.Message = message
		return ok(note, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "set_conversation_status",
		Description: "Set a conversation status to open, pending, resolved or snoozed and return the status Chatwoot reports. " +
			"snoozed_until is an RFC 3339 timestamp accepted only with snoozed; omitted, the conversation snoozes until the next reply.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setConversationStatusInput) (*mcp.CallToolResult, result[statusOutput], error) {
		status, err := svc.SetConversationStatus(ctx, in.ConversationID, in.Status, in.SnoozedUntil)
		if err != nil {
			return fail[statusOutput](err)
		}
		text, truncated := truncateUTF8(status.Status, MaxTextBytes)
		snoozed, cutSnoozed := truncateUTF8(status.SnoozedUntil, MaxTextBytes)
		return ok(statusOutput{ConversationID: status.ConversationID, Status: text, SnoozedUntil: snoozed}, truncated || cutSnoozed)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "set_priority",
		Description: "Set a conversation priority to none, low, medium, high or urgent and return the resulting value.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setPriorityInput) (*mcp.CallToolResult, result[priorityOutput], error) {
		priority, err := svc.SetPriority(ctx, in.ConversationID, in.Priority)
		if err != nil {
			return fail[priorityOutput](err)
		}
		text, truncated := truncateUTF8(priority.Priority, MaxTextBytes)
		return ok(priorityOutput{ConversationID: priority.ConversationID, Priority: text}, truncated)
	})
}
