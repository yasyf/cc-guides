// Command cc-guides: Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and shell artifacts from embedded, versioned fragments
package main

import (
	"context"
	"fmt"
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

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		// Minimal error handling: report on stderr and exit non-zero. As the CLI
		// grows, map typed errors to exit codes here (see STYLEGUIDE.md § Error Handling).
		fmt.Fprintln(os.Stderr, "cc-guides:", err)
		os.Exit(1)
	}
}
