package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"chatwoot-mcp/internal/core"
)

const testToken = "secret"

const conversationJSON = `{"id":11,"inbox_id":5,"contact_id":0,"status":"open",` +
	`"can_reply":true,"meta":{"sender":{"id":99}},` +
	`"messages":[{"id":1,"content":"hi","private":false,"status":"sent","created_at":1700000000}]}`

const contactJSON = `{"id":99,"name":"Ana","email":"ana@example.com","phone_number":"+5511555"}`

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(core.Settings{BaseURL: srv.URL, AccountID: 7, Token: testToken}, srv.Client())
	return c, srv
}

func requireKind(t *testing.T, err error, want Kind) *Error {
	t.Helper()
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not *Error", err)
	}
	if apiErr.Kind != want {
		t.Fatalf("kind = %q, want %q (err=%v)", apiErr.Kind, want, err)
	}
	return apiErr
}

func TestCheck(t *testing.T) {
	var paths []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("api_access_token"); got != testToken {
			t.Errorf("token header = %q on %s", got, r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept = %q", got)
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/accounts/7":
			_, _ = w.Write([]byte(`{"id":7,"name":"Acme Support"}`))
		case "/api/v1/profile":
			_, _ = w.Write([]byte(`{"id":3,"name":"Ana","email":"ana@example.com"}`))
		default:
			http.NotFound(w, r)
		}
	})

	id, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if id.AccountID != 7 || id.AccountName != "Acme Support" || id.UserName != "Ana" {
		t.Fatalf("identity = %#v", id)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v, want account and profile", paths)
	}
}

func TestListConversations(t *testing.T) {
	t.Run("maps payload and reports next page from all_count", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/api/v1/accounts/7/conversations" {
				t.Errorf("path = %q", got)
			}
			if got := r.Header.Get("api_access_token"); got != testToken {
				t.Errorf("token header = %q", got)
			}
			q := r.URL.Query()
			if q.Get("status") != "open" || q.Get("inbox_id") != "5" || q.Get("page") != "2" {
				t.Errorf("query = %v", q)
			}
			_, _ = w.Write([]byte(`{"data":{"meta":{"mine_count":1,"assigned_count":1,"unassigned_count":0,"all_count":60},"payload":[` + conversationJSON + `]}}`))
		})

		page, err := c.ListConversations(context.Background(), core.ListOptions{Page: 2, Status: "open", InboxID: 5})
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("items = %d", len(page.Items))
		}
		got := page.Items[0]
		if got.ID != 11 || got.InboxID != 5 || got.ContactID != 99 || got.Status != "open" || !got.CanReply {
			t.Fatalf("conversation = %#v", got)
		}
		if len(got.Messages) != 1 || got.Messages[0].ID != 1 || got.Messages[0].CreatedAt != 1700000000 {
			t.Fatalf("messages = %#v", got.Messages)
		}
		if page.NextPage != 3 {
			t.Fatalf("next page = %d, want 3", page.NextPage)
		}
	})

	t.Run("last page reports no next page", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{"meta":{"all_count":30},"payload":[` + conversationJSON + `]}}`))
		})
		page, err := c.ListConversations(context.Background(), core.ListOptions{Page: 2})
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		if page.NextPage != 0 {
			t.Fatalf("next page = %d, want 0", page.NextPage)
		}
	})

	t.Run("falls back to payload length when meta is absent", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			items := make([]any, 0, conversationPageSize)
			for i := 0; i < conversationPageSize; i++ {
				items = append(items, json.RawMessage(conversationJSON))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"payload": items},
			})
		})
		page, err := c.ListConversations(context.Background(), core.ListOptions{Page: 1})
		if err != nil {
			t.Fatalf("ListConversations: %v", err)
		}
		if page.NextPage != 2 {
			t.Fatalf("next page = %d, want 2", page.NextPage)
		}
	})
}

func TestGetConversation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v1/accounts/7/conversations/42" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("api_access_token"); got != testToken {
			t.Errorf("token header = %q", got)
		}
		_, _ = w.Write([]byte(conversationJSON))
	})

	conv, err := c.GetConversation(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if conv.ID != 11 || conv.ContactID != 99 || conv.InboxID != 5 || !conv.CanReply || conv.Status != "open" {
		t.Fatalf("conversation = %#v", conv)
	}
	if len(conv.Messages) != 1 || conv.Messages[0].Content != "hi" || conv.Messages[0].Status != "sent" {
		t.Fatalf("messages = %#v", conv.Messages)
	}
}

func TestSearchContacts(t *testing.T) {
	t.Run("has_more drives next page", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/api/v1/accounts/7/contacts/search" {
				t.Errorf("path = %q", got)
			}
			q := r.URL.Query()
			if q.Get("q") != "ana" || q.Get("page") != "1" {
				t.Errorf("query = %v", q)
			}
			_, _ = w.Write([]byte(`{"meta":{"count":20,"current_page":1,"has_more":true},"payload":[` + contactJSON + `]}`))
		})
		page, err := c.SearchContacts(context.Background(), "ana", 1)
		if err != nil {
			t.Fatalf("SearchContacts: %v", err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("items = %d", len(page.Items))
		}
		got := page.Items[0]
		if got.ID != 99 || got.Name != "Ana" || got.Email != "ana@example.com" || got.Phone != "+5511555" {
			t.Fatalf("contact = %#v", got)
		}
		if page.NextPage != 2 {
			t.Fatalf("next page = %d, want 2", page.NextPage)
		}
	})

	t.Run("has_more false stops pagination", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"meta":{"count":1,"current_page":1,"has_more":false},"payload":[` + contactJSON + `]}`))
		})
		page, err := c.SearchContacts(context.Background(), "ana", 1)
		if err != nil {
			t.Fatalf("SearchContacts: %v", err)
		}
		if page.NextPage != 0 {
			t.Fatalf("next page = %d, want 0", page.NextPage)
		}
	})

	t.Run("derives next page from count when has_more is absent", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"meta":{"count":20,"current_page":1},"payload":[` + contactJSON + `]}`))
		})
		page, err := c.SearchContacts(context.Background(), "ana", 1)
		if err != nil {
			t.Fatalf("SearchContacts: %v", err)
		}
		if page.NextPage != 2 {
			t.Fatalf("next page = %d, want 2", page.NextPage)
		}
	})
}

func TestContactConversations(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/v1/accounts/7/contacts/99/conversations" {
			t.Errorf("path = %q", got)
		}
		_, _ = w.Write([]byte(`{"payload":[` + conversationJSON + `]}`))
	})
	page, err := c.ContactConversations(context.Background(), 99, 1)
	if err != nil {
		t.Fatalf("ContactConversations: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 11 || page.Items[0].ContactID != 99 {
		t.Fatalf("items = %#v", page.Items)
	}
	if page.NextPage != 0 {
		t.Fatalf("next page = %d, want 0", page.NextPage)
	}
}

func TestCreateMessage(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if got := r.URL.Path; got != "/api/v1/accounts/7/conversations/42/messages" {
			t.Errorf("path = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["content"] != "Olá" || body["message_type"] != "outgoing" {
			t.Errorf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"id":500,"content":"Olá","private":false,"status":"sent","created_at":1700000001}`))
	})

	msg, err := c.CreateMessage(context.Background(), 42, "Olá")
	if err != nil {
		t.Fatalf("CreateMessage: %v", err)
	}
	if msg.ID != 500 || msg.Content != "Olá" || msg.Status != "sent" || msg.CreatedAt != 1700000001 {
		t.Fatalf("message = %#v", msg)
	}
	if calls != 1 {
		t.Fatalf("create calls = %d, want 1", calls)
	}
}

func TestCreateMessageNeverRetries(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"temporarily unavailable"}`))
			})
			_, err := c.CreateMessage(context.Background(), 42, "Olá")
			if err == nil {
				t.Fatal("expected error")
			}
			if calls != 1 {
				t.Fatalf("create calls = %d, want exactly 1 (no auto retry)", calls)
			}
		})
	}
}

func TestTypedErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		header map[string]string
		kind   Kind
	}{
		{"unauthorized", 401, `{"error":"Invalid Access Token"}`, nil, KindUnauthorized},
		{"forbidden", 403, `{"error":"You are not authorized to do this action"}`, nil, KindForbidden},
		{"not found", 404, `{"error":"Conversation not found"}`, nil, KindNotFound},
		{"rate limited", 429, ``, map[string]string{"Retry-After": "7"}, KindRateLimited},
		{"server error", 500, `{"error":"Internal Server Error"}`, nil, KindServer},
		{"bad request", 400, `{"description":"Bad Request","errors":[{"message":"invalid conversation"}]}`, nil, KindRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.GetConversation(context.Background(), 42)
			apiErr := requireKind(t, err, tc.kind)
			if apiErr.StatusCode != tc.status {
				t.Fatalf("status code = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Fatalf("error leaked the token: %v", err)
			}
			if tc.kind == KindNotFound {
				if !strings.Contains(err.Error(), "conversation 42") {
					t.Fatalf("error = %v, want the requested conversation id", err)
				}
				if !IsNotFound(err) {
					t.Fatal("IsNotFound = false")
				}
			}
			if tc.kind == KindRateLimited && apiErr.RetryAfter != 7*time.Second {
				t.Fatalf("retry after = %v, want 7s", apiErr.RetryAfter)
			}
		})
	}
}

func TestErrorPredicates(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Invalid Access Token"}`))
	})
	_, err := c.Check(context.Background())
	if !IsUnauthorized(err) {
		t.Fatalf("IsUnauthorized = false for %v", err)
	}
	if IsForbidden(err) || IsNotFound(err) || IsRateLimited(err) || IsTimeout(err) || IsServerError(err) {
		t.Fatalf("unexpected predicate match for %v", err)
	}
}

func TestTimeout(t *testing.T) {
	t.Run("http client timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := NewClient(core.Settings{BaseURL: srv.URL, AccountID: 7, Token: testToken}, &http.Client{Timeout: 30 * time.Millisecond})
		_, err := c.ListConversations(context.Background(), core.ListOptions{})
		requireKind(t, err, KindTimeout)
		if !IsTimeout(err) {
			t.Fatalf("IsTimeout = false for %v", err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		c := NewClient(core.Settings{BaseURL: srv.URL, AccountID: 7, Token: testToken}, srv.Client())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := c.GetConversation(ctx, 42)
		requireKind(t, err, KindTimeout)
	})
}

func TestBaseURLPrefixAndTrailingSlash(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		_, _ = w.Write([]byte(conversationJSON))
	}))
	defer srv.Close()
	c := NewClient(core.Settings{BaseURL: srv.URL + "/chatwoot/", AccountID: 7, Token: testToken}, srv.Client())
	if _, err := c.GetConversation(context.Background(), 42); err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got != "/chatwoot/api/v1/accounts/7/conversations/42" {
		t.Fatalf("path = %q, want /chatwoot/api/v1/accounts/7/conversations/42", got)
	}
}

func TestErrorStringIncludesResource(t *testing.T) {
	err := &Error{Kind: KindNotFound, StatusCode: 404, Resource: "conversation 42", Message: "Conversation not found"}
	if got := err.Error(); !strings.Contains(got, "conversation 42") || !strings.Contains(got, "not found") {
		t.Fatalf("error = %q", got)
	}
	if fmt.Sprintf("%v", err) == "" {
		t.Fatal("empty error string")
	}
}
