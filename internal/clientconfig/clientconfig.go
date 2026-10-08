// Package clientconfig detects and updates the configuration of local MCP
// clients so the installed binary can be registered as the "chatwoot" server.
// It never discards unrelated settings: JSON is merged key by key and the Codex
// TOML table is replaced in place, and every write is preceded by a backup.
package clientconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Client names a supported MCP client.
type Client string

const (
	Claude Client = "claude"
	Codex  Client = "codex"
)

// ServerName is the MCP server key added to every client configuration.
const ServerName = "chatwoot"

// Supported returns every client this package can configure.
func Supported() []Client { return []Client{Claude, Codex} }

// ParseClient resolves a user-supplied client name.
func ParseClient(value string) (Client, error) {
	switch Client(strings.ToLower(strings.TrimSpace(value))) {
	case Claude:
		return Claude, nil
	case Codex:
		return Codex, nil
	default:
		return "", fmt.Errorf("unknown client %q; use claude or codex", value)
	}
}

// Target is one client configuration file and whether it already exists.
type Target struct {
	Client Client `json:"client"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// Plan is a computed, not-yet-applied configuration change.
type Plan struct {
	Target     Target `json:"target"`
	Command    string `json:"command"`
	Before     string `json:"-"`
	After      string `json:"-"`
	Changed    bool   `json:"changed"`
	BackupPath string `json:"backup_path,omitempty"`
}

// Detect resolves the configuration path for a client using the current user's
// home directory and environment.
func Detect(client Client) (Target, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Target{}, fmt.Errorf("resolve home directory: %w", err)
	}
	return DetectIn(client, runtime.GOOS, home, os.Getenv)
}

// DetectIn resolves the configuration path with an explicit OS, home and
// environment, so path selection is testable on any host.
func DetectIn(client Client, goos, home string, getenv func(string) string) (Target, error) {
	if home == "" {
		return Target{}, fmt.Errorf("home directory is required")
	}
	var path string
	switch client {
	case Claude:
		switch goos {
		case "darwin":
			path = filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
		case "windows":
			base := getenv("APPDATA")
			if base == "" {
				base = filepath.Join(home, "AppData", "Roaming")
			}
			path = filepath.Join(base, "Claude", "claude_desktop_config.json")
		default:
			base := getenv("XDG_CONFIG_HOME")
			if base == "" {
				base = filepath.Join(home, ".config")
			}
			path = filepath.Join(base, "Claude", "claude_desktop_config.json")
		}
	case Codex:
		path = filepath.Join(home, ".codex", "config.toml")
	default:
		return Target{}, fmt.Errorf("unknown client %q", client)
	}
	target := Target{Client: client, Path: path}
	if _, err := os.Stat(path); err == nil {
		target.Exists = true
	}
	return target, nil
}

// PlanFor computes the resulting configuration without touching the file.
func PlanFor(target Target, command string) (Plan, error) {
	if strings.TrimSpace(command) == "" {
		return Plan{}, fmt.Errorf("command path must not be empty")
	}
	before := ""
	if target.Exists {
		data, err := os.ReadFile(target.Path)
		if err != nil {
			return Plan{}, fmt.Errorf("read %s: %w", target.Path, err)
		}
		before = string(data)
	}

	var after string
	var err error
	switch target.Client {
	case Claude:
		after, err = mergeClaude(before, command)
	case Codex:
		after = upsertCodex(before, command)
	default:
		return Plan{}, fmt.Errorf("unknown client %q", target.Client)
	}
	if err != nil {
		return Plan{}, err
	}
	return Plan{Target: target, Command: command, Before: before, After: after, Changed: after != before}, nil
}

// Apply writes the plan, backing up any existing file first. It returns the
// backup path, empty when there was nothing to back up.
func Apply(plan Plan) (string, error) {
	if !plan.Changed {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(plan.Target.Path), 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}

	backup := ""
	if plan.Target.Exists {
		backup = plan.Target.Path + ".chatwoot-mcp." + time.Now().UTC().Format("20060102T150405") + ".bak"
		if err := os.WriteFile(backup, []byte(plan.Before), 0o600); err != nil {
			return "", fmt.Errorf("write backup: %w", err)
		}
	}
	if err := os.WriteFile(plan.Target.Path, []byte(plan.After), 0o600); err != nil {
		return backup, fmt.Errorf("write config: %w", err)
	}
	return backup, nil
}

func mergeClaude(existing, command string) (string, error) {
	root := map[string]any{}
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &root); err != nil {
			return "", fmt.Errorf("existing config is not valid JSON: %w", err)
		}
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[ServerName] = map[string]any{"command": command, "args": []string{"mcp"}}
	root["mcpServers"] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	return string(out) + "\n", nil
}

// upsertCodex replaces or appends the [mcp_servers.chatwoot] table without
// reformatting the rest of the file, so comments and unrelated tables survive.
func upsertCodex(existing, command string) string {
	block := "[mcp_servers.chatwoot]\ncommand = " + tomlQuote(command) + "\nargs = [\"mcp\"]\n"
	lines := strings.Split(existing, "\n")

	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "[mcp_servers.chatwoot]" {
			start = i
			break
		}
	}
	if start == -1 {
		trimmed := strings.TrimRight(existing, "\n")
		if strings.TrimSpace(trimmed) == "" {
			return block
		}
		return trimmed + "\n\n" + block
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			end = i
			break
		}
	}

	blockLines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	merged := make([]string, 0, len(lines))
	merged = append(merged, lines[:start]...)
	merged = append(merged, blockLines...)
	merged = append(merged, lines[end:]...)
	return strings.Join(merged, "\n")
}

func tomlQuote(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
	)
	return "\"" + replacer.Replace(value) + "\""
}
