// Package app runs the Chatwoot MCP as a desktop application: it serves the
// local setup panel and shows a system tray icon whose menu opens the panel and
// quits. When a tray is unavailable (headless host, unsupported session) it
// falls back to a headless run so the panel keeps working and never leaves the
// user without a way to configure the client.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"

	"chatwoot-mcp/internal/core"
	"chatwoot-mcp/internal/panel"
)

// Options configures one app run. Store and Factory are required; the rest
// have safe defaults.
type Options struct {
	Store       core.Store
	Factory     panel.ClientFactory
	Port        int
	OpenBrowser func(string) error
	Diagnostics io.Writer
	// Tray overrides the system tray runner. Tests inject a fake; nil uses the
	// real cross-platform tray.
	Tray TrayRunner
}

// TrayRunner shows the tray until ctx is cancelled or the user quits, invoking
// openPanel to bring the panel up. It returns an error when no tray is
// available so Run can fall back to a headless run.
type TrayRunner func(ctx context.Context, panelURL string, openPanel func()) error

// Run serves the panel and, when possible, a tray icon. It returns when the
// user quits the tray or ctx is cancelled. Starting a second instance against a
// busy port opens the existing panel and returns without binding again.
func Run(ctx context.Context, opts Options) error {
	if opts.Diagnostics == nil {
		opts.Diagnostics = io.Discard
	}
	if opts.OpenBrowser == nil {
		opts.OpenBrowser = func(string) error { return nil }
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.Port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		if isAddressInUse(err) {
			url := "http://" + address + "/"
			fmt.Fprintf(opts.Diagnostics, "Chatwoot MCP is already running; opening %s\n", url)
			_ = opts.OpenBrowser(url)
			return nil
		}
		return fmt.Errorf("start panel: %w", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	panelURL := fmt.Sprintf("http://127.0.0.1:%d/", actualPort)

	serveErr := make(chan error, 1)
	go func() { serveErr <- panel.Serve(ctx, listener, opts.Store, opts.Factory, actualPort) }()

	if err := opts.OpenBrowser(panelURL); err != nil {
		fmt.Fprintf(opts.Diagnostics, "could not open browser (%v); open %s manually\n", err, panelURL)
	}

	tray := opts.Tray
	if tray == nil {
		tray = runTray
	}
	if trayErr := tray(ctx, panelURL, func() { _ = opts.OpenBrowser(panelURL) }); trayErr != nil {
		fmt.Fprintf(opts.Diagnostics, "tray unavailable (%v); panel running at %s; press Ctrl+C to quit\n", trayErr, panelURL)
		<-ctx.Done()
	}

	cancel()
	return <-serveErr
}

func isAddressInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
