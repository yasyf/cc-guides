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
	"github.com/yasyf/cc-guides/internal/migrate"
	"github.com/yasyf/cc-guides/layout"
	"github.com/yasyf/cc-guides/lockfile"
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
	// A render with path arguments is scoped (surgical lock merge, shared pins
	// frozen); a no-argument render is a full, authoritative rebuild.
	scoped := len(args) > 0
	if err := renderV3(ctx, cmd, root, v3dirs, overrides, ver, o, scoped); err != nil {
		return err
	}
	return renderV1(ctx, cmd, root, v1srcs, overrides, ver, o)
}

// renderV3 composes and writes every v3 artifact dir, resolving imports through a
// single run-wide resolver so every artifact pins the same sha per alias. md/sh
// artifacts carry a version-free marker; json artifacts are written raw.
//
// Errors surface before any write: it composes the whole batch and refuses any
// handwritten clobber up front. It then writes the repo lock BEFORE the artifacts,
// so a crash mid-batch leaves the lock registering every target (a retry re-renders
// instead of refusing a half-written file). A full render (scoped false) rebuilds
// the lock from this run alone; a scoped render pins aliases already in the lock and
// merges into it.
func renderV3(ctx context.Context, cmd *cobra.Command, root string, dirs []string, overrides map[string]string, ver string, o renderOpts, scoped bool) error {
	if len(dirs) == 0 {
		return nil
	}
	existingLock, _, err := lockfile.Load(root)
	if err != nil {
		return exit(2, err)
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
	pinned, err := scopedPins(scoped, existingLock, specs)
	if err != nil {
		return exit(2, err)
	}
	resolver, err := source.New(source.Options{Specs: specs, Pinned: pinned})
	if err != nil {
		return exit(2, err)
	}

	// Compose the whole batch first: a composition error must abort before a single
	// file is written.
	type output struct {
		ad    *artifactDir
		final []byte
	}
	outputs := make([]output, 0, len(ads))
	rendered := make([]string, 0, len(ads))
	usedAliases := map[string]bool{}
	for _, ad := range ads {
		body, err := ad.compose(ctx, resolver)
		if err != nil {
			return exit(2, fmt.Errorf("%s: %w", ad.dir, err))
		}
		final := body
		if ad.kind != guide.KindJSON {
			final = guide.AddMarker(ad.kind, ad.dir, body)
		}
		outputs = append(outputs, output{ad, final})
		rendered = append(rendered, ad.target)
		for _, a := range ad.lay.UsedAliases() {
			usedAliases[a] = true
		}
	}

	if o.stdout || o.dryRun {
		for _, out := range outputs {
			if err := writeArtifact(cmd, root, out.ad.target, out.ad.dir, out.ad.kind, out.final, v3Overwritable(out.ad, existingLock), o); err != nil {
				return err
			}
		}
		return nil
	}

	// Refuse any handwritten clobber before writing anything, so the lock is never
	// advanced past a batch that cannot complete. A target that does not exist yet is
	// fine (writeArtifact creates it); any other read error is fatal here, so an
	// unreadable handwritten file is never silently registered and later clobbered.
	for _, out := range outputs {
		abs := filepath.Join(root, filepath.FromSlash(out.ad.target))
		disk, readErr := os.ReadFile(abs) // #nosec G304 -- reads the artifact target to check managed-ness before overwrite
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return exit(2, fmt.Errorf("checking %s for a handwritten file before overwrite: %w", out.ad.target, readErr))
		}
		if !v3Overwritable(out.ad, existingLock)(disk) && !o.force {
			return exit(2, fmt.Errorf("%w: %s (pass --force to overwrite)", guide.ErrHandwrittenOverwrite, out.ad.target))
		}
	}

	if err := writeLock(root, ver, rendered, usedAliases, specs, resolver, existingLock, scoped); err != nil {
		return err
	}
	for _, out := range outputs {
		if err := writeArtifact(cmd, root, out.ad.target, out.ad.dir, out.ad.kind, out.final, v3Overwritable(out.ad, existingLock), o); err != nil {
			return err
		}
	}
	return nil
}

// scopedPins freezes a scoped render's shared pins: for every alias already in the
// lock it returns the lock's recorded commit (so the pin never moves), leaving
// fresh aliases to resolve normally. A layout spec that disagrees with the lock's
// recorded spec for an alias can't be pinned safely, so it hard-errors and demands
// a full render. A full render (scoped false) or a first render (no lock) pins
// nothing — everything resolves fresh.
func scopedPins(scoped bool, existing *lockfile.Lock, specs map[string]string) (map[string]string, error) {
	if !scoped || existing == nil {
		return nil, nil
	}
	pinned := map[string]string{}
	for alias, spec := range specs {
		lp, ok := existing.Sources[alias]
		if !ok {
			continue
		}
		if lp.Spec != spec {
			return nil, fmt.Errorf("source %q now resolves to %q but the lock pins %q — run a full 'cc-guides render' (no path arguments) to re-resolve", alias, spec, lp.Spec)
		}
		pinned[alias] = lp.Commit
	}
	return pinned, nil
}

// v3Overwritable reports, per disk content, whether a v3 target is cc-guides
// managed and so safe to clobber: a marker or legacy banner (md/sh), or membership
// in the existing lock's artifacts (the only mechanism for pristine json).
func v3Overwritable(ad *artifactDir, lock *lockfile.Lock) func([]byte) bool {
	return func(disk []byte) bool {
		if ad.kind != guide.KindJSON {
			if _, ok := guide.ParseMarker(ad.kind, disk); ok {
				return true
			}
			if _, ok := guide.ParseBanner(ad.kind, disk); ok {
				return true
			}
		}
		return lock != nil && lock.HasArtifact(ad.target)
	}
}

// writeLock composes and writes the repo lock: schema 1, the render version, the
// targets rendered this run, and one commit per alias used this run (from the
// resolver's full-sha pin) against its post-override spec. A full render is
// authoritative — the fresh lock is written as-is, pruning any target or source no
// longer rendered. A scoped render merges the fresh lock into the existing one,
// preserving untouched targets and pins.
func writeLock(root, ver string, rendered []string, usedAliases map[string]bool, specs map[string]string, resolver *source.Resolver, existing *lockfile.Lock, scoped bool) error {
	fresh := &lockfile.Lock{Schema: 1, Version: ver, Artifacts: rendered, Sources: map[string]lockfile.SourcePin{}}
	for a := range usedAliases {
		commit, ok := resolver.FullPin(a)
		if !ok {
			continue
		}
		fresh.Sources[a] = lockfile.SourcePin{Spec: specs[a], Commit: commit}
	}
	final := fresh
	if scoped {
		final = lockfile.Merge(existing, fresh)
	}
	p := filepath.Join(root, filepath.FromSlash(lockfile.Path))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return exit(2, err)
	}
	if err := os.WriteFile(p, final.Encode(), 0o644); err != nil { // #nosec G306 -- committed provenance file, world-readable by design
		return exit(2, err)
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
	foutln(stderr, "cc-guides: warning: rendering v1 *.src.* sources is deprecated — run 'cc-guides migrate' to adopt layout composition; the implicit cc-skills source is v1-only and will be removed")

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
		pin, _ := resolver.Pin(migrate.CCSkillsAlias)
		final := guide.AddBanner(kind, ver, src, v1FragmentsString(doc, pin), body)
		if err := writeArtifact(cmd, root, artifactRel, src, kind, final, v1Overwritable(kind), o); err != nil {
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
			return migrate.CCSkillsAlias + "@" + pin
		}
	}
	return "none"
}

// v1Overwritable reports whether a v1 target on disk carries a cc-guides banner —
// the transitional path's managed-file test.
func v1Overwritable(kind guide.Kind) func([]byte) bool {
	return func(disk []byte) bool {
		_, ok := guide.ParseBanner(kind, disk)
		return ok
	}
}

// writeArtifact writes final to <root>/<target>, honoring --stdout/--dry-run and
// refusing to clobber a handwritten (unmanaged) file without --force — canOverwrite
// decides managed-ness from the on-disk bytes. New .sh artifacts are executable; an
// existing artifact keeps its mode.
func writeArtifact(cmd *cobra.Command, root, target, srcLabel string, kind guide.Kind, final []byte, canOverwrite func([]byte) bool, o renderOpts) error {
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
		disk, _ := os.ReadFile(abs) // #nosec G304 -- reads the artifact target to check managed-ness before overwrite
		if !canOverwrite(disk) && !o.force {
			return exit(2, fmt.Errorf("%w: %s (pass --force to overwrite)", guide.ErrHandwrittenOverwrite, target))
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
