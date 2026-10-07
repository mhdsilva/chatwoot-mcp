package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

var errNotWired = errors.New("command not wired yet")

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stderr io.Writer) error {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <panel|mcp>")
		return errors.New("expected one command")
	}

	switch args[0] {
	case "panel", "mcp":
		return errNotWired
	default:
		fmt.Fprintln(stderr, "usage: chatwoot-mcp <panel|mcp>")
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
