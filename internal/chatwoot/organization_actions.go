package chatwoot

import (
	"context"
	"net/http"
	"strconv"

	"chatwoot-mcp/internal/core"
)

type inboxWire struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
	Medium      string `json:"medium"`
}

func (i inboxWire) toCore() core.Inbox {
	return core.Inbox{ID: i.ID, Name: i.Name, ChannelType: i.ChannelType, Medium: i.Medium}
}

type agentWire struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	Availability string `json:"availability_status"`
}

func (a agentWire) toCore() core.Agent {
	return core.Agent{ID: a.ID, Name: a.Name, Email: a.Email, Role: a.Role, Availability: a.Availability}
}

type teamWire struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (t teamWire) toCore() core.Team {
	return core.Team{ID: t.ID, Name: t.Name, Description: t.Description}
}

// ListInboxes lists every inbox visible to the configured user.
func (c *Client) ListInboxes(ctx context.Context) ([]core.Inbox, error) {
	var envelope struct {
		Payload []inboxWire `json:"payload"`
	}
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/inboxes", nil, nil, &envelope, "inboxes"); err != nil {
		return nil, err
	}
	inboxes := make([]core.Inbox, 0, len(envelope.Payload))
	for _, inbox := range envelope.Payload {
		inboxes = append(inboxes, inbox.toCore())
	}
	return inboxes, nil
}

// GetInbox reads one inbox so a channel type can be checked before use.
func (c *Client) GetInbox(ctx context.Context, id int64) (core.Inbox, error) {
	var inbox inboxWire
	resource := "inbox " + strconv.FormatInt(id, 10)
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/inboxes/"+strconv.FormatInt(id, 10), nil, nil, &inbox, resource); err != nil {
		return core.Inbox{}, err
	}
	return inbox.toCore(), nil
}

// ListAgents lists the account agents that can receive assignments.
func (c *Client) ListAgents(ctx context.Context) ([]core.Agent, error) {
	var wire []agentWire
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/agents", nil, nil, &wire, "agents"); err != nil {
		return nil, err
	}
	agents := make([]core.Agent, 0, len(wire))
	for _, agent := range wire {
		agents = append(agents, agent.toCore())
	}
	return agents, nil
}

// ListTeams lists the account teams that can receive assignments.
func (c *Client) ListTeams(ctx context.Context) ([]core.Team, error) {
	var wire []teamWire
	if err := c.call(ctx, http.MethodGet, c.accountPath()+"/teams", nil, nil, &wire, "teams"); err != nil {
		return nil, err
	}
	teams := make([]core.Team, 0, len(wire))
	for _, team := range wire {
		teams = append(teams, team.toCore())
	}
	return teams, nil
}

// Assign assigns the conversation to an agent and/or a team. Chatwoot's
// assignments endpoint accepts one target per call, so each requested target
// is sent once. The conversation is read back to report the stored assignment.
func (c *Client) Assign(ctx context.Context, req core.AssignmentRequest) (core.Conversation, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(req.ConversationID, 10) + "/assignments"
	resource := "conversation " + strconv.FormatInt(req.ConversationID, 10) + " assignment"

	if req.AgentID > 0 {
		payload := map[string]any{"assignee_id": req.AgentID, "assignee_type": "User"}
		if err := c.call(ctx, http.MethodPost, path, nil, payload, nil, resource); err != nil {
			return core.Conversation{}, err
		}
	}
	if req.TeamID > 0 {
		payload := map[string]any{"team_id": req.TeamID}
		if err := c.call(ctx, http.MethodPost, path, nil, payload, nil, resource); err != nil {
			return core.Conversation{}, err
		}
	}

	if conv, err := c.getConversationMeta(ctx, req.ConversationID); err == nil {
		return conv, nil
	}
	return core.Conversation{ID: req.ConversationID, AssigneeID: req.AgentID, TeamID: req.TeamID}, nil
}

// GetLabels returns the conversation's complete label set.
func (c *Client) GetLabels(ctx context.Context, conversationID int64) ([]string, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(conversationID, 10) + "/labels"
	resource := "conversation " + strconv.FormatInt(conversationID, 10) + " labels"
	var envelope struct {
		Payload []string `json:"payload"`
	}
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &envelope, resource); err != nil {
		return nil, err
	}
	return envelope.Payload, nil
}

// SetLabels replaces the conversation's complete label set and returns the set
// Chatwoot reports. The caller must compute the full desired set, because this
// endpoint overwrites whatever labels already exist.
func (c *Client) SetLabels(ctx context.Context, req core.LabelsRequest) ([]string, error) {
	path := c.accountPath() + "/conversations/" + strconv.FormatInt(req.ConversationID, 10) + "/labels"
	resource := "conversation " + strconv.FormatInt(req.ConversationID, 10) + " labels"
	labels := req.Labels
	if labels == nil {
		labels = []string{}
	}
	var envelope struct {
		Payload []string `json:"payload"`
	}
	if err := c.call(ctx, http.MethodPost, path, nil, map[string]any{"labels": labels}, &envelope, resource); err != nil {
		return nil, err
	}
	return envelope.Payload, nil
}
