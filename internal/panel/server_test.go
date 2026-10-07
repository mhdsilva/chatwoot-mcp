package panel_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/panel"
)

const panelHost = "127.0.0.1:8765"

type memoryStore struct {
	settings core.Settings
	saves    int
}

func (m *memoryStore) Load(context.Context) (core.Settings, error) { return m.settings, nil }
func (m *memoryStore) Save(_ context.Context, s core.Settings) error {
	m.settings = s
	m.saves++
	return nil
}

type fakeAPI struct {
	identity core.Identity
	err      error
}

func (f fakeAPI) Check(context.Context) (core.Identity, error) { return f.identity, f.err }
func (fakeAPI) ListConversations(context.Context, core.ListOptions) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}
func (fakeAPI) GetConversation(context.Context, int64) (core.Conversation, error) {
	return core.Conversation{}, nil
}
func (fakeAPI) SearchContacts(context.Context, string, int) (core.Page[core.Contact], error) {
	return core.Page[core.Contact]{}, nil
}
func (fakeAPI) ContactConversations(context.Context, int64, int) (core.Page[core.Conversation], error) {
	return core.Page[core.Conversation]{}, nil
}
func (fakeAPI) CreateMessage(context.Context, int64, string) (core.Message, error) {
	return core.Message{}, nil
}

func testHandler(t *testing.T, store *memoryStore) http.Handler {
	t.Helper()
	h, err := panel.NewHandler(store, func(s core.Settings) core.API {
		if s.Token == "submitted-secret" {
			return fakeAPI{identity: core.Identity{AccountID: s.AccountID, AccountName: "Support", UserName: "Ana"}}
		}
		return fakeAPI{}
	}, 8765)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func request(t *testing.T, h http.Handler, method, path, body, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = panelHost
	if method == http.MethodPost {
		r.Header.Set("Origin", "http://"+panelHost)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func pageToken(t *testing.T, h http.Handler) string {
	t.Helper()
	w := request(t, h, "GET", "/", "", "")
	if w.Code != 200 {
		t.Fatalf("page: %d", w.Code)
	}
	const marker = `<meta name="csrf-token" content="`
	i := strings.Index(w.Body.String(), marker)
	if i < 0 {
		t.Fatal("page missing CSRF token")
	}
	s := w.Body.String()[i+len(marker):]
	j := strings.IndexByte(s, '"')
	if j < 0 {
		t.Fatal("malformed CSRF token")
	}
	return s[:j]
}

func TestInitialUnconfiguredStateAndReadEndpoints(t *testing.T) {
	h := testHandler(t, &memoryStore{})
	for _, path := range []string{"/api/status", "/api/client-config", "/api/tools"} {
		w := request(t, h, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatalf("%s: status %d: %s", path, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s cache-control=%q", path, w.Header().Get("Cache-Control"))
		}
	}
	var status map[string]any
	w := request(t, h, "GET", "/api/status", "", "")
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["configured"] != false || status["connected"] != false || status["has_token"] != false {
		t.Fatalf("unexpected initial status: %#v", status)
	}
	w = request(t, h, "GET", "/api/tools", "", "")
	for _, name := range []string{"check_connection", "list_conversations", "get_conversation", "search_contacts", "get_contact_conversations", "send_reply"} {
		if !strings.Contains(w.Body.String(), name) {
			t.Errorf("tools missing %s", name)
		}
	}
}

func TestValidSetupChecksBeforeSavingAndRedactsToken(t *testing.T) {
	store := &memoryStore{}
	h := testHandler(t, store)
	csrf := pageToken(t, h)
	w := request(t, h, "POST", "/api/setup", `{"base_url":"https://cw.example","account_id":7,"token":"submitted-secret"}`, csrf)
	if w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	if store.saves != 1 || store.settings.Token != "submitted-secret" {
		t.Fatalf("settings not saved after check: %#v saves=%d", store.settings.Public(), store.saves)
	}
	if strings.Contains(w.Body.String(), "submitted-secret") {
		t.Fatal("setup response leaked token")
	}
	for _, path := range []string{"/api/status", "/api/client-config"} {
		w = request(t, h, "GET", path, "", "")
		if strings.Contains(w.Body.String(), "submitted-secret") {
			t.Fatalf("%s leaked token", path)
		}
	}
}

func TestFailedConnectionCheckDoesNotSave(t *testing.T) {
	store := &memoryStore{}
	h, err := panel.NewHandler(store, func(core.Settings) core.API { return fakeAPI{err: io.ErrUnexpectedEOF} }, 8765)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/setup", `{"base_url":"https://cw.example","account_id":7,"token":"submitted-secret"}`, pageToken(t, h))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("failed check: status=%d body=%s", w.Code, w.Body.String())
	}
	if store.saves != 0 {
		t.Fatalf("failed connection check saved settings %d times", store.saves)
	}
	if strings.Contains(w.Body.String(), "submitted-secret") {
		t.Fatal("failed setup response leaked token")
	}
}

func TestSetupRejectsInsecureURLBeforeCallingFactory(t *testing.T) {
	store := &memoryStore{}
	factoryCalls := 0
	h, err := panel.NewHandler(store, func(core.Settings) core.API {
		factoryCalls++
		return fakeAPI{identity: core.Identity{AccountID: 7}}
	}, 8765)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/setup", `{"base_url":"http://external.example","account_id":7,"token":"submitted-secret"}`, pageToken(t, h))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("insecure setup: status=%d body=%s", w.Code, w.Body.String())
	}
	if factoryCalls != 0 {
		t.Fatalf("factory called %d times for insecure URL", factoryCalls)
	}
	if store.saves != 0 {
		t.Fatalf("insecure URL saved %d times", store.saves)
	}
}

func TestSetupRequiresCSRFToken(t *testing.T) {
	for _, csrf := range []string{"", "wrong"} {
		store := &memoryStore{}
		h := testHandler(t, store)
		valid := pageToken(t, h)
		if csrf == "wrong" {
			valid = "wrong"
		} else {
			valid = ""
		}
		w := request(t, h, "POST", "/api/setup", `{"base_url":"https://cw.example","account_id":7,"token":"submitted-secret"}`, valid)
		if w.Code != http.StatusForbidden {
			t.Errorf("csrf %q: status=%d", csrf, w.Code)
		}
		if store.saves != 0 {
			t.Fatal("invalid CSRF request saved settings")
		}
	}
}

func TestRejectsUntrustedHostAndOrigin(t *testing.T) {
	h := testHandler(t, &memoryStore{})
	for _, tc := range []struct{ host, origin string }{{"evil.example", "http://127.0.0.1:8765"}, {panelHost, "http://evil.example"}} {
		r := httptest.NewRequest("POST", "/api/setup", strings.NewReader(`{}`))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", pageToken(t, h))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("accepted host/origin %q %q: %d", tc.host, tc.origin, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("rejected host/origin response cache-control=%q", w.Header().Get("Cache-Control"))
		}
	}
}

func TestRejectedCSRFCannotBeCached(t *testing.T) {
	h := testHandler(t, &memoryStore{})
	w := request(t, h, "POST", "/api/setup", `{}`, "wrong")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", w.Header().Get("Cache-Control"))
	}
}

func TestReadOnlyEndpointsDoNotSaveConfiguration(t *testing.T) {
	store := &memoryStore{settings: core.Settings{BaseURL: "https://cw.example", AccountID: 7, Token: "saved"}}
	h := testHandler(t, store)
	for _, path := range []string{"/api/status", "/api/client-config", "/api/tools"} {
		w := request(t, h, "GET", path, "", "")
		if w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if store.saves != 0 {
		t.Fatalf("read endpoint wrote config %d times", store.saves)
	}
	w := request(t, h, "GET", "/api/client-config", "", "")
	b, _ := io.ReadAll(w.Body)
	if strings.Contains(string(b), "saved") {
		t.Fatal("client config leaked token")
	}
}

func TestClientConfigUsesAbsoluteRunningExecutableWithoutToken(t *testing.T) {
	const token = "saved-secret-token"
	store := &memoryStore{settings: core.Settings{BaseURL: "https://cw.example", AccountID: 7, Token: token}}
	h := testHandler(t, store)
	w := request(t, h, "GET", "/api/client-config", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("client config: status=%d body=%s", w.Code, w.Body.String())
	}
	var result struct {
		Config struct {
			MCPServers map[string]struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	entry, ok := result.Config.MCPServers["chatwoot"]
	if !ok {
		t.Fatal("missing chatwoot MCP server config")
	}
	expected, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	if !filepath.IsAbs(entry.Command) {
		t.Fatalf("command path is not absolute: %q", entry.Command)
	}
	if entry.Command != expected {
		t.Fatalf("command=%q, want executable %q", entry.Command, expected)
	}
	if len(entry.Args) != 1 || entry.Args[0] != "mcp" {
		t.Fatalf("args=%v, want [mcp]", entry.Args)
	}
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("client config leaked saved token")
	}
}
