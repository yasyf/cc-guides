package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/layout"
	"github.com/yasyf/cc-guides/source"
)

type renderOpts struct {
	stdout  bool
	dryRun  bool
	force   bool
	banner  string
	sources []string
}

func newRenderCmd(ctx context.Context) *cobra.Command {
	var o renderOpts
	cmd := &cobra.Command{
		Use:   "render [paths...]",
		Short: "Render .claude/fragments/<target>/ artifact dirs (and transitional *.src.* sources) to their artifacts",
		Long: "Compose each .claude/fragments/<target>/ artifact dir into its target,\n" +
			"expanding local *.fragment.* pieces and imports of shared fragments and\n" +
			"stamping a GENERATED banner. Transitional *.src.{md,sh} sources still\n" +
			"render (with a deprecation warning). With no paths, discover both from the\n" +
			"repo root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRender(ctx, cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.stdout, "stdout", false, "write rendered output to stdout instead of files")
	f.BoolVar(&o.dryRun, "dry-run", false, "report what would be written without writing")
	f.BoolVar(&o.force, "force", false, "overwrite an artifact even if it carries no cc-guides banner")
	f.StringVar(&o.banner, "banner-version", "", "override the version stamped into the banner")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runRender(ctx context.Context, cmd *cobra.Command, args []string, o renderOpts) error {
	stderr := cmd.ErrOrStderr()
	root := repoRoot()
	ver := bannerVersion(o.banner, stderr)
	overrides, err := parseSourceOverrides(o.sources)
	if err != nil {
		return exit(2, err)
	}

	v3dirs, v1srcs, err := collectUnits(root, args)
	if err != nil {
		return exit(2, err)
	}
	if len(v3dirs)+len(v1srcs) == 0 {
		foutln(stderr, "cc-guides: no artifact dirs or *.src.* sources found")
		return nil
	}
	// Preflight the whole batch: an unsafe target (escaping, source-shaped, shared,
	// or a selected source) must not clobber anything mid-run.
	if err := preflightTargets(v3dirs, v1srcs); err != nil {
		return exit(2, err)
	}
	if err := renderV3(ctx, cmd, root, v3dirs, overrides, ver, o); err != nil {
		return err
	}
	return renderV1(ctx, cmd, root, v1srcs, overrides, ver, o)
}

// renderV3 composes and writes every v3 artifact dir, resolving imports through a
// single run-wide resolver so every artifact pins the same sha per alias.
func renderV3(ctx context.Context, cmd *cobra.Command, root string, dirs []string, overrides map[string]string, ver string, o renderOpts) error {
	if len(dirs) == 0 {
		return nil
	}
	ads := make([]*artifactDir, 0, len(dirs))
	layouts := map[string]*layout.Layout{}
	for _, dir := range dirs {
		ad, err := loadArtifactDir(root, dir)
		if err != nil {
			return exit(2, err)
		}
		ads = append(ads, ad)
		layouts[dir] = ad.lay
	}
	specs, err := unionSpecs(layouts, overrides)
	if err != nil {
		return exit(2, err)
	}
	resolver, err := source.New(source.Options{Specs: specs})
	if err != nil {
		return exit(2, err)
	}
	for _, ad := range ads {
		body, err := ad.compose(ctx, resolver)
		if err != nil {
			return exit(2, fmt.Errorf("%s: %w", ad.dir, err))
		}
		final := guide.AddBanner(ad.kind, ver, ad.dir, pinString(ad.lay, resolver), body)
		if err := writeArtifact(cmd, root, ad.target, ad.dir, ad.kind, final, o); err != nil {
			return err
		}
	}
	return nil
}

// renderV1 renders the transitional v1 sources, whose {{> name}} directives now
// resolve against the remote cc-skills source instead of the deleted embed.
func renderV1(ctx context.Context, cmd *cobra.Command, root string, srcs []string, overrides map[string]string, ver string, o renderOpts) error {
	if len(srcs) == 0 {
		return nil
	}
	stderr := cmd.ErrOrStderr()
	foutln(stderr, "cc-guides: warning: rendering v1 *.src.* sources is deprecated — run 'cc-guides migrate' to adopt layout composition")

	resolver, err := newV1Resolver(overrides, nil)
	if err != nil {
		return exit(2, err)
	}
	for _, src := range srcs {
		abs := filepath.Join(root, filepath.FromSlash(src))
		raw, err := os.ReadFile(abs) // #nosec G304 -- render reads the user-named source file by design
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
		// v1Chain resolves the cc-skills alias only when the doc has directives, so
		// a directive-free source renders fully offline (fragments=none).
		chain, err := v1Chain(ctx, root, doc, kind, resolver)
		if err != nil {
			return exit(2, fmt.Errorf("%s: %w", src, err))
		}
		body, err := guide.Render(doc, chain)
		if err != nil {
			return exit(2, fmt.Errorf("%s: %w", src, err))
		}
		artifactRel, err := guide.ArtifactPath(src)
		if err != nil {
			return exit(2, err)
		}
		pin, _ := resolver.Pin(layout.DefaultAlias)
		final := guide.AddBanner(kind, ver, src, v1FragmentsString(doc, pin), body)
		if err := writeArtifact(cmd, root, artifactRel, src, kind, final, o); err != nil {
			return err
		}
	}
	return nil
}

// v1FragmentsString pins a transitional artifact's banner: the cc-skills sha when
// the source has directives, else `none`.
func v1FragmentsString(doc *guide.Doc, pin string) string {
	for _, n := range doc.Nodes {
		if n.Include != nil {
			if pin == "" || pin == source.LocalPin {
				return source.LocalPin
			}
			return layout.DefaultAlias + "@" + pin
		}
	}
	return "none"
}

// writeArtifact writes final to <root>/<target>, honoring --stdout/--dry-run and
// refusing to clobber a bannerless (handwritten) file without --force. New .sh
// artifacts are executable; an existing artifact keeps its mode.
func writeArtifact(cmd *cobra.Command, root, target, srcLabel string, kind guide.Kind, final []byte, o renderOpts) error {
	if o.stdout {
		_, err := cmd.OutOrStdout().Write(final)
		return err
	}
	abs := filepath.Join(root, filepath.FromSlash(target))
	if o.dryRun {
		fout(cmd.ErrOrStderr(), "would render %s -> %s\n", srcLabel, target)
		return nil
	}

	mode := os.FileMode(0o644)
	exists := false
	if info, statErr := os.Stat(abs); statErr == nil {
		exists = true
		mode = info.Mode().Perm()
		disk, _ := os.ReadFile(abs) // #nosec G304 -- reads the artifact target to check for a banner before overwrite
		if _, ok := guide.ParseBanner(kind, disk); !ok && !o.force {
			return exit(2, fmt.Errorf("%w: %s (pass --force to overwrite a handwritten file)", guide.ErrBannerlessOverwrite, target))
		}
	} else if kind == guide.KindSH {
		mode = 0o755
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return exit(2, err)
	}
	// Generated artifacts are world-readable and .sh must be executable, so the
	// modes are intentionally looser than gosec's 0600 default.
	if err := os.WriteFile(abs, final, mode); err != nil { // #nosec G302 G306 -- artifact path and perms are intentional
		return exit(2, err)
	}
	if !exists && kind == guide.KindSH {
		if err := os.Chmod(abs, 0o755); err != nil { // #nosec G302 -- shell artifacts must be executable
			return exit(2, err)
		}
	}
	fout(cmd.ErrOrStderr(), "rendered %s -> %s\n", srcLabel, target)
	return nil
}

// allTargets maps each work item (v3 dir or v1 source) to its target artifact,
// for the pre-write collision check.
// preflightTargets validates the whole batch's targets before any write, folding
// one deterministic error over every unsafe target: one that escapes the repo via
// "..", one that is itself source-shaped (a v1 X.src.src.md → X.src.md would
// clobber a source), one that is also a selected source, or one shared by two work
// items. It carries forward the v1 source/artifact collision preflight.
func preflightTargets(v3dirs, v1srcs []string) error {
	targets := map[string]string{}    // work item -> cleaned target
	byTarget := map[string][]string{} // cleaned target -> work items
	srcSet := map[string]bool{}       // cleaned selected v1 sources
	register := func(item, target string) {
		ct := path.Clean(filepath.ToSlash(target))
		targets[item] = ct
		byTarget[ct] = append(byTarget[ct], item)
	}
	for _, s := range v1srcs {
		srcSet[path.Clean(s)] = true
	}
	for _, dir := range v3dirs {
		target, _, err := guide.TargetForLayoutDir(dir)
		if err != nil {
			return err
		}
		register(dir, target)
	}
	for _, s := range v1srcs {
		target, err := guide.ArtifactPath(s)
		if err != nil {
			return err
		}
		register("src:"+s, target)
	}

	var msgs []string
	for item, ct := range targets {
		switch {
		case ct == ".." || strings.HasPrefix(ct, "../") || path.IsAbs(ct):
			msgs = append(msgs, fmt.Sprintf("%q: target %q escapes the repo root", item, ct))
		case guide.IsSource(ct):
			msgs = append(msgs, fmt.Sprintf("%q: target %q is itself a source file", item, ct))
		case srcSet[ct]:
			msgs = append(msgs, fmt.Sprintf("%q: target %q is also a selected source", item, ct))
		case len(byTarget[ct]) > 1:
			others := append([]string(nil), byTarget[ct]...)
			sort.Strings(others)
			msgs = append(msgs, fmt.Sprintf("target %q is shared by %s", ct, strings.Join(others, ", ")))
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	sort.Strings(msgs)
	return fmt.Errorf("refusing to render: %s", strings.Join(msgs, "; "))
}
