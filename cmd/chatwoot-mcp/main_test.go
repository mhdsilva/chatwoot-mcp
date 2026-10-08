package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"chatwoot-mcp/internal/app"
	"chatwoot-mcp/internal/config"
	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/panel"
	"chatwoot-mcp/internal/service"
)

func TestPanelContinuesWhenBrowserCannotOpen(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	serve := func(_ context.Context, _ core.Store, _ panel.ClientFactory, port int) error {
		called = true
		if port != panelPort {
			t.Fatalf("port = %d, want %d", port, panelPort)
		}
		return nil
	}
	err := runWith(context.Background(), []string{"panel"}, &stderr, strings.NewReader(""), &stdout,
		func(string) error { return errors.New("no browser") }, serve,
		func(context.Context, service.Service, io.Reader, io.Writer) error {
			t.Fatal("unexpected MCP runner")
			return nil
		})
	if err != nil {
		t.Fatalf("run panel: %v", err)
	}
	if !called {
		t.Fatal("panel listener was not started")
	}
	if !strings.Contains(stdout.String(), "http://127.0.0.1:8765/") {
		t.Fatalf("URL not printed to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "open the URL above manually") {
		t.Fatalf("browser failure was not reported: %q", stderr.String())
	}
}

func TestMCPUnconfiguredWritesNoStdout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	err := runWith(context.Background(), []string{"mcp"}, &stderr, strings.NewReader(""), &stdout,
		func(string) error { return nil },
		func(context.Context, core.Store, panel.ClientFactory, int) error {
			t.Fatal("unexpected panel runner")
			return nil
		},
		func(context.Context, service.Service, io.Reader, io.Writer) error {
			t.Fatal("unexpected MCP runner")
			return nil
		})
	if err == nil || !strings.Contains(err.Error(), "run `chatwoot-mcp panel`") {
		t.Fatalf("expected helpful unconfigured error, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunnerReceivesSharedDependencies(t *testing.T) {
	var stderr bytes.Buffer
	called := false
	serveApp := func(_ context.Context, opts app.Options) error {
		called = true
		if opts.Store == nil {
			t.Fatal("app runner received nil store")
		}
		if opts.Factory == nil {
			t.Fatal("app runner received nil client factory")
		}
		if opts.Port != panelPort {
			t.Fatalf("port = %d, want %d", opts.Port, panelPort)
		}
		if opts.OpenBrowser == nil || opts.Diagnostics == nil {
			t.Fatal("app runner received nil browser or diagnostics")
		}
		return nil
	}
	if err := runAppWith(context.Background(), &stderr, func(string) error { return nil }, serveApp); err != nil {
		t.Fatalf("runAppWith: %v", err)
	}
	if !called {
		t.Fatal("app runner was not called")
	}
}

func TestValidateSettingsRequiresCompleteConnection(t *testing.T) {
	for _, settings := range []core.Settings{
		{},
		{BaseURL: "https://chatwoot.example", AccountID: 1},
		{BaseURL: "https://chatwoot.example", AccountID: 0, Token: "secret"},
		{BaseURL: "file:///tmp", AccountID: 1, Token: "secret"},
	} {
		if err := validateSettings(settings); err == nil {
			t.Fatalf("validateSettings(%#v) succeeded", settings)
		}
	}
}

func TestMCPConfiguredPassesStdioAndPropagatesRunnerError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings := core.Settings{BaseURL: "https://chatwoot.example/prefix", AccountID: 7, Token: "test-token"}
	if err := config.NewStore("").Save(context.Background(), settings); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdin := strings.NewReader("client protocol input")
	var stdout, stderr bytes.Buffer
	wantErr := errors.New("runner stopped")
	calls := 0
	err := runWith(context.Background(), []string{"mcp"}, &stderr, stdin, &stdout,
		func(string) error { t.Fatal("unexpected browser opener"); return nil },
		func(context.Context, core.Store, panel.ClientFactory, int) error {
			t.Fatal("unexpected panel runner")
			return nil
		},
		func(_ context.Context, svc service.Service, gotIn io.Reader, gotOut io.Writer) error {
			calls++
			if svc == nil {
				t.Fatal("MCP runner received nil service")
			}
			if gotIn != stdin {
				t.Fatal("MCP runner did not receive the supplied stdin")
			}
			if gotOut != &stdout {
				t.Fatal("MCP runner did not receive the supplied stdout")
			}
			_, _ = io.WriteString(gotOut, "protocol-frame")
			return wantErr
		})
	if calls != 1 {
		t.Fatalf("MCP runner called %d times, want 1", calls)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v, want wrapped %v", err, wantErr)
	}
	if got := stdout.String(); got != "protocol-frame" {
		t.Fatalf("stdout = %q, want only runner protocol bytes", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunWithUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"panel", "mcp"}, {"unknown"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stderr, stdout bytes.Buffer
			err := runWith(context.Background(), args, &stderr, strings.NewReader(""), &stdout,
				func(string) error { t.Fatal("unexpected browser opener"); return nil },
				func(context.Context, core.Store, panel.ClientFactory, int) error {
					t.Fatal("unexpected panel runner")
					return nil
				},
				func(context.Context, service.Service, io.Reader, io.Writer) error {
					t.Fatal("unexpected MCP runner")
					return nil
				})
			if err == nil {
				t.Fatal("expected usage error")
			}
			if !strings.Contains(stderr.String(), "usage: chatwoot-mcp <app|panel|mcp>") {
				t.Fatalf("usage missing from stderr: %q", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("unexpected stdout: %q", stdout.String())
			}
		})
	}
}

func TestValidateSettingsAcceptsPrefixedHTTPSURLAndRejectsUserinfo(t *testing.T) {
	valid := core.Settings{BaseURL: "https://chatwoot.example/chatwoot", AccountID: 7, Token: "secret"}
	if err := validateSettings(valid); err != nil {
		t.Fatalf("prefixed HTTPS URL rejected: %v", err)
	}
	invalid := valid
	invalid.BaseURL = "https://user:password@chatwoot.example/chatwoot"
	if err := validateSettings(invalid); err == nil {
		t.Fatal("URL containing userinfo was accepted")
	}
}
