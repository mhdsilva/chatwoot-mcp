package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
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

	"chatwoot-mcp/internal/analytics"
	"chatwoot-mcp/internal/app"
	"chatwoot-mcp/internal/chatwoot"
	"chatwoot-mcp/internal/clientconfig"
	"chatwoot-mcp/internal/config"
	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/mcpserver"
	"chatwoot-mcp/internal/panel"
	"chatwoot-mcp/internal/service"
)

const panelPort = 8765

// version is injected at build time with -ldflags "-X main.version=...".
var version = "dev"

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
	if len(args) >= 1 && args[0] == "app" {
		if len(args) != 1 {
			return fmt.Errorf("app takes no arguments")
		}
		return runAppWith(ctx, stderr, openBrowser, app.Run)
	}
	if len(args) >= 1 && args[0] == "configure-client" {
		return runConfigureClient(args[1:], stderr, stdout, stdin, configureDeps{
			detect:     clientconfig.Detect,
			plan:       clientconfig.PlanFor,
			apply:      clientconfig.Apply,
			executable: os.Executable,
		})
	}
	return runWith(ctx, args, stderr, stdin, stdout, openBrowser, panel.ListenAndServe, mcpserver.Run)
}

type configureDeps struct {
	detect     func(clientconfig.Client) (clientconfig.Target, error)
	plan       func(clientconfig.Target, string) (clientconfig.Plan, error)
	apply      func(clientconfig.Plan) (string, error)
	executable func() (string, error)
}

// runConfigureClient registers the running binary in local MCP clients. It
// prints the plan first and, unless --yes is given, asks for confirmation on
// stdin; every existing file is backed up before it is rewritten.
func runConfigureClient(args []string, stderr, stdout io.Writer, stdin io.Reader, deps configureDeps) error {
	flags := flag.NewFlagSet("configure-client", flag.ContinueOnError)
	flags.SetOutput(stderr)
	clientName := flags.String("client", "all", "claude, codex or all")
	command := flags.String("command", "", "path to the chatwoot-mcp executable (defaults to this executable)")
	configPath := flags.String("config", "", "override the client configuration path (single client only)")
	assumeYes := flags.Bool("yes", false, "apply without prompting")
	dryRun := flags.Bool("dry-run", false, "print the change without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}

	commandPath := strings.TrimSpace(*command)
	if commandPath == "" {
		executable, err := deps.executable()
		if err != nil {
			return fmt.Errorf("locate executable: %w", err)
		}
		commandPath = executable
	}

	var clients []clientconfig.Client
	if strings.EqualFold(strings.TrimSpace(*clientName), "all") {
		if strings.TrimSpace(*configPath) != "" {
			return fmt.Errorf("--config requires a single --client")
		}
		clients = clientconfig.Supported()
	} else {
		client, err := clientconfig.ParseClient(*clientName)
		if err != nil {
			return err
		}
		clients = []clientconfig.Client{client}
	}

	for _, client := range clients {
		target, err := resolveTarget(client, strings.TrimSpace(*configPath), deps.detect)
		if err != nil {
			return err
		}
		plan, err := deps.plan(target, commandPath)
		if err != nil {
			return err
		}
		if !plan.Changed {
			fmt.Fprintf(stdout, "%s: already configured at %s\n", client, target.Path)
			continue
		}
		if *dryRun {
			fmt.Fprintf(stdout, "%s: would update %s to run %q\n", client, target.Path, commandPath)
			continue
		}
		if !*assumeYes {
			fmt.Fprintf(stdout, "%s: update %s to run %q? [y/N] ", client, target.Path, commandPath)
			if !confirmed(stdin) {
				fmt.Fprintf(stdout, "skipped %s\n", client)
				continue
			}
		}
		backup, err := deps.apply(plan)
		if err != nil {
			return err
		}
		if backup != "" {
			fmt.Fprintf(stdout, "%s: updated %s (backup: %s)\n", client, target.Path, backup)
		} else {
			fmt.Fprintf(stdout, "%s: updated %s\n", client, target.Path)
		}
	}
	return nil
}

func resolveTarget(client clientconfig.Client, override string, detect func(clientconfig.Client) (clientconfig.Target, error)) (clientconfig.Target, error) {
	if override == "" {
		return detect(client)
	}
	target := clientconfig.Target{Client: client, Path: override}
	if _, err := os.Stat(override); err == nil {
		target.Exists = true
	}
	return target, nil
}

func confirmed(stdin io.Reader) bool {
	reader := bufio.NewReader(stdin)
	line, _ := reader.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

type panelRunner func(context.Context, core.Store, panel.ClientFactory, int) error
type mcpRunner func(context.Context, service.Service, analytics.Service, io.Reader, io.Writer) error
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
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <app|panel|mcp|configure-client|version>")
		return errors.New("expected one command")
	}

	switch args[0] {
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "chatwoot-mcp %s\n", version)
		return nil
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
		operations := service.New(api)
		insights := analytics.New(api, api, nil)
		return serveMCP(ctx, operations, insights, stdin, stdout)
	default:
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <app|panel|mcp|configure-client|version>")
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
