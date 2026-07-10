package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/migrate"
	"github.com/yasyf/cc-guides/source"
)

type migrateOpts struct {
	dryRun  bool
	banner  string
	sources []string
}

func newMigrateCmd(ctx context.Context) *cobra.Command {
	var o migrateOpts
	cmd := &cobra.Command{
		Use:   "migrate [paths...]",
		Short: "Convert v1 X.src.{md,sh} sources to .claude/fragments/<target>/ layout dirs",
		Long: "For each v1 source, split its prose runs into local *.fragment.* pieces and\n" +
			"its {{> name}} directives into imports, write a .claude/fragments/<target>/\n" +
			"dir with a layout.toml, self-verify the composition reproduces the deployed\n" +
			"artifact byte-for-byte, then re-render with the v2 banner and git-rm the\n" +
			"source. A self-verify mismatch writes nothing and exits 1.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrate(ctx, cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.dryRun, "dry-run", false, "report the migration without writing files")
	f.StringVar(&o.banner, "banner-version", "", "override the version stamped into the banner")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runMigrate(ctx context.Context, cmd *cobra.Command, args []string, o migrateOpts) error {
	stderr := cmd.ErrOrStderr()
	root := repoRoot()
	ver := bannerVersion(o.banner, stderr)
	overrides, err := parseSourceOverrides(o.sources)
	if err != nil {
		return exit(2, err)
	}

	var srcs []string
	if len(args) > 0 {
		for _, a := range args {
			if !guide.IsSource(a) {
				return exit(2, fmt.Errorf("not a v1 source: %q (expected X.src.md or X.src.sh)", a))
			}
			srcs = append(srcs, filepath.ToSlash(a))
		}
	} else {
		srcs, err = discoverSources(root)
		if err != nil {
			return exit(2, err)
		}
	}
	if len(srcs) == 0 {
		foutln(stderr, "cc-guides: no *.src.* sources to migrate")
		return nil
	}

	specs := map[string]string{migrate.CCSkillsAlias: migrate.CCSkillsSpec}
	for a, s := range overrides {
		specs[a] = s
	}
	resolver, err := source.New(source.Options{Specs: specs})
	if err != nil {
		return exit(2, err)
	}
	for _, src := range srcs {
		if err := migrateOne(ctx, cmd, root, src, ver, resolver, o); err != nil {
			return err
		}
	}
	return nil
}

func migrateOne(ctx context.Context, cmd *cobra.Command, root, src, ver string, resolver source.Importer, o migrateOpts) error {
	out := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	abs := filepath.Join(root, filepath.FromSlash(src))
	raw, err := os.ReadFile(abs) // #nosec G304 -- migrate reads the user-named source
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
	target, err := guide.ArtifactPath(src)
	if err != nil {
		return exit(2, err)
	}

	old, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(target))) // #nosec G304 -- reads the deployed artifact to self-verify against
	if err != nil {
		return exit(2, fmt.Errorf("%s: the deployed artifact %q must exist to migrate (render the v1 source first): %w", src, target, err))
	}
	expectBody, ok := guide.StripBanner(kind, old)
	if !ok {
		return exit(2, fmt.Errorf("%s: %q carries no cc-guides banner (run 'cc-guides init' for a handwritten file)", src, target))
	}

	// Fold any flat v1 override (.claude/fragments/<name>.<ext>) the deployed
	// artifact was rendered with into a local fragment, so the override is preserved
	// rather than silently reverted to the shared import.
	overrides, overridePaths, err := collectOverrides(root, doc, kind)
	if err != nil {
		return exit(2, fmt.Errorf("%s: %w", src, err))
	}

	built, err := migrate.Build(ctx, migrate.Input{
		Target:     target,
		Kind:       kind,
		Segments:   migrate.Segments(doc),
		ExpectBody: expectBody,
		Tolerant:   false,
		Version:    ver,
		Importer:   resolver,
		Overrides:  overrides,
	})
	if err != nil {
		var sv *migrate.SelfVerifyError
		if errors.As(err, &sv) {
			fout(out, "MISMATCH\t%s\n", src)
			fout(stderr, "%s", sv.Diff)
			return silent(1)
		}
		return exit(2, fmt.Errorf("%s: %w", src, err))
	}

	if o.dryRun {
		fout(out, "WOULD-MIGRATE\t%s -> %s/\n", src, built.LayoutDir)
		return nil
	}
	if err := writeMigration(root, built, target, kind, append([]string{src}, overridePaths...)); err != nil {
		return exit(2, err)
	}
	fout(out, "MIGRATED\t%s -> %s/\n", src, built.LayoutDir)
	return nil
}

// collectOverrides finds any flat v1 override file (.claude/fragments/<name>.<ext>)
// backing a directive in doc, returning the override bodies keyed by directive name
// and the repo-relative paths to remove after folding. A CRLF override is rejected.
func collectOverrides(root string, doc *guide.Doc, kind guide.Kind) (map[string][]byte, []string, error) {
	overrides := map[string][]byte{}
	var paths []string
	for _, n := range doc.Nodes {
		if n.Include == nil {
			continue
		}
		name := n.Include.Name
		if _, done := overrides[name]; done {
			continue
		}
		rel := guide.FragmentsRoot + "/" + name + kind.Ext()
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) // #nosec G304 -- reads a flat v1 override under the repo's .claude/fragments
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, err
		}
		if bytes.IndexByte(body, '\r') >= 0 {
			return nil, nil, fmt.Errorf("%w: override %s", guide.ErrCRLF, rel)
		}
		overrides[name] = body
		paths = append(paths, rel)
	}
	return overrides, paths, nil
}

// writeMigration writes the layout dir + fragment files, re-renders the artifact,
// and git-rm's the v1 source plus any folded override files (falling back to a
// plain remove when a path is untracked).
func writeMigration(root string, built migrate.Output, target string, kind guide.Kind, remove []string) error {
	absDir := filepath.Join(root, filepath.FromSlash(built.LayoutDir))
	if err := os.MkdirAll(absDir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(absDir, "layout.toml"), built.LayoutTOML, 0o644); err != nil { // #nosec G306 -- committed config, world-readable by design
		return err
	}
	for name, body := range built.FragmentFiles {
		if err := os.WriteFile(filepath.Join(absDir, name), body, 0o644); err != nil { // #nosec G306 -- committed content, world-readable by design
			return err
		}
	}
	absTarget := filepath.Join(root, filepath.FromSlash(target))
	mode := artifactMode(absTarget, kind)
	if err := os.WriteFile(absTarget, built.Artifact, mode); err != nil { // #nosec G306 G302 -- rendered artifact, mode intentional
		return err
	}
	// The migration is functionally complete once the layout dir + artifact are
	// written; the removes only clean up superseded v1 sources. Don't bail on the
	// first failure — that leaves an inaccurate report of what survived on disk.
	// Try every removal, then report all failures together with the true state.
	var failed []string
	for _, p := range remove {
		if err := removeSource(root, p); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", p, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("migration complete — wrote %s/layout.toml and %s — but could not remove %d superseded source file(s): %s; remove them by hand",
			built.LayoutDir, target, len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// artifactMode preserves an existing artifact's mode, else defaults (0755 for a
// new shell artifact, 0644 otherwise).
func artifactMode(absTarget string, kind guide.Kind) os.FileMode {
	if info, err := os.Stat(absTarget); err == nil {
		return info.Mode().Perm()
	}
	if kind == guide.KindSH {
		return 0o755
	}
	return 0o644
}

func removeSource(root, src string) error {
	cmd := exec.Command("git", "-C", root, "rm", "-q", "--", src) // #nosec G204 -- fixed git subcommand; root/src are validated repo paths, not a shell string
	if err := cmd.Run(); err == nil {
		return nil
	}
	return os.Remove(filepath.Join(root, filepath.FromSlash(src)))
}
