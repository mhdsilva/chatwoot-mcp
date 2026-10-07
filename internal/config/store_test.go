package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"chatwoot-mcp/internal/config"
	"chatwoot-mcp/internal/core"
)

func validSettings() core.Settings {
	return core.Settings{
		BaseURL:   "https://app.chatwoot.com",
		AccountID: 7,
		Token:     "secret",
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	ctx := context.Background()

	if err := config.NewStore(path).Save(ctx, validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := config.NewStore(path).Load(ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.BaseURL != "https://app.chatwoot.com" || got.AccountID != 7 || got.Token != "secret" {
		t.Fatalf("load: %+v", got.Public())
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "config.json")
	if err := config.NewStore(path).Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
}

func TestSaveRestrictsFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.NewStore(path).Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestSaveReplacesFileWithoutLeftoverTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("old-content"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := config.NewStore(path).Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "old-content") {
		t.Fatalf("old content survived replacement: %q", data)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config-") {
			t.Fatalf("leftover temporary file: %s", entry.Name())
		}
	}
}

func TestSaveValidationFailureLeavesExistingFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	bad := validSettings()
	bad.Token = ""
	if err := config.NewStore(path).Save(context.Background(), bad); err == nil {
		t.Fatal("expected validation error")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("existing file was modified: %q", data)
	}
}

func TestSaveRejectsInvalidSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*core.Settings)
	}{
		{"empty base url", func(s *core.Settings) { s.BaseURL = "" }},
		{"whitespace base url", func(s *core.Settings) { s.BaseURL = "   " }},
		{"base url without host", func(s *core.Settings) { s.BaseURL = "https://" }},
		{"remote http", func(s *core.Settings) { s.BaseURL = "http://app.chatwoot.com" }},
		{"unsupported scheme", func(s *core.Settings) { s.BaseURL = "ftp://app.chatwoot.com" }},
		{"relative url", func(s *core.Settings) { s.BaseURL = "/chatwoot" }},
		{"empty token", func(s *core.Settings) { s.Token = "" }},
		{"whitespace token", func(s *core.Settings) { s.Token = "   " }},
		{"zero account id", func(s *core.Settings) { s.AccountID = 0 }},
		{"negative account id", func(s *core.Settings) { s.AccountID = -1 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			settings := validSettings()
			tc.mutate(&settings)

			if err := config.NewStore(path).Save(context.Background(), settings); err == nil {
				t.Fatal("expected error, got nil")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid settings must not write a file, stat err = %v", err)
			}
		})
	}
}

func TestSaveAllowsLoopbackHTTP(t *testing.T) {
	urls := []string{
		"http://127.0.0.1",
		"http://127.0.0.1:3000",
		"http://localhost:3000",
		"http://[::1]:3000",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			settings := validSettings()
			settings.BaseURL = u
			if err := config.NewStore(path).Save(context.Background(), settings); err != nil {
				t.Fatalf("save %s: %v", u, err)
			}
		})
	}
}

func TestSaveErrorsNeverContainToken(t *testing.T) {
	const secret = "super-secret-token-value"
	ctx := context.Background()

	// A settings value that fails a field other than the token, so the token
	// is present in the input but must not leak into the error text.
	settings := core.Settings{BaseURL: "", AccountID: 7, Token: secret}
	err := config.NewStore(filepath.Join(t.TempDir(), "config.json")).Save(ctx, settings)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestSaveRejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliably available")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("symlinks not permitted: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	err := config.NewStore(link).Save(context.Background(), validSettings())
	if err == nil {
		t.Fatal("expected symlink rejection")
	}

	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(data) != "original" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliably available")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := config.NewStore(target).Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("symlinks not permitted: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	if _, err := config.NewStore(link).Load(context.Background()); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestLoadRejectsSymlinkToOwnerOnlyValidConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not reliably available")
	}
	const secret = "super-secret-token-value"
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	settings := validSettings()
	settings.Token = secret
	if err := config.NewStore(target).Save(context.Background(), settings); err != nil {
		t.Fatalf("seed: %v", err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		if errors.Is(err, syscall.EPERM) {
			t.Skipf("symlinks not permitted: %v", err)
		}
		t.Fatalf("symlink: %v", err)
	}

	got, err := config.NewStore(link).Load(context.Background())
	if err == nil {
		t.Fatalf("expected symlink rejection, got %+v", got.Public())
	}
	if got.Token != "" {
		t.Fatalf("symlinked config was accepted: %+v", got.Public())
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked token: %v", err)
	}
}

func TestLoadRejectsGroupOrOtherAccessibleFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	const secret = "super-secret-token-value"
	modes := []os.FileMode{0o640, 0o604, 0o644, 0o660, 0o607, 0o666}

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			settings := validSettings()
			settings.Token = secret
			if err := config.NewStore(path).Save(context.Background(), settings); err != nil {
				t.Fatalf("save: %v", err)
			}
			if err := os.Chmod(path, mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}

			got, err := config.NewStore(path).Load(context.Background())
			if err == nil {
				t.Fatalf("expected permission rejection for %04o, got %+v", mode, got.Public())
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaked token: %v", err)
			}
		})
	}
}

func TestLoadAcceptsOwnerOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.NewStore(path).Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got, err := config.NewStore(path).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Token != "secret" || got.AccountID != 7 {
		t.Fatalf("load: %+v", got.Public())
	}
}

func TestLoadMissingFileIsUnconfigured(t *testing.T) {
	got, err := config.NewStore(filepath.Join(t.TempDir(), "missing.json")).Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != (core.Settings{}) {
		t.Fatalf("got %+v, want zero settings", got.Public())
	}
}

func TestNewStoreEmptyPathUsesUserConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := config.NewStore("").Save(context.Background(), validSettings()); err != nil {
		t.Fatalf("save: %v", err)
	}

	want := filepath.Join(dir, "chatwoot-mcp", "config.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default config not written at %s: %v", want, err)
	}
}
