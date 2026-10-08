package chatwoot

import (
	"context"
	"net/http"
	"testing"
)

func TestListTemplatesReadsPayload(t *testing.T) {
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/accounts/7/inboxes/5/message_templates" {
			t.Fatalf("path = %q", req.URL.Path)
		}
		return jsonHTTPResponse(req, `{"payload":[{"name":"welcome","language":"en_US","category":"UTILITY","status":"APPROVED","components":[{"type":"BODY","text":"Hello {{1}}"}]}],"meta":{}}`), nil
	})

	templates, err := client.ListTemplates(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("templates = %#v", templates)
	}
	template := templates[0]
	if template.Name != "welcome" || template.Language != "en_US" || template.Status != "APPROVED" || template.Body != "Hello {{1}}" {
		t.Fatalf("template = %#v", template)
	}
}

func TestListTemplatesNormalizesFriendlyName(t *testing.T) {
	client := newRoundTripClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(req, `{"payload":[{"friendly_name":"order_update","language":"pt_BR","body":"Pedido atualizado"}]}`), nil
	})

	templates, err := client.ListTemplates(context.Background(), 5)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 1 || templates[0].Name != "order_update" || templates[0].Body != "Pedido atualizado" {
		t.Fatalf("templates = %#v", templates)
	}
}
