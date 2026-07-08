// Command cc-guides: Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and shell artifacts from embedded, versioned fragments
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/yasyf/cc-guides/internal/cli"
	applog "github.com/yasyf/cc-guides/internal/log"
)

func main() {
	applog.Setup()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// cli.Execute owns the exit-code contract: 0 ok · 1 drift · 2 invalid input.
	os.Exit(cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
