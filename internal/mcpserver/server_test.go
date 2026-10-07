package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/service"
)

// Run must keep this exact public shape so the mcp command can wire it.
var _ func(context.Context, service.Service, io.Reader, io.Writer) error = Run

var wantToolNames = []string{
	"check_connection",
	"get_contact_conversations",
	"get_conversation",
	"list_conversations",
	"search_contacts",
	"send_reply",
}

var wantRequired = map[string][]string{
	"check_connection":          nil,
	"list_conversations":        nil,
	"get_conversation":          {"conversation_id"},
	"search_contacts":           {"query"},
	"get_contact_conversations": {"contact_id"},
	"send_reply":                {"conversation_id", "content"},
}

type fakeService struct {
	identity  core.Identity
	checkErr  error
	checkCall int

	listPage  core.Page[core.Conversation]
	listErr   error
	listCalls int
	listOpts  core.ListOptions

	getResult service.ConversationResult
	getErr    error
	getCalls  int
	getIDs    []int64

	searchPage    core.Page[core.Contact]
	searchErr     error
	searchCalls   int
	searchQuery   string
	searchPageArg int

	contactPage    core.Page[core.Conversation]
	contactErr     error
	contactCalls   int
	contactID      int64
	contactPageArg int

	sendResult service.SendResult
	sendErr    error
	sendCalls  int
	sendIDs    []int64
	sendTexts  []string
}

func (f *fakeService) CheckConnection(context.Context) (core.Identity, error) {
	f.checkCall++
	return f.identity, f.checkErr
}

func (f *fakeService) ListConversations(_ context.Context, opts core.ListOptions) (core.Page[core.Conversation], error) {
	f.listCalls++
	f.listOpts = opts
	return f.listPage, f.listErr
}

func (f *fakeService) GetConversation(_ context.Context, id int64) (service.ConversationResult, error) {
	f.getCalls++
	f.getIDs = append(f.getIDs, id)
	return f.getResult, f.getErr
}

func (f *fakeService) SearchContacts(_ context.Context, query string, page int) (core.Page[core.Contact], error) {
	f.searchCalls++
	f.searchQuery = query
	f.searchPageArg = page
	return f.searchPage, f.searchErr
}

func (f *fakeService) GetContactConversations(_ context.Context, id int64, page int) (core.Page[core.Conversation], error) {
	f.contactCalls++
	f.contactID = id
	f.contactPageArg = page
	return f.contactPage, f.contactErr
}

func (f *fakeService) SendReply(_ context.Context, id int64, content string) (service.SendResult, error) {
	f.sendCalls++
	f.sendIDs = append(f.sendIDs, id)
	f.sendTexts = append(f.sendTexts, content)
	return f.sendResult, f.sendErr
}

// fakeAPI is a minimal core.API used to exercise the real service bounding.
type fakeAPI struct {
	getConv core.Conversation
	getErr  error
}

func (f *fakeAPI) Check(context.Context) (core.Identity, error) { return core.Identity{}, nil }

func (f *fakeAPI) ListConversations(context.Context, core.ListOptions) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}

func (f *fakeAPI) GetConversation(context.Context, int64) (core.Conversation, error) {
	return f.getConv, f.getErr
}

func (f *fakeAPI) SearchContacts(context.Context, string, int) (core.Page[core.Contact], error) {
	return core.Page[core.Contact]{}, nil
}

func (f *fakeAPI) ContactConversations(context.Context, int64, int) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}

func (f *fakeAPI) CreateMessage(context.Context, int64, string) (core.Message, error) {
	return core.Message{}, nil
}

func connectSession(t *testing.T, svc service.Service) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	server := newServer(svc)
	srvTransport, cliTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, srvTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, cliTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, ctx
}

func structuredEnvelope(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	env, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content is %T, want object", res.StructuredContent)
	}
	return env
}

func structuredData(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	env := structuredEnvelope(t, res)
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("structured data is %T, want object (envelope %v)", env["data"], env)
	}
	return data
}

func structuredError(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	env := structuredEnvelope(t, res)
	e, ok := env["error"].(map[string]any)
	if !ok {
		t.Fatalf("structured error is %T, want object (envelope %v)", env["error"], env)
	}
	return e
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestRegistersExactlySixToolsWithRequiredFields(t *testing.T) {
	session, ctx := connectSession(t, &fakeService{})

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		byName[tool.Name] = tool
	}
	sort.Strings(names)
	if len(names) != len(wantToolNames) {
		t.Fatalf("registered %d tools %v, want %d %v", len(names), names, len(wantToolNames), wantToolNames)
	}
	for i, want := range wantToolNames {
		if names[i] != want {
			t.Fatalf("tool[%d] = %q, want %q (all: %v)", i, names[i], want, names)
		}
	}

	for name, want := range wantRequired {
		schema, ok := byName[name].InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s: input schema is %T, want object", name, byName[name].InputSchema)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			if len(want) != 0 {
				t.Fatalf("%s: properties is %T, want object", name, schema["properties"])
			}
			props = map[string]any{}
		}
		var required []string
		if raw, ok := schema["required"].([]any); ok {
			for _, r := range raw {
				required = append(required, fmt.Sprint(r))
			}
		}
		sort.Strings(required)
		wantSorted := append([]string(nil), want...)
		sort.Strings(wantSorted)
		if strings.Join(required, ",") != strings.Join(wantSorted, ",") {
			t.Fatalf("%s: required = %v, want %v", name, required, wantSorted)
		}
		for _, field := range required {
			if _, ok := props[field]; !ok {
				t.Fatalf("%s: required field %q missing from properties %v", name, field, props)
			}
		}
		if byName[name].Description == "" {
			t.Fatalf("%s: missing description", name)
		}
	}
}

func TestSendReplyUsesChosenConversationID(t *testing.T) {
	fake := &fakeService{sendResult: service.SendResult{
		ConversationID: 42,
		Message:        core.Message{ID: 500, Content: "Olá", Status: "sent", CreatedAt: 1700000001},
		Delivery:       service.DeliveryAcceptedByAPI,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_reply",
		Arguments: map[string]any{"conversation_id": 42, "content": "Olá"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.sendCalls != 1 {
		t.Fatalf("send calls = %d, want exactly 1", fake.sendCalls)
	}
	if len(fake.sendIDs) != 1 || fake.sendIDs[0] != 42 {
		t.Fatalf("sent ids = %v, want [42]", fake.sendIDs)
	}
	if len(fake.sendTexts) != 1 || fake.sendTexts[0] != "Olá" {
		t.Fatalf("sent texts = %v, want [Olá]", fake.sendTexts)
	}

	data := structuredData(t, res)
	if got := data["conversation_id"]; got != float64(42) {
		t.Fatalf("result conversation_id = %v, want 42", got)
	}
	if got := data["delivery"]; got != service.DeliveryAcceptedByAPI {
		t.Fatalf("result delivery = %v, want %q", got, service.DeliveryAcceptedByAPI)
	}
	if !strings.Contains(resultText(res), service.DeliveryAcceptedByAPI) {
		t.Fatalf("result text missing delivery note: %s", resultText(res))
	}
}

func TestServiceErrorBecomesReadableCode(t *testing.T) {
	fake := &fakeService{sendErr: &service.Error{
		Code:    service.CodeCannotReply,
		Message: "conversation 42 cannot accept a reply",
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_reply",
		Arguments: map[string]any{"conversation_id": 42, "content": "Olá"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for a service failure")
	}
	e := structuredError(t, res)
	if got := e["code"]; got != string(service.CodeCannotReply) {
		t.Fatalf("error code = %v, want %q", got, service.CodeCannotReply)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "cannot accept a reply") {
		t.Fatalf("error message = %q, want readable detail", msg)
	}
	if !strings.Contains(resultText(res), string(service.CodeCannotReply)) {
		t.Fatalf("error text = %q, want it to carry the code", resultText(res))
	}
}

func TestValidationErrorCarriesCode(t *testing.T) {
	session, ctx := connectSession(t, service.New(&fakeAPI{}))

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_contacts",
		Arguments: map[string]any{"query": "   "},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for empty query")
	}
	if got := structuredError(t, res)["code"]; got != string(service.CodeInvalidInput) {
		t.Fatalf("error code = %v, want %q", got, service.CodeInvalidInput)
	}
}

func TestGetConversationSurfacesBoundedMessagesAndNotice(t *testing.T) {
	const total = service.MaxMessages + 15
	messages := make([]core.Message, total)
	for i := range messages {
		messages[i] = core.Message{ID: int64(i + 1), Content: fmt.Sprintf("m%d", i+1), Status: "sent"}
	}
	api := &fakeAPI{getConv: core.Conversation{ID: 42, CanReply: true, Messages: messages}}
	session, ctx := connectSession(t, service.New(api))

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation",
		Arguments: map[string]any{"conversation_id": 42},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	data := structuredData(t, res)
	if got := data["total_messages"]; got != float64(total) {
		t.Fatalf("total_messages = %v, want %d", got, total)
	}
	if got := data["returned_messages"]; got != float64(service.MaxMessages) {
		t.Fatalf("returned_messages = %v, want %d", got, service.MaxMessages)
	}
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %v, want true", got)
	}
	if got := data["untrusted_content"]; got != true {
		t.Fatalf("untrusted_content = %v, want true", got)
	}
	notice, _ := data["content_notice"].(string)
	if !strings.Contains(notice, "untrusted") {
		t.Fatalf("content_notice = %q, want untrusted notice", notice)
	}
	conv, _ := data["conversation"].(map[string]any)
	kept, _ := conv["messages"].([]any)
	if len(kept) != service.MaxMessages {
		t.Fatalf("kept messages = %d, want %d", len(kept), service.MaxMessages)
	}
	if !strings.Contains(resultText(res), "untrusted") {
		t.Fatalf("tool text missing untrusted notice: %s", resultText(res))
	}
}

func TestListConversationsIsBounded(t *testing.T) {
	items := make([]core.Conversation, MaxListItems+25)
	for i := range items {
		items[i] = core.Conversation{ID: int64(i + 1), Status: "open", CanReply: true}
	}
	fake := &fakeService{listPage: core.Page[core.Conversation]{Items: items, NextPage: 3}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_conversations",
		Arguments: map[string]any{"page": 1, "status": "open", "inbox_id": 5},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.listOpts.Page != 1 || fake.listOpts.Status != "open" || fake.listOpts.InboxID != 5 {
		t.Fatalf("service options = %#v", fake.listOpts)
	}
	data := structuredData(t, res)
	convs, _ := data["conversations"].([]any)
	if len(convs) != MaxListItems {
		t.Fatalf("conversations = %d, want bounded %d", len(convs), MaxListItems)
	}
	if data["truncated"] != true {
		t.Fatalf("truncated = %v, want true", data["truncated"])
	}
	if data["next_page"] != float64(3) {
		t.Fatalf("next_page = %v, want 3", data["next_page"])
	}
	if data["untrusted_content"] != true {
		t.Fatalf("untrusted_content = %v, want true", data["untrusted_content"])
	}
}

func TestListConversationsDefaultsPageToOne(t *testing.T) {
	fake := &fakeService{}
	session, ctx := connectSession(t, fake)

	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_conversations",
		Arguments: map[string]any{},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if fake.listOpts.Page != 1 {
		t.Fatalf("page = %d, want default 1", fake.listOpts.Page)
	}
}

func TestOutputsNeverContainCredentialFields(t *testing.T) {
	fake := &fakeService{
		identity: core.Identity{AccountID: 7, AccountName: "Acme", UserName: "Ana"},
		listPage: core.Page[core.Conversation]{Items: []core.Conversation{{ID: 11, Status: "open", CanReply: true}}},
		getResult: service.ConversationResult{
			Conversation:     core.Conversation{ID: 42, CanReply: true, Messages: []core.Message{{ID: 1, Content: "oi"}}},
			TotalMessages:    1,
			ReturnedMessages: 1,
			UntrustedContent: true,
			ContentNotice:    service.UntrustedContentNotice,
		},
		searchPage:  core.Page[core.Contact]{Items: []core.Contact{{ID: 99, Name: "Ana", Email: "ana@example.com", Phone: "+5511"}}},
		contactPage: core.Page[core.Conversation]{Items: []core.Conversation{{ID: 11, Status: "open", CanReply: true}}},
		sendResult: service.SendResult{
			ConversationID: 42,
			Message:        core.Message{ID: 500, Content: "Olá", Status: "sent"},
			Delivery:       service.DeliveryAcceptedByAPI,
		},
	}
	session, ctx := connectSession(t, fake)

	calls := []struct {
		name string
		args map[string]any
	}{
		{"check_connection", map[string]any{}},
		{"list_conversations", map[string]any{}},
		{"get_conversation", map[string]any{"conversation_id": 42}},
		{"search_contacts", map[string]any{"query": "ana"}},
		{"get_contact_conversations", map[string]any{"contact_id": 99}},
		{"send_reply", map[string]any{"conversation_id": 42, "content": "Olá"}},
	}
	for _, call := range calls {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil {
			t.Fatalf("%s: CallTool: %v", call.name, err)
		}
		if res.IsError {
			t.Fatalf("%s: unexpected tool error: %s", call.name, resultText(res))
		}
		env := structuredEnvelope(t, res)
		if env["text_truncated"] != false {
			t.Fatalf("%s: text_truncated = %v, want false for small text", call.name, env["text_truncated"])
		}
		blob, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("%s: marshal result: %v", call.name, err)
		}
		var parsed any
		if err := json.Unmarshal(blob, &parsed); err != nil {
			t.Fatalf("%s: unmarshal result: %v", call.name, err)
		}
		for _, key := range []string{"token", "api_access_token", "authorization"} {
			if hasJSONKey(parsed, key) {
				t.Fatalf("%s: result carries credential field %q: %s", call.name, key, blob)
			}
		}
		for _, c := range res.Content {
			text, ok := c.(*mcp.TextContent)
			if !ok {
				continue
			}
			var embedded any
			if err := json.Unmarshal([]byte(text.Text), &embedded); err != nil {
				continue
			}
			for _, key := range []string{"token", "api_access_token", "authorization"} {
				if hasJSONKey(embedded, key) {
					t.Fatalf("%s: content carries credential field %q: %s", call.name, key, text.Text)
				}
			}
		}
	}
}

func TestGetConversationTruncatesLargeMessageContent(t *testing.T) {
	big := strings.Repeat("a", MaxMessageContentBytes*3)
	fake := &fakeService{getResult: service.ConversationResult{
		Conversation:     core.Conversation{ID: 42, CanReply: true, Messages: []core.Message{{ID: 1, Content: big, Status: "sent"}}},
		TotalMessages:    1,
		ReturnedMessages: 1,
		UntrustedContent: true,
		ContentNotice:    service.UntrustedContentNotice,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation",
		Arguments: map[string]any{"conversation_id": 42},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true for oversized content")
	}
	content := firstMessageContent(t, structuredData(t, res))
	if len(content) > MaxMessageContentBytes {
		t.Fatalf("content = %d bytes, want <= %d", len(content), MaxMessageContentBytes)
	}
	if !utf8.ValidString(content) {
		t.Fatalf("content is not valid UTF-8")
	}
}

func TestGetConversationTruncatesOnRuneBoundary(t *testing.T) {
	// Three-byte runes so the byte limit does not divide evenly.
	big := strings.Repeat("日", 1000) // 3000 bytes
	fake := &fakeService{getResult: service.ConversationResult{
		Conversation:     core.Conversation{ID: 42, CanReply: true, Messages: []core.Message{{ID: 1, Content: big, Status: "sent"}}},
		TotalMessages:    1,
		ReturnedMessages: 1,
		UntrustedContent: true,
		ContentNotice:    service.UntrustedContentNotice,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_conversation",
		Arguments: map[string]any{"conversation_id": 42},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true")
	}
	content := firstMessageContent(t, structuredData(t, res))
	if !utf8.ValidString(content) {
		t.Fatalf("content is not valid UTF-8 after truncation")
	}
	want := (MaxMessageContentBytes / 3) * 3
	if len(content) != want {
		t.Fatalf("content = %d bytes, want %d (whole runes only)", len(content), want)
	}
}

func TestSearchContactsTruncatesTextFieldBytes(t *testing.T) {
	long := strings.Repeat("é", 500) // 1000 bytes, 2-byte runes
	fake := &fakeService{searchPage: core.Page[core.Contact]{
		Items: []core.Contact{{ID: 99, Name: long, Email: long, Phone: long}},
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_contacts",
		Arguments: map[string]any{"query": "ana"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true for oversized contact fields")
	}
	contacts, _ := structuredData(t, res)["contacts"].([]any)
	if len(contacts) != 1 {
		t.Fatalf("contacts = %d, want 1", len(contacts))
	}
	contact := contacts[0].(map[string]any)
	for _, field := range []string{"name", "email", "phone"} {
		value, _ := contact[field].(string)
		if len(value) > MaxTextBytes {
			t.Fatalf("%s = %d bytes, want <= %d", field, len(value), MaxTextBytes)
		}
		if !utf8.ValidString(value) {
			t.Fatalf("%s is not valid UTF-8", field)
		}
	}
}

func TestListConversationsTruncatesSummaryText(t *testing.T) {
	long := strings.Repeat("界", 400) // 1200 bytes, 3-byte runes
	fake := &fakeService{listPage: core.Page[core.Conversation]{
		Items: []core.Conversation{{ID: 1, Status: long, CanReply: true}},
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_conversations",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true for oversized status")
	}
	convs, _ := structuredData(t, res)["conversations"].([]any)
	status, _ := convs[0].(map[string]any)["status"].(string)
	if len(status) > MaxTextBytes {
		t.Fatalf("status = %d bytes, want <= %d", len(status), MaxTextBytes)
	}
	if !utf8.ValidString(status) {
		t.Fatalf("status is not valid UTF-8")
	}
}

func TestSendReplyDoesNotAlterPostedContentButBoundsEcho(t *testing.T) {
	big := strings.Repeat("x", MaxMessageContentBytes*2)
	fake := &fakeService{sendResult: service.SendResult{
		ConversationID: 42,
		Message:        core.Message{ID: 500, Content: big, Status: "sent"},
		Delivery:       service.DeliveryAcceptedByAPI,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_reply",
		Arguments: map[string]any{"conversation_id": 42, "content": big},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(fake.sendTexts) != 1 || fake.sendTexts[0] != big {
		t.Fatalf("service received %d bytes, want the original %d bytes (POST must be untouched)", len(fake.sendTexts), len(big))
	}
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true for the oversized echoed message")
	}
	message, _ := structuredData(t, res)["message"].(map[string]any)
	content, _ := message["content"].(string)
	if len(content) > MaxMessageContentBytes {
		t.Fatalf("echoed content = %d bytes, want <= %d", len(content), MaxMessageContentBytes)
	}
}

func firstMessageContent(t *testing.T, data map[string]any) string {
	t.Helper()
	conv, ok := data["conversation"].(map[string]any)
	if !ok {
		t.Fatalf("conversation is %T, want object", data["conversation"])
	}
	messages, _ := conv["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("no messages in result")
	}
	content, _ := messages[0].(map[string]any)["content"].(string)
	return content
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type recordWriteCloser struct {
	mu  sync.Mutex
	buf bytes.Buffer
	w   io.WriteCloser
}

func (r *recordWriteCloser) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	r.mu.Lock()
	r.buf.Write(p[:n])
	r.mu.Unlock()
	return n, err
}

func (r *recordWriteCloser) Close() error { return r.w.Close() }

func (r *recordWriteCloser) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func TestRunKeepsProtocolOnStdoutAndDiagnosticsOffIt(t *testing.T) {
	svc := &fakeService{identity: core.Identity{AccountID: 7, AccountName: "Acme", UserName: "Ana"}}

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	stdout := &recordWriteCloser{w: outW}
	var diagnostics syncBuffer

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx, svc, inR, stdout, &diagnostics)
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "check_connection", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	_ = session.Close()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the session closed")
	}

	captured := stdout.String()
	if strings.TrimSpace(captured) == "" {
		t.Fatal("no protocol output captured on stdout")
	}
	for i, line := range strings.Split(strings.TrimSpace(captured), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("stdout line %d is not JSON-RPC: %q (%v)", i, line, err)
		}
		if hasJSONKey(v, "token") || hasJSONKey(v, "api_access_token") || hasJSONKey(v, "authorization") {
			t.Fatalf("credential field on stdout line %d: %q", i, line)
		}
	}
}

func hasJSONKey(v any, key string) bool {
	switch value := v.(type) {
	case map[string]any:
		for k, child := range value {
			if strings.EqualFold(k, key) || hasJSONKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if hasJSONKey(child, key) {
				return true
			}
		}
	}
	return false
}
