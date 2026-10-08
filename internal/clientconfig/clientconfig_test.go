package clientconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClient(t *testing.T) {
	for _, value := range []string{"claude", "Claude", " codex "} {
		if _, err := ParseClient(value); err != nil {
			t.Errorf("ParseClient(%q): %v", value, err)
		}
	}
	if _, err := ParseClient("cursor"); err == nil {
		t.Fatal("unknown client accepted")
	}
}

func TestDetectInSelectsPlatformPath(t *testing.T) {
	home := "/home/ana"
	getenv := func(key string) string {
		switch key {
		case "XDG_CONFIG_HOME":
			return "/custom/config"
		case "APPDATA":
			return `C:\Users\ana\AppData\Roaming`
		default:
			return ""
		}
	}
	cases := []struct {
		goos   string
		client Client
		want   string
	}{
		{"darwin", Claude, filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{"windows", Claude, filepath.Join(`C:\Users\ana\AppData\Roaming`, "Claude", "claude_desktop_config.json")},
		{"linux", Claude, filepath.Join("/custom/config", "Claude", "claude_desktop_config.json")},
		{"linux", Codex, filepath.Join(home, ".codex", "config.toml")},
	}
	for _, test := range cases {
		target, err := DetectIn(test.client, test.goos, home, getenv)
		if err != nil {
			t.Fatalf("DetectIn(%s, %s): %v", test.goos, test.client, err)
		}
		if target.Path != test.want {
			t.Errorf("DetectIn(%s, %s) = %q, want %q", test.goos, test.client, target.Path, test.want)
		}
	}
}

func TestMergeClaudePreservesOtherServers(t *testing.T) {
	existing := `{"mcpServers":{"other":{"command":"/usr/bin/other"}},"theme":"dark"}`
	out, err := mergeClaude(existing, "/opt/chatwoot-mcp")
	if err != nil {
		t.Fatalf("mergeClaude: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Fatal("unrelated server was dropped")
	}
	chatwoot := servers["chatwoot"].(map[string]any)
	if chatwoot["command"] != "/opt/chatwoot-mcp" {
		t.Fatalf("chatwoot command = %v", chatwoot["command"])
	}
	args, ok := chatwoot["args"].([]any)
	if !ok || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("chatwoot args = %v", chatwoot["args"])
	}
	if root["theme"] != "dark" {
		t.Fatal("unrelated top-level key was dropped")
	}
}

func TestMergeClaudeRejectsInvalidJSON(t *testing.T) {
	if _, err := mergeClaude("{not json", "/opt/chatwoot-mcp"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestUpsertCodexAppendsAndPreservesComments(t *testing.T) {
	existing := "# my codex config\nmodel = \"gpt\"\n"
	out := upsertCodex(existing, "/opt/chatwoot-mcp")
	if !strings.Contains(out, "# my codex config") || !strings.Contains(out, "model = \"gpt\"") {
		t.Fatalf("existing content lost:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.chatwoot]") || !strings.Contains(out, `command = "/opt/chatwoot-mcp"`) {
		t.Fatalf("chatwoot table missing:\n%s", out)
	}
}

func TestUpsertCodexReplacesExistingTable(t *testing.T) {
	existing := "[mcp_servers.chatwoot]\ncommand = \"/old/path\"\nargs = [\"mcp\"]\n\n[other]\nkey = 1\n"
	out := upsertCodex(existing, "/new/path")
	if strings.Contains(out, "/old/path") {
		t.Fatalf("old command kept:\n%s", out)
	}
	if !strings.Contains(out, `command = "/new/path"`) {
		t.Fatalf("new command missing:\n%s", out)
	}
	if !strings.Contains(out, "[other]") || !strings.Contains(out, "key = 1") {
		t.Fatalf("following table lost:\n%s", out)
	}
}

func TestUpsertCodexQuotesWindowsPath(t *testing.T) {
	out := upsertCodex("", `C:\Program Files\chatwoot-mcp.exe`)
	if !strings.Contains(out, `command = "C:\\Program Files\\chatwoot-mcp.exe"`) {
		t.Fatalf("windows path not escaped:\n%s", out)
	}
}

func TestPlanForAndApplyWritesBackupAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	target := Target{Client: Claude, Path: path, Exists: true}

	plan, err := PlanFor(target, "/opt/chatwoot-mcp")
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	if !plan.Changed {
		t.Fatal("plan reported no change")
	}
	backup, err := Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if backup == "" {
		t.Fatal("no backup was written")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	applied, _ := os.ReadFile(path)
	if !strings.Contains(string(applied), "/opt/chatwoot-mcp") {
		t.Fatalf("command not written:\n%s", applied)
	}

	second, err := PlanFor(Target{Client: Claude, Path: path, Exists: true}, "/opt/chatwoot-mcp")
	if err != nil {
		t.Fatalf("second PlanFor: %v", err)
	}
	if second.Changed {
		t.Fatal("second plan should be idempotent")
	}
	if backup, err := Apply(second); err != nil || backup != "" {
		t.Fatalf("idempotent apply = (%q, %v), want no write", backup, err)
	}
}

func TestPlanForCreatesNewCodexConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.toml")
	plan, err := PlanFor(Target{Client: Codex, Path: path, Exists: false}, "/opt/chatwoot-mcp")
	if err != nil {
		t.Fatalf("PlanFor: %v", err)
	}
	if !plan.Changed {
		t.Fatal("new file should be a change")
	}
	backup, err := Apply(plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if backup != "" {
		t.Fatalf("unexpected backup for a new file: %q", backup)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not created: %v", err)
	}
}
