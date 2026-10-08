package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"chatwoot-mcp/internal/core"
)

type fakeStore struct{}

func (fakeStore) Load(context.Context) (core.Settings, error) { return core.Settings{}, nil }
func (fakeStore) Save(context.Context, core.Settings) error   { return nil }

func fakeFactory(core.Settings) core.API { return nil }

func TestRunOpensPanelAndReturnsWhenTrayQuits(t *testing.T) {
	var opened []string
	opts := Options{
		Store:       fakeStore{},
		Factory:     fakeFactory,
		OpenBrowser: func(url string) error { opened = append(opened, url); return nil },
		Diagnostics: io.Discard,
		Tray:        func(context.Context, string, func()) error { return nil },
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(opened) != 1 || !strings.HasPrefix(opened[0], "http://127.0.0.1:") {
		t.Fatalf("opened = %#v, want one loopback panel URL", opened)
	}
}

func TestRunFallsBackToHeadlessWhenTrayUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var diagnostics bytes.Buffer
	done := make(chan error, 1)
	opts := Options{
		Store:       fakeStore{},
		Factory:     fakeFactory,
		OpenBrowser: func(string) error { return nil },
		Diagnostics: &diagnostics,
		Tray:        func(context.Context, string, func()) error { return errors.New("no display") },
	}
	go func() { done <- Run(ctx, opts) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !strings.Contains(diagnostics.String(), "tray unavailable") {
			t.Fatalf("fallback not reported: %q", diagnostics.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRunSecondInstanceOpensExistingPanel(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	var opened string
	opts := Options{
		Store:       fakeStore{},
		Factory:     fakeFactory,
		Port:        port,
		OpenBrowser: func(url string) error { opened = url; return nil },
		Diagnostics: io.Discard,
		Tray: func(context.Context, string, func()) error {
			t.Fatal("tray must not run for a second instance")
			return nil
		},
	}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := fmt.Sprintf("http://127.0.0.1:%d/", port); opened != want {
		t.Fatalf("opened = %q, want %q", opened, want)
	}
}
