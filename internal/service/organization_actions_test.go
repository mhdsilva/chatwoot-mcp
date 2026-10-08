package service

import (
	"context"
	"testing"

	"chatwoot-mcp/internal/core"
)

func TestListInboxesAgentsTeamsPassThrough(t *testing.T) {
	fake := &fakeAPI{
		inboxes: []core.Inbox{{ID: 1, Name: "Site", ChannelType: "Channel::WebWidget"}},
		agents:  []core.Agent{{ID: 5, Name: "Ana"}},
		teams:   []core.Team{{ID: 3, Name: "Suporte"}},
	}
	svc := New(fake)

	if inboxes, err := svc.ListInboxes(context.Background()); err != nil || len(inboxes) != 1 || inboxes[0].ID != 1 {
		t.Fatalf("ListInboxes = %#v, %v", inboxes, err)
	}
	if agents, err := svc.ListAgents(context.Background()); err != nil || len(agents) != 1 || agents[0].ID != 5 {
		t.Fatalf("ListAgents = %#v, %v", agents, err)
	}
	if teams, err := svc.ListTeams(context.Background()); err != nil || len(teams) != 1 || teams[0].ID != 3 {
		t.Fatalf("ListTeams = %#v, %v", teams, err)
	}
}

func TestAssignConversationValidatesTargetsAndReturnsStored(t *testing.T) {
	fake := &fakeAPI{
		getConv:    core.Conversation{ID: 42},
		agents:     []core.Agent{{ID: 5}},
		teams:      []core.Team{{ID: 3}},
		assignConv: core.Conversation{ID: 42, AssigneeID: 5, TeamID: 3},
	}
	result, err := New(fake).AssignConversation(context.Background(), 42, 5, 3)
	if err != nil {
		t.Fatalf("AssignConversation: %v", err)
	}
	if len(fake.assignReqs) != 1 || fake.assignReqs[0].AgentID != 5 || fake.assignReqs[0].TeamID != 3 {
		t.Fatalf("assign requests = %#v", fake.assignReqs)
	}
	if result.ConversationID != 42 || result.AgentID != 5 || result.TeamID != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestAssignConversationRejectsUnknownAgent(t *testing.T) {
	fake := &fakeAPI{getConv: core.Conversation{ID: 42}, agents: []core.Agent{{ID: 9}}}
	_, err := New(fake).AssignConversation(context.Background(), 42, 5, 0)
	requireCode(t, err, CodeNotFound)
	if len(fake.assignReqs) != 0 {
		t.Fatalf("assigned an unknown agent")
	}
}

func TestAssignConversationRejectsUnknownTeam(t *testing.T) {
	fake := &fakeAPI{getConv: core.Conversation{ID: 42}, teams: []core.Team{{ID: 9}}}
	_, err := New(fake).AssignConversation(context.Background(), 42, 0, 3)
	requireCode(t, err, CodeNotFound)
	if len(fake.assignReqs) != 0 {
		t.Fatalf("assigned an unknown team")
	}
}

func TestAssignConversationRequiresATarget(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).AssignConversation(context.Background(), 42, 0, 0)
	requireCode(t, err, CodeInvalidInput)
}

func TestAssignConversationRejectsConversationMismatch(t *testing.T) {
	fake := &fakeAPI{getConv: core.Conversation{ID: 99}, agents: []core.Agent{{ID: 5}}}
	_, err := New(fake).AssignConversation(context.Background(), 42, 5, 0)
	requireCode(t, err, CodeConversationMismatch)
	if len(fake.assignReqs) != 0 {
		t.Fatalf("assigned despite a conversation mismatch")
	}
}

func TestAddConversationLabelsWritesUnion(t *testing.T) {
	fake := &fakeAPI{getLabels: []string{"vip"}, setLabels: []string{"vip", "urgent"}}
	result, err := New(fake).AddConversationLabels(context.Background(), 42, []string{"urgent"})
	if err != nil {
		t.Fatalf("AddConversationLabels: %v", err)
	}
	if len(fake.setLabelsReqs) != 1 || len(fake.setLabelsReqs[0].Labels) != 2 || fake.setLabelsReqs[0].Labels[0] != "vip" || fake.setLabelsReqs[0].Labels[1] != "urgent" {
		t.Fatalf("set labels = %#v, want [vip urgent]", fake.setLabelsReqs)
	}
	if len(result.Previous) != 1 || result.Previous[0] != "vip" || len(result.Added) != 1 || result.Added[0] != "urgent" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRemoveConversationLabelsWritesDifference(t *testing.T) {
	fake := &fakeAPI{getLabels: []string{"vip", "urgent"}, setLabels: []string{"urgent"}}
	result, err := New(fake).RemoveConversationLabels(context.Background(), 42, []string{"vip"})
	if err != nil {
		t.Fatalf("RemoveConversationLabels: %v", err)
	}
	if len(fake.setLabelsReqs) != 1 || len(fake.setLabelsReqs[0].Labels) != 1 || fake.setLabelsReqs[0].Labels[0] != "urgent" {
		t.Fatalf("set labels = %#v, want [urgent]", fake.setLabelsReqs)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "vip" {
		t.Fatalf("result = %#v", result)
	}
}

func TestAddConversationLabelsSkipsWriteWhenUnchanged(t *testing.T) {
	fake := &fakeAPI{getLabels: []string{"vip"}}
	result, err := New(fake).AddConversationLabels(context.Background(), 42, []string{"vip"})
	if err != nil {
		t.Fatalf("AddConversationLabels: %v", err)
	}
	if len(fake.setLabelsReqs) != 0 {
		t.Fatalf("wrote labels with no change")
	}
	if len(result.Labels) != 1 || result.Labels[0] != "vip" {
		t.Fatalf("result = %#v", result)
	}
}

func TestChangeConversationLabelsRejectsEmpty(t *testing.T) {
	fake := &fakeAPI{}
	_, err := New(fake).AddConversationLabels(context.Background(), 42, []string{"  ", ""})
	requireCode(t, err, CodeInvalidInput)
	if fake.getLabelsCalls != 0 {
		t.Fatalf("read labels for an empty add")
	}
}

func TestGetConversationLabels(t *testing.T) {
	fake := &fakeAPI{getLabels: []string{"vip"}}
	result, err := New(fake).GetConversationLabels(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetConversationLabels: %v", err)
	}
	if len(result.Labels) != 1 || result.Labels[0] != "vip" {
		t.Fatalf("result = %#v", result)
	}
}
