// Package service organization actions: inbox, agent and team discovery,
// conversation assignment and label management. Assignment ids are validated
// against the configured account. Because Chatwoot's label endpoint replaces
// the whole set, add and remove read the current set, compute the new one and
// return both so concurrent changes are visible.
package service

import (
	"context"
	"fmt"
	"strings"

	"chatwoot-mcp/internal/core"
)

// AssignmentResult reports the assignment Chatwoot stored.
type AssignmentResult struct {
	ConversationID int64 `json:"conversation_id"`
	AgentID        int64 `json:"assignee_id,omitempty"`
	TeamID         int64 `json:"team_id,omitempty"`
}

// LabelsResult reports the previous and resulting label sets. Added and
// Removed list only the difference for add and remove operations.
type LabelsResult struct {
	ConversationID int64    `json:"conversation_id"`
	Previous       []string `json:"previous_labels"`
	Labels         []string `json:"labels"`
	Added          []string `json:"added,omitempty"`
	Removed        []string `json:"removed,omitempty"`
}

// ListInboxes lists every inbox visible to the configured user.
func (s *service) ListInboxes(ctx context.Context) ([]core.Inbox, error) {
	inboxes, err := s.api.ListInboxes(ctx)
	if err != nil {
		return nil, fromAPI(err)
	}
	return inboxes, nil
}

// ListAgents lists the account agents available for assignment.
func (s *service) ListAgents(ctx context.Context) ([]core.Agent, error) {
	agents, err := s.api.ListAgents(ctx)
	if err != nil {
		return nil, fromAPI(err)
	}
	return agents, nil
}

// ListTeams lists the account teams available for assignment.
func (s *service) ListTeams(ctx context.Context) ([]core.Team, error) {
	teams, err := s.api.ListTeams(ctx)
	if err != nil {
		return nil, fromAPI(err)
	}
	return teams, nil
}

// AssignConversation validates the conversation, agent and team against the
// configured account before assigning, then returns the stored assignment.
func (s *service) AssignConversation(ctx context.Context, conversationID, agentID, teamID int64) (AssignmentResult, error) {
	if conversationID <= 0 {
		return AssignmentResult{}, invalidInput("conversation id must be a positive integer")
	}
	if agentID <= 0 && teamID <= 0 {
		return AssignmentResult{}, invalidInput("provide a positive agent_id or team_id to assign")
	}

	conv, err := s.api.GetConversation(ctx, conversationID)
	if err != nil {
		return AssignmentResult{}, fromAPI(err)
	}
	if conv.ID != conversationID {
		return AssignmentResult{}, &Error{
			Code:    CodeConversationMismatch,
			Message: fmt.Sprintf("requested conversation %d but the API returned conversation %d", conversationID, conv.ID),
		}
	}

	if agentID > 0 {
		if err := s.requireAgent(ctx, agentID); err != nil {
			return AssignmentResult{}, err
		}
	}
	if teamID > 0 {
		if err := s.requireTeam(ctx, teamID); err != nil {
			return AssignmentResult{}, err
		}
	}

	assigned, err := s.api.Assign(ctx, core.AssignmentRequest{ConversationID: conversationID, AgentID: agentID, TeamID: teamID})
	if err != nil {
		return AssignmentResult{}, fromAPI(err)
	}
	result := AssignmentResult{ConversationID: conversationID, AgentID: assigned.AssigneeID, TeamID: assigned.TeamID}
	if result.AgentID == 0 {
		result.AgentID = agentID
	}
	if result.TeamID == 0 {
		result.TeamID = teamID
	}
	return result, nil
}

func (s *service) requireAgent(ctx context.Context, agentID int64) error {
	agents, err := s.api.ListAgents(ctx)
	if err != nil {
		return fromAPI(err)
	}
	for _, agent := range agents {
		if agent.ID == agentID {
			return nil
		}
	}
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf("agent %d is not available in the configured account", agentID)}
}

func (s *service) requireTeam(ctx context.Context, teamID int64) error {
	teams, err := s.api.ListTeams(ctx)
	if err != nil {
		return fromAPI(err)
	}
	for _, team := range teams {
		if team.ID == teamID {
			return nil
		}
	}
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf("team %d is not available in the configured account", teamID)}
}

// GetConversationLabels returns the conversation's current label set.
func (s *service) GetConversationLabels(ctx context.Context, conversationID int64) (LabelsResult, error) {
	if conversationID <= 0 {
		return LabelsResult{}, invalidInput("conversation id must be a positive integer")
	}
	labels, err := s.api.GetLabels(ctx, conversationID)
	if err != nil {
		return LabelsResult{}, fromAPI(err)
	}
	return LabelsResult{ConversationID: conversationID, Labels: labels}, nil
}

// AddConversationLabels reads the current set, adds the requested labels and
// writes the union. It returns the previous and resulting sets.
func (s *service) AddConversationLabels(ctx context.Context, conversationID int64, labels []string) (LabelsResult, error) {
	if conversationID <= 0 {
		return LabelsResult{}, invalidInput("conversation id must be a positive integer")
	}
	add := normalizeLabels(labels)
	if len(add) == 0 {
		return LabelsResult{}, invalidInput("provide at least one non-empty label to add")
	}

	previous, err := s.api.GetLabels(ctx, conversationID)
	if err != nil {
		return LabelsResult{}, fromAPI(err)
	}
	desired, added := unionLabels(previous, add)
	if len(added) == 0 {
		return LabelsResult{ConversationID: conversationID, Previous: previous, Labels: previous}, nil
	}
	resulting, err := s.api.SetLabels(ctx, core.LabelsRequest{ConversationID: conversationID, Labels: desired})
	if err != nil {
		return LabelsResult{}, fromAPI(err)
	}
	return LabelsResult{ConversationID: conversationID, Previous: previous, Labels: resulting, Added: added}, nil
}

// RemoveConversationLabels reads the current set, removes the requested labels
// and writes the difference. It returns the previous and resulting sets.
func (s *service) RemoveConversationLabels(ctx context.Context, conversationID int64, labels []string) (LabelsResult, error) {
	if conversationID <= 0 {
		return LabelsResult{}, invalidInput("conversation id must be a positive integer")
	}
	remove := normalizeLabels(labels)
	if len(remove) == 0 {
		return LabelsResult{}, invalidInput("provide at least one non-empty label to remove")
	}

	previous, err := s.api.GetLabels(ctx, conversationID)
	if err != nil {
		return LabelsResult{}, fromAPI(err)
	}
	desired, removed := subtractLabels(previous, remove)
	if len(removed) == 0 {
		return LabelsResult{ConversationID: conversationID, Previous: previous, Labels: previous}, nil
	}
	resulting, err := s.api.SetLabels(ctx, core.LabelsRequest{ConversationID: conversationID, Labels: desired})
	if err != nil {
		return LabelsResult{}, fromAPI(err)
	}
	return LabelsResult{ConversationID: conversationID, Previous: previous, Labels: resulting, Removed: removed}, nil
}

// normalizeLabels trims whitespace, drops empty labels and de-duplicates while
// preserving order.
func normalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	seen := make(map[string]bool, len(labels))
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func unionLabels(base, add []string) (result, added []string) {
	seen := make(map[string]bool, len(base)+len(add))
	result = make([]string, 0, len(base)+len(add))
	for _, label := range base {
		if !seen[label] {
			seen[label] = true
			result = append(result, label)
		}
	}
	for _, label := range add {
		if !seen[label] {
			seen[label] = true
			result = append(result, label)
			added = append(added, label)
		}
	}
	return result, added
}

func subtractLabels(base, remove []string) (result, removed []string) {
	drop := make(map[string]bool, len(remove))
	for _, label := range remove {
		drop[label] = true
	}
	result = make([]string, 0, len(base))
	for _, label := range base {
		if drop[label] {
			removed = append(removed, label)
			continue
		}
		result = append(result, label)
	}
	return result, removed
}
