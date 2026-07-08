package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/fragments"
	"github.com/yasyf/cc-guides/internal/legacy"
)

func newInitCmd() *cobra.Command {
	var dryRun, keepMismatched bool
	cmd := &cobra.Command{
		Use:   "init [artifact]",
		Short: "Migrate a legacy stamped markdown artifact to a rendered .src.md source",
		Long: "Collapse each stamped canonical block in a handwritten markdown artifact\n" +
			"into a {{> name}} directive, self-verify the result re-renders to the\n" +
			"original, then write X.src.md and the re-rendered X.md. Markdown only.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return exit(2, errors.New("init requires an artifact path (e.g. AGENTS.md)"))
			}
			return runInit(cmd, args[0], dryRun, keepMismatched)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report the migration without writing files")
	cmd.Flags().BoolVar(&keepMismatched, "keep-mismatched", false, "leave mismatched/unknown blocks literal and migrate the rest")
	return cmd
}

func runInit(cmd *cobra.Command, artifact string, dryRun, keepMismatched bool) error {
	ver := bannerVersion("", cmd.ErrOrStderr())
	res, err := legacy.Migrate(artifact, legacy.Options{
		DryRun:         dryRun,
		KeepMismatched: keepMismatched,
		Version:        ver,
		Resolver:       fragments.Resolver(),
	})
	out := cmd.OutOrStdout()
	for _, row := range res.Rows {
		fout(out, "%s\t%s\n", row.Status, row.Detail)
	}
	if err != nil {
		if errors.Is(err, legacy.ErrDrift) {
			return silent(1) // rows already printed; mismatch is fixable drift
		}
		return exit(2, err)
	}
	return nil
}
