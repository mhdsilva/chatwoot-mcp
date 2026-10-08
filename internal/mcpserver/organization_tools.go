package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/service"
)

type listInput struct{}

type assignConversationInput struct {
	ConversationID int64 `json:"conversation_id" jsonschema:"explicit positive conversation id to assign"`
	AgentID        int64 `json:"agent_id,omitempty" jsonschema:"positive agent id from list_agents; 0 or omitted to leave the agent unchanged"`
	TeamID         int64 `json:"team_id,omitempty" jsonschema:"positive team id from list_teams; 0 or omitted to leave the team unchanged"`
}

type conversationLabelsInput struct {
	ConversationID int64 `json:"conversation_id" jsonschema:"explicit positive conversation id"`
}

type changeConversationLabelsInput struct {
	ConversationID int64    `json:"conversation_id" jsonschema:"explicit positive conversation id"`
	Labels         []string `json:"labels" jsonschema:"label names to add or remove"`
}

type inboxSummary struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
}

type agentSummary struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"`
	Role         string `json:"role,omitempty"`
	Availability string `json:"availability,omitempty"`
}

type teamSummary struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type inboxesOutput struct {
	Inboxes   []inboxSummary `json:"inboxes"`
	Truncated bool           `json:"truncated"`
}

type agentsOutput struct {
	Agents    []agentSummary `json:"agents"`
	Truncated bool           `json:"truncated"`
}

type teamsOutput struct {
	Teams     []teamSummary `json:"teams"`
	Truncated bool          `json:"truncated"`
}

type labelsOutput struct {
	ConversationID int64    `json:"conversation_id"`
	PreviousLabels []string `json:"previous_labels"`
	Labels         []string `json:"labels"`
	Added          []string `json:"added,omitempty"`
	Removed        []string `json:"removed,omitempty"`
	Truncated      bool     `json:"truncated"`
}

// registerOrganizationTools adds inbox, agent and team discovery plus
// assignment and label management.
func registerOrganizationTools(server *mcp.Server, svc service.Service) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_inboxes",
		Description: "List every inbox visible to the configured user with its id, name and channel_type. Use it to choose an inbox id for create_conversation or a channel-aware action.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (*mcp.CallToolResult, result[inboxesOutput], error) {
		inboxes, err := svc.ListInboxes(ctx)
		if err != nil {
			return fail[inboxesOutput](err)
		}
		items, truncated := boundList(inboxes)
		out := make([]inboxSummary, len(items))
		textTruncated := false
		for i, inbox := range items {
			name, cut := truncateUTF8(inbox.Name, MaxTextBytes)
			channel, cutChannel := truncateUTF8(inbox.ChannelType, MaxTextBytes)
			textTruncated = textTruncated || cut || cutChannel
			out[i] = inboxSummary{ID: inbox.ID, Name: name, ChannelType: channel}
		}
		return ok(inboxesOutput{Inboxes: out, Truncated: truncated}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_agents",
		Description: "List the account agents available for assignment with their ids. Use a returned id with assign_conversation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (*mcp.CallToolResult, result[agentsOutput], error) {
		agents, err := svc.ListAgents(ctx)
		if err != nil {
			return fail[agentsOutput](err)
		}
		items, truncated := boundList(agents)
		out := make([]agentSummary, len(items))
		textTruncated := false
		for i, agent := range items {
			name, cutName := truncateUTF8(agent.Name, MaxTextBytes)
			email, cutEmail := truncateUTF8(agent.Email, MaxTextBytes)
			role, cutRole := truncateUTF8(agent.Role, MaxTextBytes)
			availability, cutAvailability := truncateUTF8(agent.Availability, MaxTextBytes)
			textTruncated = textTruncated || cutName || cutEmail || cutRole || cutAvailability
			out[i] = agentSummary{ID: agent.ID, Name: name, Email: email, Role: role, Availability: availability}
		}
		return ok(agentsOutput{Agents: out, Truncated: truncated}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_teams",
		Description: "List the account teams available for assignment with their ids. Use a returned id with assign_conversation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ listInput) (*mcp.CallToolResult, result[teamsOutput], error) {
		teams, err := svc.ListTeams(ctx)
		if err != nil {
			return fail[teamsOutput](err)
		}
		items, truncated := boundList(teams)
		out := make([]teamSummary, len(items))
		textTruncated := false
		for i, team := range items {
			name, cutName := truncateUTF8(team.Name, MaxTextBytes)
			description, cutDescription := truncateUTF8(team.Description, MaxTextBytes)
			textTruncated = textTruncated || cutName || cutDescription
			out[i] = teamSummary{ID: team.ID, Name: name, Description: description}
		}
		return ok(teamsOutput{Teams: out, Truncated: truncated}, textTruncated)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "assign_conversation",
		Description: "Assign an explicit conversation id to an agent and/or a team from list_agents and list_teams. " +
			"Unknown ids are refused; the returned assignment is the one Chatwoot stored.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in assignConversationInput) (*mcp.CallToolResult, result[service.AssignmentResult], error) {
		assigned, err := svc.AssignConversation(ctx, in.ConversationID, in.AgentID, in.TeamID)
		if err != nil {
			return fail[service.AssignmentResult](err)
		}
		return ok(assigned, false)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_conversation_labels",
		Description: "Read the complete label set of an explicit conversation id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in conversationLabelsInput) (*mcp.CallToolResult, result[labelsOutput], error) {
		labels, err := svc.GetConversationLabels(ctx, in.ConversationID)
		if err != nil {
			return fail[labelsOutput](err)
		}
		return ok(labelsResultOutput(labels), false)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "add_conversation_labels",
		Description: "Add labels to a conversation. Chatwoot replaces the whole label set, so this reads the current set, " +
			"writes the union and returns both the previous and resulting sets to make concurrent-change risk visible.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in changeConversationLabelsInput) (*mcp.CallToolResult, result[labelsOutput], error) {
		labels, err := svc.AddConversationLabels(ctx, in.ConversationID, in.Labels)
		if err != nil {
			return fail[labelsOutput](err)
		}
		return ok(labelsResultOutput(labels), false)
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "remove_conversation_labels",
		Description: "Remove labels from a conversation. Chatwoot replaces the whole label set, so this reads the current set, " +
			"writes the difference and returns both the previous and resulting sets to make concurrent-change risk visible.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in changeConversationLabelsInput) (*mcp.CallToolResult, result[labelsOutput], error) {
		labels, err := svc.RemoveConversationLabels(ctx, in.ConversationID, in.Labels)
		if err != nil {
			return fail[labelsOutput](err)
		}
		return ok(labelsResultOutput(labels), false)
	})
}

func labelsResultOutput(result service.LabelsResult) labelsOutput {
	previous, truncated := boundedLabels(result.Previous)
	labels, cutLabels := boundedLabels(result.Labels)
	added, cutAdded := boundedLabels(result.Added)
	removed, cutRemoved := boundedLabels(result.Removed)
	return labelsOutput{
		ConversationID: result.ConversationID,
		PreviousLabels: previous,
		Labels:         labels,
		Added:          added,
		Removed:        removed,
		Truncated:      truncated || cutLabels || cutAdded || cutRemoved,
	}
}

func boundedLabels(labels []string) ([]string, bool) {
	if labels == nil {
		return nil, false
	}
	items, truncated := boundList(labels)
	out := make([]string, len(items))
	for i, label := range items {
		trimmed, cut := truncateUTF8(label, MaxTextBytes)
		truncated = truncated || cut
		out[i] = trimmed
	}
	return out, truncated
}
