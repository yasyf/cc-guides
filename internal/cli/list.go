package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available fragments (name, kind, origin) as TSV",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolver := buildChain("")
			lister, ok := resolver.(guide.Lister)
			if !ok {
				return exit(2, fmt.Errorf("resolver does not support listing"))
			}
			out := cmd.OutOrStdout()
			for _, e := range lister.Entries() {
				fout(out, "%s\t%s\t%s\n", e.Name, e.Kind, e.Origin)
			}
			return nil
		},
	}
}
