package chatwoot

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"chatwoot-mcp/internal/core"
)

func TestSetStatusSendsStatusAndParsesReply(t *testing.T) {
	var method, path string
	var body map[string]any
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		method, path = req.Method, req.URL.Path
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return jsonHTTPResponse(req, `{"meta":{},"payload":{"success":true,"conversation_id":42,"current_status":"resolved","snoozed_until":null}}`), nil
	})
	client := NewClient(core.Settings{BaseURL: "https://chatwoot.invalid", AccountID: 7, Token: testToken}, &http.Client{Transport: transport})

	conv, err := client.SetStatus(context.Background(), core.StatusRequest{ConversationID: 42, Status: "resolved"})
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if method != http.MethodPost || path != "/api/v1/accounts/7/conversations/42/toggle_status" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["status"] != "resolved" {
		t.Fatalf("body = %#v", body)
	}
	if conv.ID != 42 || conv.Status != "resolved" {
		t.Fatalf("conversation = %#v", conv)
	}
}

func TestSetStatusForwardsSnoozedUntil(t *testing.T) {
	var body map[string]any
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(req.Body).Decode(&body)
		return jsonHTTPResponse(req, `{"payload":{"conversation_id":42,"current_status":"snoozed","snoozed_until":"2030-07-21T17:32:28Z"}}`), nil
	})
	client := NewClient(core.Settings{BaseURL: "https://chatwoot.invalid", AccountID: 7, Token: testToken}, &http.Client{Transport: transport})

	until := time.Date(2030, 7, 21, 17, 32, 28, 0, time.UTC)
	conv, err := client.SetStatus(context.Background(), core.StatusRequest{ConversationID: 42, Status: "snoozed", SnoozedUntil: &until})
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if body["snoozed_until"] != "2030-07-21T17:32:28Z" {
		t.Fatalf("snoozed_until = %#v", body["snoozed_until"])
	}
	if conv.SnoozedUntil != "2030-07-21T17:32:28Z" {
		t.Fatalf("conversation = %#v", conv)
	}
}

func TestSetPriorityPostsThenReadsBack(t *testing.T) {
	var paths []string
	var body map[string]any
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.Method {
		case http.MethodPost:
			_ = json.NewDecoder(req.Body).Decode(&body)
			return jsonHTTPResponse(req, ``), nil
		case http.MethodGet:
			return jsonHTTPResponse(req, `{"id":42,"priority":"high"}`), nil
		default:
			t.Fatalf("unexpected method %s", req.Method)
			return nil, nil
		}
	})
	client := NewClient(core.Settings{BaseURL: "https://chatwoot.invalid", AccountID: 7, Token: testToken}, &http.Client{Transport: transport})

	conv, err := client.SetPriority(context.Background(), core.PriorityRequest{ConversationID: 42, Priority: "high"})
	if err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/accounts/7/conversations/42/toggle_priority" || paths[1] != "/api/v1/accounts/7/conversations/42" {
		t.Fatalf("paths = %v", paths)
	}
	if body["priority"] != "high" {
		t.Fatalf("body = %#v", body)
	}
	if conv.Priority != "high" {
		t.Fatalf("conversation = %#v", conv)
	}
}
