package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
)

func newCheckCmd() *cobra.Command {
	var diff bool
	cmd := &cobra.Command{
		Use:   "check [paths...]",
		Short: "Verify artifacts are in sync with their sources (TSV STATUS on stdout)",
		Long: "Re-render each source in memory and byte-compare it against the artifact\n" +
			"on disk. Emit one TSV row per source: OK, STALE, or MISSING. Exit 1 on any\n" +
			"drift, 2 on invalid input. With no paths, discover sources from the working\n" +
			"directory.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, args, diff)
		},
	}
	cmd.Flags().BoolVar(&diff, "diff", false, "print a unified diff to stderr for each STALE artifact")
	return cmd
}

func runCheck(cmd *cobra.Command, args []string, diff bool) error {
	stderr := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()
	ver := bannerVersion("", stderr)
	resolver := buildChain("")

	explicit, sources, err := collectSources(args)
	if err != nil {
		return exit(2, err)
	}
	if len(sources) == 0 && !explicit {
		foutln(stderr, "cc-guides: no *.src.* sources found")
		return nil
	}

	// A source whose target collides with another (or is itself source-shaped) is
	// invalid input, not something to compare against a sibling artifact.
	collisions := targetCollisions(sources)
	worst := 0
	for _, src := range sources {
		if cerr, bad := collisions[src]; bad {
			fout(stderr, "cc-guides: %s: %v\n", src, cerr)
			if worst < 2 {
				worst = 2
			}
			continue
		}
		status, path, invalid, cerr := checkOne(src, ver, resolver, diff, stderr)
		if invalid {
			fout(stderr, "cc-guides: %s: %v\n", src, cerr)
			if worst < 2 {
				worst = 2
			}
			continue
		}
		fout(out, "%s\t%s\n", status, path)
		if status != "OK" && worst < 1 {
			worst = 1
		}
	}
	if worst == 0 {
		return nil
	}
	return silent(worst)
}

func checkOne(src, ver string, resolver guide.Resolver, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	raw, err := os.ReadFile(src) // #nosec G304 -- check reads user-named sources/artifacts by design
	if err != nil {
		return "", src, true, err
	}
	kind, err := guide.KindForPath(src)
	if err != nil {
		return "", src, true, err
	}
	doc, err := guide.Parse(raw, kind)
	if err != nil {
		return "", src, true, err
	}
	body, err := guide.Render(doc, resolver)
	if err != nil {
		return "", src, true, err
	}
	final := guide.AddBanner(kind, ver, filepath.Base(src), body)

	artifact, err := guide.ArtifactPath(src)
	if err != nil {
		return "", src, true, err
	}
	disk, err := os.ReadFile(artifact) // #nosec G304 -- check reads user-named sources/artifacts by design
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING", artifact, false, nil
		}
		return "", artifact, true, err
	}
	if bytes.Equal(disk, final) {
		return "OK", artifact, false, nil
	}
	if diff {
		fout(stderr, "%s", guide.UnifiedDiff(artifact, disk, final))
	}
	return "STALE", artifact, false, nil
}
