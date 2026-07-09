package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List artifact dirs and the fragments each composes (TSV)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root := repoRoot()
			dirs, err := discoverArtifactDirs(root)
			if err != nil {
				return exit(2, err)
			}
			out := cmd.OutOrStdout()
			for _, dir := range dirs {
				ad, err := loadArtifactDir(root, dir)
				if err != nil {
					return exit(2, err)
				}
				refs := make([]string, 0, len(ad.lay.Entries))
				for _, e := range ad.lay.Entries {
					refs = append(refs, e.Ref())
				}
				fout(out, "%s\t%s\t%s\n", ad.target, ad.kind, strings.Join(refs, ","))
			}
			return nil
		},
	}
}
