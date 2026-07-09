package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/legacy"
	"github.com/yasyf/cc-guides/internal/migrate"
	"github.com/yasyf/cc-guides/layout"
)

type initOpts struct {
	dryRun         bool
	keepMismatched bool
	banner         string
	sources        []string
}

func newInitCmd(ctx context.Context) *cobra.Command {
	var o initOpts
	cmd := &cobra.Command{
		Use:   "init [artifact]",
		Short: "Migrate a legacy stamped markdown artifact directly to a v3 layout dir",
		Long: "Collapse each stamped canonical block in a handwritten markdown artifact\n" +
			"into an import, split the surrounding prose into local fragments, and emit a\n" +
			".claude/fragments/<artifact>/ layout dir plus the re-rendered artifact.\n" +
			"Markdown only.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return exit(2, errors.New("init requires an artifact path (e.g. AGENTS.md)"))
			}
			return runInit(ctx, cmd, args[0], o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.dryRun, "dry-run", false, "report the migration without writing files")
	f.BoolVar(&o.keepMismatched, "keep-mismatched", false, "leave mismatched/unknown blocks literal and migrate the rest")
	f.StringVar(&o.banner, "banner-version", "", "override the version stamped into the banner")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runInit(ctx context.Context, cmd *cobra.Command, artifact string, o initOpts) error {
	out := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()
	root := repoRoot()
	ver := bannerVersion(o.banner, stderr)
	overrides, err := parseSourceOverrides(o.sources)
	if err != nil {
		return exit(2, err)
	}

	abs, err := filepath.Abs(artifact)
	if err != nil {
		return exit(2, err)
	}
	target, err := filepath.Rel(root, abs)
	if err != nil {
		return exit(2, err)
	}
	target = filepath.ToSlash(target)

	resolver, err := newV1Resolver(overrides, nil)
	if err != nil {
		return exit(2, err)
	}
	adapter := srcAdapter{ctx: ctx, imp: resolver, alias: layout.DefaultAlias}

	// Stage 1: collapse stamps into a synthesized v1 source + reconstruction.
	res, err := legacy.ToV1Source(abs, legacy.Options{KeepMismatched: o.keepMismatched, Resolver: adapter})
	for _, row := range res.Rows {
		fout(out, "%s\t%s\n", row.Status, row.Detail)
	}
	if err != nil {
		if errors.Is(err, legacy.ErrDrift) {
			return silent(1) // rows already printed; mismatch is fixable drift
		}
		return exit(2, err)
	}

	// Stage 2: parse the synthesized source and emit the v3 layout shape.
	doc, err := guide.Parse(res.SourceBytes, guide.KindMD)
	if err != nil {
		return exit(2, err)
	}
	built, err := migrate.Build(ctx, migrate.Input{
		Target:     target,
		Kind:       guide.KindMD,
		Segments:   migrate.Segments(doc),
		ExpectBody: res.Reconstruction,
		Tolerant:   true,
		Version:    ver,
		Importer:   resolver,
	})
	if err != nil {
		var sv *migrate.SelfVerifyError
		if errors.As(err, &sv) {
			fout(stderr, "%s", sv.Diff)
			return exit(2, fmt.Errorf("%w for %s", legacy.ErrSelfVerify, target))
		}
		return exit(2, err)
	}

	if o.dryRun {
		fout(out, "WOULD-INIT\t%s -> %s/\n", target, built.LayoutDir)
		return nil
	}
	if err := writeInit(root, built, target); err != nil {
		return exit(2, err)
	}
	fout(out, "INIT\t%s -> %s/\n", target, built.LayoutDir)
	return nil
}

// writeInit writes the layout dir + fragment files and overwrites the artifact.
// Unlike migrate there is no v1 source to remove — the handwritten artifact is
// replaced in place by its rendered form.
func writeInit(root string, built migrate.Output, target string) error {
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
	return os.WriteFile(absTarget, built.Artifact, artifactMode(absTarget, guide.KindMD)) // #nosec G306 -- rendered artifact, world-readable by design
}
