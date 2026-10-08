package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"chatwoot-mcp/internal/app"
	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/config"
	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/mcpserver"
	"chatwoot-mcp/internal/panel"
	"chatwoot-mcp/internal/service"
)

const panelPort = 8765

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runContext(ctx, os.Args[1:], os.Stderr, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	return runContext(context.Background(), args, stderr, os.Stdin, os.Stdout)
}

func runContext(ctx context.Context, args []string, stderr io.Writer, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 1 && args[0] == "app" {
		return runAppWith(ctx, stderr, openBrowser, app.Run)
	}
	return runWith(ctx, args, stderr, stdin, stdout, openBrowser, panel.ListenAndServe, mcpserver.Run)
}

type panelRunner func(context.Context, core.Store, panel.ClientFactory, int) error
type mcpRunner func(context.Context, service.Service, io.Reader, io.Writer) error
type appRunner func(context.Context, app.Options) error

type browserOpener func(string) error

// runAppWith builds the shared service dependencies and starts the desktop app.
func runAppWith(ctx context.Context, stderr io.Writer, browser browserOpener, serveApp appRunner) error {
	factory := func(settings core.Settings) core.API {
		return chatwoot.NewClient(settings, nil)
	}
	return serveApp(ctx, app.Options{
		Store:       config.NewStore(""),
		Factory:     factory,
		Port:        panelPort,
		OpenBrowser: browser,
		Diagnostics: stderr,
	})
}

func runWith(ctx context.Context, args []string, stderr io.Writer, stdin io.Reader, stdout io.Writer, browser browserOpener, servePanel panelRunner, serveMCP mcpRunner) error {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <app|panel|mcp>")
		return errors.New("expected one command")
	}

	switch args[0] {
	case "panel":
		const address = "http://127.0.0.1:8765/"
		fmt.Fprintf(stdout, "Chatwoot MCP panel: %s\n", address)
		if err := browser(address); err != nil {
			fmt.Fprintf(stderr, "could not open browser (%v); open the URL above manually\n", err)
		}
		factory := func(settings core.Settings) core.API {
			return chatwoot.NewClient(settings, nil)
		}
		return servePanel(ctx, config.NewStore(""), factory, panelPort)
	case "mcp":
		store := config.NewStore("")
		settings, err := store.Load(ctx)
		if err != nil {
			return fmt.Errorf("load local configuration: %w", err)
		}
		if err := validateSettings(settings); err != nil {
			return err
		}
		api := chatwoot.NewClient(settings, nil)
		return serveMCP(ctx, service.New(api), stdin, stdout)
	default:
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <app|panel|mcp>")
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func validateSettings(settings core.Settings) error {
	if strings.TrimSpace(settings.Token) == "" {
		return errors.New("Chatwoot is not configured; run `chatwoot-mcp panel` and save a connection first")
	}
	if settings.AccountID <= 0 {
		return errors.New("local configuration has an invalid account ID; run `chatwoot-mcp panel` to update it")
	}
	parsed, err := url.Parse(strings.TrimSpace(settings.BaseURL))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("local configuration has an invalid base URL; run `chatwoot-mcp panel` to update it")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return errors.New("local configuration must use https; run `chatwoot-mcp panel` to update it")
	}
	return nil
}

func openBrowser(address string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{address}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		command, args = "xdg-open", []string{address}
	}
	cmd := exec.Command(command, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Start()
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
