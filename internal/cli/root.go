// Package cli builds the cobra command tree.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/internal/version"
)

// NewRootCmd builds the root command and registers its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cc-guides",
		Short:         "Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and shell artifacts from embedded, versioned fragments",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(newHelloCmd())
	return root
}
