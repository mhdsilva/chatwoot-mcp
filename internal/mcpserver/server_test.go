package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/service"
)

// Run must keep this exact public shape so the mcp command can wire it.
var _ func(context.Context, service.Service, io.Reader, io.Writer) error = Run

var wantToolNames = []string{
	"add_conversation_labels",
	"add_private_note",
	"assign_conversation",
	"check_connection",
	"get_contact_conversations",
	"get_conversation",
	"get_conversation_labels",
	"list_agents",
	"list_conversations",
	"list_inboxes",
	"list_message_templates",
	"list_teams",
	"remove_conversation_labels",
	"search_contacts",
	"send_attachment",
	"send_reply",
	"send_template",
	"set_conversation_status",
	"set_priority",
}

var wantRequired = map[string][]string{
	"add_conversation_labels":    {"conversation_id", "labels"},
	"add_private_note":           {"conversation_id", "content"},
	"assign_conversation":        {"conversation_id"},
	"check_connection":           nil,
	"get_conversation_labels":    {"conversation_id"},
	"list_agents":                nil,
	"list_conversations":         nil,
	"list_inboxes":               nil,
	"list_message_templates":     {"inbox_id"},
	"list_teams":                 nil,
	"remove_conversation_labels": {"conversation_id", "labels"},
	"get_conversation":           {"conversation_id"},
	"search_contacts":            {"query"},
	"get_contact_conversations":  {"contact_id"},
	"send_attachment":            {"conversation_id", "path"},
	"send_reply":                 {"conversation_id", "content"},
	"send_template":              {"conversation_id", "template_name", "language", "category"},
	"set_conversation_status":    {"conversation_id", "status"},
	"set_priority":               {"conversation_id", "priority"},
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

	noteResult service.NoteResult
	noteErr    error
	noteCalls  int
	noteID     int64
	noteText   string

	statusResult service.StatusResult
	statusErr    error
	statusCalls  int
	statusID     int64
	statusValue  string
	statusSnooze string

	priorityResult service.PriorityResult
	priorityErr    error
	priorityCalls  int
	priorityID     int64
	priorityValue  string

	inboxes    []core.Inbox
	inboxesErr error

	agents    []core.Agent
	agentsErr error

	teams    []core.Team
	teamsErr error

	assignResult service.AssignmentResult
	assignErr    error
	assignCalls  int
	assignConv   int64
	assignAgent  int64
	assignTeam   int64

	labelsResult service.LabelsResult
	labelsErr    error
	labelsCalls  int
	labelsOp     string
	labelsConv   int64
	labelsValues []string

	attachmentResult  service.AttachmentResult
	attachmentErr     error
	attachmentCalls   int
	attachmentConv    int64
	attachmentPath    string
	attachmentContent string

	templates      []core.Template
	templatesErr   error
	templatesCalls int
	templatesInbox int64

	templateResult service.SendResult
	templateErr    error
	templateCalls  int
	templateConv   int64
	templateInput  service.TemplateInput
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

func (f *fakeService) AddPrivateNote(_ context.Context, id int64, content string) (service.NoteResult, error) {
	f.noteCalls++
	f.noteID = id
	f.noteText = content
	return f.noteResult, f.noteErr
}

func (f *fakeService) SetConversationStatus(_ context.Context, id int64, status, snoozedUntil string) (service.StatusResult, error) {
	f.statusCalls++
	f.statusID = id
	f.statusValue = status
	f.statusSnooze = snoozedUntil
	return f.statusResult, f.statusErr
}

func (f *fakeService) SetPriority(_ context.Context, id int64, priority string) (service.PriorityResult, error) {
	f.priorityCalls++
	f.priorityID = id
	f.priorityValue = priority
	return f.priorityResult, f.priorityErr
}

func (f *fakeService) ListInboxes(context.Context) ([]core.Inbox, error) {
	return f.inboxes, f.inboxesErr
}

func (f *fakeService) ListAgents(context.Context) ([]core.Agent, error) {
	return f.agents, f.agentsErr
}

func (f *fakeService) ListTeams(context.Context) ([]core.Team, error) {
	return f.teams, f.teamsErr
}

func (f *fakeService) AssignConversation(_ context.Context, conversationID, agentID, teamID int64) (service.AssignmentResult, error) {
	f.assignCalls++
	f.assignConv = conversationID
	f.assignAgent = agentID
	f.assignTeam = teamID
	return f.assignResult, f.assignErr
}

func (f *fakeService) GetConversationLabels(_ context.Context, conversationID int64) (service.LabelsResult, error) {
	f.labelsCalls++
	f.labelsOp = "get"
	f.labelsConv = conversationID
	return f.labelsResult, f.labelsErr
}

func (f *fakeService) AddConversationLabels(_ context.Context, conversationID int64, labels []string) (service.LabelsResult, error) {
	f.labelsCalls++
	f.labelsOp = "add"
	f.labelsConv = conversationID
	f.labelsValues = labels
	return f.labelsResult, f.labelsErr
}

func (f *fakeService) RemoveConversationLabels(_ context.Context, conversationID int64, labels []string) (service.LabelsResult, error) {
	f.labelsCalls++
	f.labelsOp = "remove"
	f.labelsConv = conversationID
	f.labelsValues = labels
	return f.labelsResult, f.labelsErr
}

func (f *fakeService) SendAttachment(_ context.Context, conversationID int64, path, content string) (service.AttachmentResult, error) {
	f.attachmentCalls++
	f.attachmentConv = conversationID
	f.attachmentPath = path
	f.attachmentContent = content
	return f.attachmentResult, f.attachmentErr
}

func (f *fakeService) ListMessageTemplates(_ context.Context, inboxID int64) ([]core.Template, error) {
	f.templatesCalls++
	f.templatesInbox = inboxID
	return f.templates, f.templatesErr
}

func (f *fakeService) SendTemplate(_ context.Context, conversationID int64, in service.TemplateInput) (service.SendResult, error) {
	f.templateCalls++
	f.templateConv = conversationID
	f.templateInput = in
	return f.templateResult, f.templateErr
}

// fakeAPI is a minimal core.API used to exercise the real service bounding and
// to inject upstream failures.
type fakeAPI struct {
	checkErr  error
	getConv   core.Conversation
	getErr    error
	searchErr error
}

func (f *fakeAPI) Check(context.Context) (core.Identity, error) { return core.Identity{}, f.checkErr }

func (f *fakeAPI) ListConversations(context.Context, core.ListOptions) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}

func (f *fakeAPI) GetConversation(context.Context, int64) (core.Conversation, error) {
	return f.getConv, f.getErr
}

func (f *fakeAPI) SearchContacts(context.Context, string, int) (core.Page[core.Contact], error) {
	return core.Page[core.Contact]{}, f.searchErr
}

func (f *fakeAPI) ContactConversations(context.Context, int64, int) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}

func (f *fakeAPI) CreateMessage(context.Context, core.MessageRequest) (core.Message, error) {
	return core.Message{}, nil
}

func (f *fakeAPI) SetStatus(context.Context, core.StatusRequest) (core.Conversation, error) {
	return core.Conversation{}, errAPINotUsed
}

func (f *fakeAPI) SetPriority(context.Context, core.PriorityRequest) (core.Conversation, error) {
	return core.Conversation{}, errAPINotUsed
}

func (f *fakeAPI) Assign(context.Context, core.AssignmentRequest) (core.Conversation, error) {
	return core.Conversation{}, errAPINotUsed
}

func (f *fakeAPI) GetLabels(context.Context, int64) ([]string, error) { return nil, errAPINotUsed }

func (f *fakeAPI) SetLabels(context.Context, core.LabelsRequest) ([]string, error) {
	return nil, errAPINotUsed
}

func (f *fakeAPI) ListInboxes(context.Context) ([]core.Inbox, error) { return nil, errAPINotUsed }

func (f *fakeAPI) GetInbox(context.Context, int64) (core.Inbox, error) {
	return core.Inbox{}, errAPINotUsed
}

func (f *fakeAPI) ListAgents(context.Context) ([]core.Agent, error) { return nil, errAPINotUsed }

func (f *fakeAPI) ListTeams(context.Context) ([]core.Team, error) { return nil, errAPINotUsed }

func (f *fakeAPI) GetContact(context.Context, int64) (core.ContactDetail, error) {
	return core.ContactDetail{}, errAPINotUsed
}

func (f *fakeAPI) UpdateContact(context.Context, core.ContactUpdate) (core.ContactDetail, error) {
	return core.ContactDetail{}, errAPINotUsed
}

func (f *fakeAPI) CreateConversation(context.Context, core.ConversationCreateRequest) (core.Conversation, error) {
	return core.Conversation{}, errAPINotUsed
}

func (f *fakeAPI) ListTemplates(context.Context, int64) ([]core.Template, error) {
	return nil, errAPINotUsed
}

var errAPINotUsed = errors.New("unexpected API call in mcpserver test")

var _ core.API = (*fakeAPI)(nil)

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

func TestRegistersAllToolsWithRequiredFields(t *testing.T) {
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

func TestAddPrivateNoteToolUsesChosenConversation(t *testing.T) {
	fake := &fakeService{noteResult: service.NoteResult{
		ConversationID: 42,
		Message:        core.Message{ID: 700, Content: "nota", Private: true, Status: "sent"},
		Private:        true,
		Delivery:       service.DeliveryAcceptedByAPI,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_private_note",
		Arguments: map[string]any{"conversation_id": 42, "content": "nota"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.noteID != 42 || fake.noteText != "nota" {
		t.Fatalf("service call = (%d, %q), want (42, nota)", fake.noteID, fake.noteText)
	}
	data := structuredData(t, res)
	if data["conversation_id"] != float64(42) || data["private"] != true {
		t.Fatalf("data = %#v", data)
	}
}

func TestSetConversationStatusToolPassesArguments(t *testing.T) {
	fake := &fakeService{statusResult: service.StatusResult{ConversationID: 42, Status: "snoozed", SnoozedUntil: "2030-07-21T17:32:28Z"}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_conversation_status",
		Arguments: map[string]any{"conversation_id": 42, "status": "snoozed", "snoozed_until": "2030-07-21T17:32:28Z"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.statusID != 42 || fake.statusValue != "snoozed" || fake.statusSnooze != "2030-07-21T17:32:28Z" {
		t.Fatalf("service call = (%d, %q, %q)", fake.statusID, fake.statusValue, fake.statusSnooze)
	}
	data := structuredData(t, res)
	if data["status"] != "snoozed" {
		t.Fatalf("data = %#v", data)
	}
}

func TestSetPriorityToolPassesArguments(t *testing.T) {
	fake := &fakeService{priorityResult: service.PriorityResult{ConversationID: 42, Priority: "high"}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_priority",
		Arguments: map[string]any{"conversation_id": 42, "priority": "high"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.priorityID != 42 || fake.priorityValue != "high" {
		t.Fatalf("service call = (%d, %q)", fake.priorityID, fake.priorityValue)
	}
	if data := structuredData(t, res); data["priority"] != "high" {
		t.Fatalf("data = %#v", data)
	}
}

func TestConversationToolValidationErrorCarriesCode(t *testing.T) {
	fake := &fakeService{statusErr: &service.Error{Code: service.CodeInvalidInput, Message: "secret detail"}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_conversation_status",
		Arguments: map[string]any{"conversation_id": 42, "status": "archived"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	e := structuredError(t, res)
	if e["code"] != string(service.CodeInvalidInput) {
		t.Fatalf("error = %#v", e)
	}
	if strings.Contains(resultText(res), "secret detail") {
		t.Fatalf("upstream detail leaked: %s", resultText(res))
	}
}

func TestListInboxesToolReturnsChannelTypes(t *testing.T) {
	fake := &fakeService{inboxes: []core.Inbox{
		{ID: 1, Name: "Site", ChannelType: "Channel::WebWidget"},
		{ID: 2, Name: "Whats", ChannelType: "Channel::Whatsapp"},
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_inboxes", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	data := structuredData(t, res)
	inboxes, ok := data["inboxes"].([]any)
	if !ok || len(inboxes) != 2 {
		t.Fatalf("inboxes = %#v", data["inboxes"])
	}
}

func TestAssignConversationToolPassesIds(t *testing.T) {
	fake := &fakeService{assignResult: service.AssignmentResult{ConversationID: 42, AgentID: 5, TeamID: 3}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "assign_conversation",
		Arguments: map[string]any{"conversation_id": 42, "agent_id": 5, "team_id": 3},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.assignConv != 42 || fake.assignAgent != 5 || fake.assignTeam != 3 {
		t.Fatalf("service call = (%d, %d, %d)", fake.assignConv, fake.assignAgent, fake.assignTeam)
	}
}

func TestAddConversationLabelsToolPassesLabels(t *testing.T) {
	fake := &fakeService{labelsResult: service.LabelsResult{
		ConversationID: 42,
		Previous:       []string{"vip"},
		Labels:         []string{"vip", "urgent"},
		Added:          []string{"urgent"},
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "add_conversation_labels",
		Arguments: map[string]any{"conversation_id": 42, "labels": []any{"urgent"}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.labelsOp != "add" || fake.labelsConv != 42 || len(fake.labelsValues) != 1 || fake.labelsValues[0] != "urgent" {
		t.Fatalf("service call = op %q conv %d values %#v", fake.labelsOp, fake.labelsConv, fake.labelsValues)
	}
	data := structuredData(t, res)
	if labels, ok := data["labels"].([]any); !ok || len(labels) != 2 {
		t.Fatalf("labels = %#v", data["labels"])
	}
	if previous, ok := data["previous_labels"].([]any); !ok || len(previous) != 1 {
		t.Fatalf("previous_labels = %#v", data["previous_labels"])
	}
}

func TestAssignUnknownAgentErrorCarriesNotFoundCode(t *testing.T) {
	fake := &fakeService{assignErr: &service.Error{Code: service.CodeNotFound, Message: "agent 5 is not available"}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "assign_conversation",
		Arguments: map[string]any{"conversation_id": 42, "agent_id": 5},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if e := structuredError(t, res); e["code"] != string(service.CodeNotFound) {
		t.Fatalf("error = %#v", e)
	}
}

func TestSendAttachmentToolPassesPathAndContent(t *testing.T) {
	fake := &fakeService{attachmentResult: service.AttachmentResult{
		ConversationID: 42,
		Message:        core.Message{ID: 900, Status: "sent"},
		Filename:       "pic.png",
		ContentType:    "image/png",
		Size:           16,
		Delivery:       service.DeliveryAcceptedByAPI,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_attachment",
		Arguments: map[string]any{"conversation_id": 42, "path": "/tmp/pic.png", "content": "veja"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.attachmentConv != 42 || fake.attachmentPath != "/tmp/pic.png" || fake.attachmentContent != "veja" {
		t.Fatalf("service call = (%d, %q, %q)", fake.attachmentConv, fake.attachmentPath, fake.attachmentContent)
	}
	if data := structuredData(t, res); data["filename"] != "pic.png" {
		t.Fatalf("data = %#v", data)
	}
}

func TestListMessageTemplatesToolPassesInbox(t *testing.T) {
	fake := &fakeService{templates: []core.Template{{Name: "welcome", Language: "en_US", Body: "Hello {{1}}"}}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_message_templates",
		Arguments: map[string]any{"inbox_id": 5},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.templatesInbox != 5 {
		t.Fatalf("inbox = %d, want 5", fake.templatesInbox)
	}
	data := structuredData(t, res)
	templates, ok := data["templates"].([]any)
	if !ok || len(templates) != 1 {
		t.Fatalf("templates = %#v", data["templates"])
	}
}

func TestSendTemplateToolPassesParameters(t *testing.T) {
	fake := &fakeService{templateResult: service.SendResult{
		ConversationID: 42,
		Message:        core.Message{ID: 901, Status: "sent"},
		Delivery:       service.DeliveryAcceptedByAPI,
	}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "send_template",
		Arguments: map[string]any{
			"conversation_id":  42,
			"template_name":    "welcome",
			"language":         "en_US",
			"category":         "UTILITY",
			"processed_params": map[string]any{"body": map[string]any{"1": "Ana"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if fake.templateConv != 42 || fake.templateInput.Name != "welcome" || fake.templateInput.Category != "UTILITY" {
		t.Fatalf("service call = (%d, %#v)", fake.templateConv, fake.templateInput)
	}
	if fake.templateInput.ProcessedParams == nil {
		t.Fatalf("processed params not forwarded: %#v", fake.templateInput)
	}
}

func TestSendAttachmentChannelUnsupportedErrorCode(t *testing.T) {
	fake := &fakeService{attachmentErr: &service.Error{Code: service.CodeChannelUnsupported, Message: "details"}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "send_attachment",
		Arguments: map[string]any{"conversation_id": 42, "path": "/tmp/pic.png"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if e := structuredError(t, res); e["code"] != string(service.CodeChannelUnsupported) {
		t.Fatalf("error = %#v", e)
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
	if got := data["total_messages_exact"]; got != true {
		t.Fatalf("total_messages_exact = %v, want true", got)
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

func TestGetConversationIncludesSafeAttachmentMetadata(t *testing.T) {
	const privateURL = "https://private.invalid/one-time-download"
	fake := &fakeAPI{getConv: core.Conversation{
		ID: 42,
		Messages: []core.Message{{
			ID: 7, ContentType: "text", Attachments: []core.MessageAttachment{{
				ID: 91, FileType: "audio", Extension: "ogg", ContentType: "audio/ogg", FileSize: 1234,
			}},
		}},
	}}
	session, ctx := connectSession(t, service.New(fake))
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
	blob, _ := json.Marshal(res)
	if !strings.Contains(string(blob), `"file_type":"audio"`) || strings.Contains(string(blob), privateURL) {
		t.Fatalf("attachment metadata missing or private URL leaked: %s", blob)
	}
}

func TestTruncateMessageBoundsAttachmentMetadata(t *testing.T) {
	attachments := make([]core.MessageAttachment, MaxMessageAttachments+2)
	for i := range attachments {
		attachments[i] = core.MessageAttachment{
			FileType:    strings.Repeat("a", MaxTextBytes*2),
			Extension:   strings.Repeat("b", MaxTextBytes*2),
			ContentType: strings.Repeat("c", MaxTextBytes*2),
		}
	}

	message, truncated := truncateMessage(core.Message{Attachments: attachments})
	if !truncated || !message.AttachmentsTruncated {
		t.Fatal("message metadata was not marked truncated")
	}
	if len(message.Attachments) != MaxMessageAttachments {
		t.Fatalf("attachments = %d, want %d", len(message.Attachments), MaxMessageAttachments)
	}
	for _, attachment := range message.Attachments {
		if len(attachment.FileType) > MaxTextBytes || len(attachment.Extension) > MaxTextBytes || len(attachment.ContentType) > MaxTextBytes {
			t.Fatalf("attachment metadata exceeded text bound: %#v", attachment)
		}
	}
	if len(attachments) != MaxMessageAttachments+2 || len(attachments[0].FileType) != MaxTextBytes*2 {
		t.Fatal("truncateMessage mutated the caller's attachment slice")
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
	// Sentinel is deliberately absent from the returned data, so it must never
	// appear. A sentinel that is legitimate message content would be echoed and
	// is intentionally not asserted here.
	const sentinel = "super-secret-token-value"
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
		if strings.Contains(string(blob), sentinel) {
			t.Fatalf("%s: sentinel value leaked into result: %s", call.name, blob)
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

func TestGetConversationTruncatesStatusFields(t *testing.T) {
	longStatus := strings.Repeat("界", 400) // 1200 bytes, 3-byte runes
	fake := &fakeService{getResult: service.ConversationResult{
		Conversation: core.Conversation{
			ID:       42,
			Status:   longStatus,
			CanReply: true,
			Messages: []core.Message{{ID: 1, Content: "oi", Status: longStatus}},
		},
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
		t.Fatalf("text_truncated = false, want true for oversized status fields")
	}
	data := structuredData(t, res)
	conversation, _ := data["conversation"].(map[string]any)
	convStatus, _ := conversation["status"].(string)
	if len(convStatus) > MaxTextBytes {
		t.Fatalf("conversation status = %d bytes, want <= %d", len(convStatus), MaxTextBytes)
	}
	if !utf8.ValidString(convStatus) {
		t.Fatalf("conversation status is not valid UTF-8")
	}
	messages, _ := conversation["messages"].([]any)
	msgStatus, _ := messages[0].(map[string]any)["status"].(string)
	if len(msgStatus) > MaxTextBytes {
		t.Fatalf("message status = %d bytes, want <= %d", len(msgStatus), MaxTextBytes)
	}
	if !utf8.ValidString(msgStatus) {
		t.Fatalf("message status is not valid UTF-8")
	}
}

func TestSendReplyTruncatesEchoedStatus(t *testing.T) {
	longStatus := strings.Repeat("é", 400) // 800 bytes, 2-byte runes
	fake := &fakeService{sendResult: service.SendResult{
		ConversationID: 42,
		Message:        core.Message{ID: 500, Content: "Olá", Status: longStatus},
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
	if structuredEnvelope(t, res)["text_truncated"] != true {
		t.Fatalf("text_truncated = false, want true for oversized echoed status")
	}
	message, _ := structuredData(t, res)["message"].(map[string]any)
	status, _ := message["status"].(string)
	if len(status) > MaxTextBytes {
		t.Fatalf("echoed status = %d bytes, want <= %d", len(status), MaxTextBytes)
	}
	if !utf8.ValidString(status) {
		t.Fatalf("echoed status is not valid UTF-8")
	}
	if len(fake.sendTexts) != 1 || fake.sendTexts[0] != "Olá" {
		t.Fatalf("service received %v, want the original content (POST must be untouched)", fake.sendTexts)
	}
}

func TestErrorTextIsFixedAndDoesNotEchoUpstream(t *testing.T) {
	const secret = "super-secret-token-value"
	huge := "upstream said: " + secret + " " + strings.Repeat("界", 1000)
	fake := &fakeService{checkErr: &service.Error{Code: service.CodeUpstream, Message: huge}}
	session, ctx := connectSession(t, fake)

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_connection",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	e := structuredError(t, res)
	if e["code"] != string(service.CodeUpstream) {
		t.Fatalf("code = %v, want %q (code must stay exact)", e["code"], service.CodeUpstream)
	}
	message, _ := e["message"].(string)
	if message != fixedErrorMessage(service.CodeUpstream) {
		t.Fatalf("message = %q, want the fixed %q message", message, fixedErrorMessage(service.CodeUpstream))
	}
	if len(message) > MaxErrorBytes {
		t.Fatalf("error message = %d bytes, want <= %d", len(message), MaxErrorBytes)
	}
	if !utf8.ValidString(message) {
		t.Fatalf("error message is not valid UTF-8")
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("upstream secret leaked into result: %s", blob)
	}
	if strings.Contains(resultText(res), secret) {
		t.Fatalf("upstream secret leaked into content text: %s", resultText(res))
	}
	if structuredEnvelope(t, res)["text_truncated"] != false {
		t.Fatalf("text_truncated = %v, want false for a fixed message", structuredEnvelope(t, res)["text_truncated"])
	}
}

func TestAPIErrorSecretNeverReachesMCPResult(t *testing.T) {
	const secret = "super-secret-token-value"
	api := &fakeAPI{checkErr: &chatwoot.Error{
		Kind:       chatwoot.KindServer,
		StatusCode: 500,
		Resource:   "account 7 profile",
		Message:    "upstream body: " + secret,
	}}
	session, ctx := connectSession(t, service.New(api))

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "check_connection",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for an upstream failure")
	}
	if got := structuredError(t, res)["code"]; got != string(service.CodeUpstreamServer) {
		t.Fatalf("code = %v, want %q", got, service.CodeUpstreamServer)
	}
	if got := structuredError(t, res)["status_code"]; got != float64(500) {
		t.Fatalf("status_code = %v, want 500", got)
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("API body secret leaked into MCP result: %s", blob)
	}
	if strings.Contains(resultText(res), secret) {
		t.Fatalf("API body secret leaked into content text: %s", resultText(res))
	}
}

func TestInvalidAPIResponseIsClassifiedWithoutLeakingBody(t *testing.T) {
	const secret = "sensitive-upstream-payload"
	api := &fakeAPI{searchErr: &chatwoot.Error{
		Kind:               chatwoot.KindInvalid,
		StatusCode:         200,
		Resource:           "contacts search",
		Message:            "unexpected body: " + secret,
		ResponseFormat:     "json",
		ResponseDecodeKind: "unexpected_json_type",
		ResponseField:      "payload",
		ExpectedJSONType:   "array",
		ActualJSONType:     "object",
	}}
	session, ctx := connectSession(t, service.New(api))

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_contacts",
		Arguments: map[string]any{"query": "34998147021"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for an invalid upstream response")
	}
	e := structuredError(t, res)
	if e["code"] != string(service.CodeInvalidResponse) || e["status_code"] != float64(200) || e["response_format"] != "json" || e["decode_error"] != "unexpected_json_type" || e["field"] != "payload" || e["expected_json_type"] != "array" || e["actual_json_type"] != "object" {
		t.Fatalf("error = %#v, want invalid_response with sanitized response diagnostics", e)
	}
	blob, _ := json.Marshal(res)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("upstream body leaked into result: %s", blob)
	}
}

func TestDeliveryUnknownMessageTellsToCheckBeforeRetry(t *testing.T) {
	const secret = "super-secret-token-value"
	fake := &fakeService{sendErr: &service.Error{
		Code:    service.CodeDeliveryUnknown,
		Message: "delivery detail " + secret,
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
		t.Fatalf("IsError = false, want true")
	}
	message, _ := structuredError(t, res)["message"].(string)
	lower := strings.ToLower(message)
	if !strings.Contains(lower, "check the conversation") || !strings.Contains(lower, "retry") {
		t.Fatalf("delivery_unknown message = %q, want it to tell the caller to check the conversation before retrying", message)
	}
	if strings.Contains(message, secret) {
		t.Fatalf("delivery_unknown message echoed upstream text: %q", message)
	}
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
