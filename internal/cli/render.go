package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
)

type renderOpts struct {
	stdout       bool
	dryRun       bool
	force        bool
	banner       string
	fragmentsDir string
}

func newRenderCmd() *cobra.Command {
	var o renderOpts
	cmd := &cobra.Command{
		Use:   "render [paths...]",
		Short: "Render X.src.{md,sh} sources to their sibling artifacts",
		Long: "Render each X.src.{md,sh} source to its sibling X.{md,sh} artifact,\n" +
			"expanding {{> name}} directives from embedded and local fragments and\n" +
			"stamping a GENERATED banner. With no paths, walk from the working\n" +
			"directory and render every discovered source.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRender(cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.stdout, "stdout", false, "write rendered output to stdout instead of files")
	f.BoolVar(&o.dryRun, "dry-run", false, "report what would be written without writing")
	f.BoolVar(&o.force, "force", false, "overwrite an artifact even if it carries no cc-guides banner")
	f.StringVar(&o.banner, "banner-version", "", "override the version stamped into the banner")
	f.StringVar(&o.fragmentsDir, "fragments-dir", "", "override the local fragments directory (default <repo>/.claude/fragments)")
	return cmd
}

func runRender(cmd *cobra.Command, args []string, o renderOpts) error {
	stderr := cmd.ErrOrStderr()
	ver := bannerVersion(o.banner, stderr)
	resolver := buildChain(o.fragmentsDir)

	_, sources, err := collectSources(args)
	if err != nil {
		return exit(2, err)
	}
	if len(sources) == 0 {
		foutln(stderr, "cc-guides: no *.src.* sources found")
		return nil
	}
	// Preflight the whole batch before writing anything: a colliding target would
	// otherwise be caught mid-run, after earlier artifacts were already written.
	if err := collisionError(sources); err != nil {
		return exit(2, err)
	}
	for _, src := range sources {
		if err := renderOne(cmd, src, ver, resolver, o); err != nil {
			return err
		}
	}
	return nil
}

func renderOne(cmd *cobra.Command, src, ver string, resolver guide.Resolver, o renderOpts) error {
	raw, err := os.ReadFile(src) // #nosec G304 -- render reads the user-named source file by design
	if err != nil {
		return exit(2, err)
	}
	kind, err := guide.KindForPath(src)
	if err != nil {
		return exit(2, err)
	}
	doc, err := guide.Parse(raw, kind)
	if err != nil {
		return exit(2, fmt.Errorf("%s: %w", src, err))
	}
	body, err := guide.Render(doc, resolver)
	if err != nil {
		return exit(2, fmt.Errorf("%s: %w", src, err))
	}
	final := guide.AddBanner(kind, ver, filepath.Base(src), body)
	artifact, err := guide.ArtifactPath(src)
	if err != nil {
		return exit(2, err)
	}

	if o.stdout {
		_, err := cmd.OutOrStdout().Write(final)
		return err
	}
	if o.dryRun {
		fout(cmd.ErrOrStderr(), "would render %s -> %s\n", src, artifact)
		return nil
	}

	mode := os.FileMode(0o644)
	exists := false
	if info, statErr := os.Stat(artifact); statErr == nil {
		exists = true
		mode = info.Mode().Perm()
		disk, _ := os.ReadFile(artifact) // #nosec G304 -- reads the artifact sibling of a user-named source
		if _, ok := guide.ParseBanner(kind, disk); !ok && !o.force {
			return exit(2, fmt.Errorf("%w: %s (run 'cc-guides init %s' for the first migration, or --force)",
				guide.ErrBannerlessOverwrite, artifact, artifact))
		}
	} else if kind == guide.KindSH {
		mode = 0o755
	}

	// Generated artifacts are world-readable and .sh must be executable, so the
	// modes are intentionally looser than gosec's 0600 default.
	if err := os.WriteFile(artifact, final, mode); err != nil { // #nosec G302 G306 G703 -- artifact path and perms are intentional
		return exit(2, err)
	}
	if !exists && kind == guide.KindSH {
		if err := os.Chmod(artifact, 0o755); err != nil { // #nosec G302 -- shell artifacts must be executable
			return exit(2, err)
		}
	}
	fout(cmd.ErrOrStderr(), "rendered %s -> %s\n", src, artifact)
	return nil
}
