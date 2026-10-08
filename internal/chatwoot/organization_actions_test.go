package chatwoot

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"chatwoot-mcp/internal/core"
)

func newRoundTripClient(t *testing.T, fn roundTripFunc) *Client {
	t.Helper()
	return NewClient(core.Settings{BaseURL: "https://chatwoot.invalid", AccountID: 7, Token: testToken}, &http.Client{Transport: fn})
}

func TestListInboxesReadsPayload(t *testing.T) {
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/accounts/7/inboxes" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return jsonHTTPResponse(req, `{"payload":[{"id":1,"name":"Site","channel_type":"Channel::WebWidget"},{"id":2,"name":"Whats","channel_type":"Channel::Whatsapp"}]}`), nil
	})

	inboxes, err := client.ListInboxes(context.Background())
	if err != nil {
		t.Fatalf("ListInboxes: %v", err)
	}
	if len(inboxes) != 2 || inboxes[1].ChannelType != "Channel::Whatsapp" {
		t.Fatalf("inboxes = %#v", inboxes)
	}
}

func TestListAgentsAndTeamsReadBareArrays(t *testing.T) {
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/accounts/7/agents":
			return jsonHTTPResponse(req, `[{"id":5,"name":"Ana","email":"ana@example.com","role":"agent","availability_status":"online"}]`), nil
		case "/api/v1/accounts/7/teams":
			return jsonHTTPResponse(req, `[{"id":3,"name":"Suporte","description":"time"}]`), nil
		default:
			t.Fatalf("unexpected path %q", req.URL.Path)
			return nil, nil
		}
	})

	agents, err := client.ListAgents(context.Background())
	if err != nil || len(agents) != 1 || agents[0].ID != 5 || agents[0].Availability != "online" {
		t.Fatalf("agents = %#v, %v", agents, err)
	}
	teams, err := client.ListTeams(context.Background())
	if err != nil || len(teams) != 1 || teams[0].ID != 3 {
		t.Fatalf("teams = %#v, %v", teams, err)
	}
}

func TestAssignSendsEachTargetOnceAndReadsBack(t *testing.T) {
	var posts int
	var lastBody map[string]any
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		switch req.Method {
		case http.MethodPost:
			posts++
			if req.URL.Path != "/api/v1/accounts/7/conversations/42/assignments" {
				t.Fatalf("path = %q", req.URL.Path)
			}
			_ = json.NewDecoder(req.Body).Decode(&lastBody)
			return jsonHTTPResponse(req, `{}`), nil
		case http.MethodGet:
			return jsonHTTPResponse(req, `{"id":42,"meta":{"assignee":{"id":5},"team":{"id":3}}}`), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})

	conv, err := client.Assign(context.Background(), core.AssignmentRequest{ConversationID: 42, AgentID: 5, TeamID: 3})
	if err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if posts != 2 {
		t.Fatalf("assignment posts = %d, want 2 (one per target)", posts)
	}
	if lastBody["team_id"] != float64(3) {
		t.Fatalf("last body = %#v", lastBody)
	}
	if conv.AssigneeID != 5 || conv.TeamID != 3 {
		t.Fatalf("conversation = %#v", conv)
	}
}

func TestGetAndSetLabels(t *testing.T) {
	var postBody map[string]any
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/accounts/7/conversations/42/labels" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		if req.Method == http.MethodPost {
			_ = json.NewDecoder(req.Body).Decode(&postBody)
			return jsonHTTPResponse(req, `{"payload":["vip","urgent"]}`), nil
		}
		return jsonHTTPResponse(req, `{"payload":["vip"]}`), nil
	})

	labels, err := client.GetLabels(context.Background(), 42)
	if err != nil || len(labels) != 1 || labels[0] != "vip" {
		t.Fatalf("GetLabels = %#v, %v", labels, err)
	}
	resulting, err := client.SetLabels(context.Background(), core.LabelsRequest{ConversationID: 42, Labels: []string{"vip", "urgent"}})
	if err != nil || len(resulting) != 2 {
		t.Fatalf("SetLabels = %#v, %v", resulting, err)
	}
	if raw, ok := postBody["labels"].([]any); !ok || len(raw) != 2 {
		t.Fatalf("post body = %#v", postBody)
	}
}
