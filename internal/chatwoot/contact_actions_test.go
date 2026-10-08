package chatwoot

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"chatwoot-mcp/internal/core"
)

func TestGetContactReadsPayload(t *testing.T) {
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/accounts/7/contacts/99" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return jsonHTTPResponse(req, `{"payload":{"id":99,"name":"Ana","email":"ana@example.com","phone_number":"+5511","identifier":"abc","blocked":false}}`), nil
	})

	contact, err := client.GetContact(context.Background(), 99)
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	if contact.ID != 99 || contact.Name != "Ana" || contact.Phone != "+5511" || contact.Identifier != "abc" {
		t.Fatalf("contact = %#v", contact)
	}
}

func TestUpdateContactSendsOnlyProvidedFields(t *testing.T) {
	var method string
	var body map[string]any
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		method = req.Method
		_ = json.NewDecoder(req.Body).Decode(&body)
		return jsonHTTPResponse(req, `{"payload":{"id":99,"name":"Ana Maria","email":"ana@example.com"}}`), nil
	})

	name := "Ana Maria"
	contact, err := client.UpdateContact(context.Background(), core.ContactUpdate{ContactID: 99, Name: &name})
	if err != nil {
		t.Fatalf("UpdateContact: %v", err)
	}
	if method != http.MethodPut {
		t.Fatalf("method = %q", method)
	}
	if body["name"] != "Ana Maria" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["email"]; ok {
		t.Fatalf("omitted email was sent: %#v", body)
	}
	if _, ok := body["phone_number"]; ok {
		t.Fatalf("omitted phone was sent: %#v", body)
	}
	if contact.Name != "Ana Maria" {
		t.Fatalf("contact = %#v", contact)
	}
}

func TestCreateConversationPostsInboxAndContact(t *testing.T) {
	var body map[string]any
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/v1/accounts/7/conversations" {
			t.Fatalf("request = %s %s", req.Method, req.URL.Path)
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		return jsonHTTPResponse(req, `{"id":77,"inbox_id":1,"status":"open","meta":{"channel":"Channel::WebWidget"}}`), nil
	})

	conv, err := client.CreateConversation(context.Background(), core.ConversationCreateRequest{InboxID: 1, ContactID: 99})
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if body["inbox_id"] != float64(1) || body["contact_id"] != float64(99) {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["source_id"]; ok {
		t.Fatalf("empty source_id was sent: %#v", body)
	}
	if conv.ID != 77 || conv.InboxID != 1 || conv.ChannelType != "Channel::WebWidget" || conv.Status != "open" {
		t.Fatalf("conversation = %#v", conv)
	}
}

func TestCreateConversationIncludesSourceIDWhenGiven(t *testing.T) {
	var body map[string]any
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		_ = json.NewDecoder(req.Body).Decode(&body)
		return jsonHTTPResponse(req, `{"id":77,"inbox_id":1,"status":"open"}`), nil
	})

	if _, err := client.CreateConversation(context.Background(), core.ConversationCreateRequest{InboxID: 1, ContactID: 99, SourceID: "abc-123"}); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if body["source_id"] != "abc-123" {
		t.Fatalf("body = %#v", body)
	}
}
