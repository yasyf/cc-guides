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
	"github.com/yasyf/cc-guides/internal/version"
	"github.com/yasyf/cc-guides/layout"
	"github.com/yasyf/cc-guides/lockfile"
	"github.com/yasyf/cc-guides/source"
)

type renderOpts struct {
	stdout  bool
	dryRun  bool
	force   bool
	prune   bool
	sources []string
}

func newRenderCmd(ctx context.Context) *cobra.Command {
	var o renderOpts
	cmd := &cobra.Command{
		Use:   "render [paths...]",
		Short: "Render .claude/fragments/<target>/ artifact dirs to their artifacts",
		Long: "Compose each .claude/fragments/<target>/ artifact dir into its target,\n" +
			"expanding local *.fragment.* pieces and imports of shared fragments,\n" +
			"stamping a version-free GENERATED marker (md/sh), and recording every\n" +
			"source pin in .claude/fragments/cc-guides.lock. With no paths, discover\n" +
			"every artifact dir from the repo root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRender(ctx, cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.stdout, "stdout", false, "write rendered output to stdout instead of files")
	f.BoolVar(&o.dryRun, "dry-run", false, "report what would be written without writing")
	f.BoolVar(&o.force, "force", false, "overwrite an artifact even if cc-guides does not manage it")
	f.BoolVar(&o.prune, "prune", false, "stop managing a locked artifact whose dir is gone, leaving its file on disk")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runRender(ctx context.Context, cmd *cobra.Command, args []string, o renderOpts) error {
	stderr := cmd.ErrOrStderr()
	root := repoRoot()
	ver := version.Bare()
	overrides, err := parseSourceOverrides(o.sources)
	if err != nil {
		return exit(2, err)
	}

	dirs, err := collectDirs(root, args)
	if err != nil {
		return exit(2, err)
	}
	if len(dirs) == 0 {
		foutln(stderr, "cc-guides: no artifact dirs found")
		return nil
	}
	// Preflight the whole batch: an unsafe target (escaping the repo, or shared by
	// two dirs) must not clobber anything mid-run.
	ads, err := preflightTargets(root, dirs)
	if err != nil {
		return exit(2, err)
	}
	// A render with path arguments is scoped (surgical lock merge, shared pins
	// frozen); a no-argument render is a full, authoritative rebuild.
	scoped := len(args) > 0
	return renderV3(ctx, cmd, root, ads, overrides, ver, o, scoped)
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
func renderV3(ctx context.Context, cmd *cobra.Command, root string, ads []*artifactDir, overrides map[string]string, ver string, o renderOpts, scoped bool) error {
	if len(ads) == 0 {
		return nil
	}
	existingLock, _, err := lockfile.Load(root)
	if err != nil {
		return exit(2, err)
	}
	layouts := map[string]*layout.Layout{}
	for _, ad := range ads {
		layouts[ad.dir] = ad.lay
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
		// Post-compose validation: re-validate the composed body so an authoring
		// mistake that only surfaces once fragments combine (e.g. the same TOML table
		// in two of them) fails here, naming the target, rather than at capt-hook parse
		// time. Validation only — it never mutates body, so rendered bytes are unchanged.
		if err := ad.kind.PostComposeValidate(body); err != nil {
			return exit(2, fmt.Errorf("%s: %w", ad.target, err))
		}
		final := body
		if ad.kind.Markered() {
			final = guide.AddMarker(ad.kind, ad.dir, body)
		}
		outputs = append(outputs, output{ad, final})
		rendered = append(rendered, ad.target)
		for _, a := range ad.lay.UsedAliases() {
			usedAliases[a] = true
		}
	}

	// A full render rebuilds the lock, so a target that leaves this batch stops being
	// managed. Refuse that while its file is still on disk — the shape of a renamed
	// artifact dir whose `target` key was never added. Checked before --stdout and
	// --dry-run report success, so a dry run surfaces the hazard too.
	if !scoped {
		if err := droppedTargets(root, existingLock, rendered, o.prune); err != nil {
			return exit(2, err)
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
// managed and so safe to clobber: a marker (md/sh), or membership in the existing
// lock's artifacts (the only mechanism for pristine json).
func v3Overwritable(ad *artifactDir, lock *lockfile.Lock) func([]byte) bool {
	return func(disk []byte) bool {
		if ad.kind.Markered() {
			if _, ok := guide.ParseMarker(ad.kind, disk); ok {
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

	mode := kind.NewFileMode()
	exists := false
	if info, statErr := os.Stat(abs); statErr == nil {
		exists = true
		mode = info.Mode().Perm()
		disk, _ := os.ReadFile(abs) // #nosec G304 -- reads the artifact target to check managed-ness before overwrite
		if !canOverwrite(disk) && !o.force {
			return exit(2, fmt.Errorf("%w: %s (pass --force to overwrite)", guide.ErrHandwrittenOverwrite, target))
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return exit(2, err)
	}
	// Generated artifacts are world-readable and an executable kind (.sh) must keep
	// its exec bit, so the modes are intentionally looser than gosec's 0600 default.
	if err := os.WriteFile(abs, final, mode); err != nil { // #nosec G302 G306 -- artifact path and perms are intentional
		return exit(2, err)
	}
	if !exists && mode&0o111 != 0 {
		if err := os.Chmod(abs, mode); err != nil { // #nosec G302 -- executable artifacts keep their exec bit
			return exit(2, err)
		}
	}
	fout(cmd.ErrOrStderr(), "rendered %s -> %s\n", srcLabel, target)
	return nil
}

// preflightTargets loads the whole batch and validates its targets before any write.
// A render is atomic — a partial tree is a corrupted one — so the first dir that
// fails to load refuses the whole batch; check loads on its own terms and shares
// only the target gate. It returns the loaded dirs so the render composes from the
// same parse.
func preflightTargets(root string, dirs []string) ([]*artifactDir, error) {
	ads := make([]*artifactDir, 0, len(dirs))
	for _, dir := range dirs {
		ad, err := loadArtifactDir(root, dir)
		if err != nil {
			return nil, err
		}
		ads = append(ads, ad)
	}
	if err := conflictingTargets(ads); err != nil {
		return nil, err
	}
	return ads, nil
}

// collisionMessage describes one target claimed by several dirs. A case-only
// collision carries both its targets and its reason: naming the dirs alone would read
// as two identical strings colliding, and on the case-sensitive filesystem CI renders
// on the layout genuinely works, so the refusal looks like a bug until the message
// says the artifacts differ per platform.
func collisionMessage(sharing []*artifactDir) string {
	distinct := map[string]bool{}
	for _, ad := range sharing {
		distinct[ad.target] = true
	}
	caseOnly := len(distinct) > 1
	entries := make([]string, 0, len(sharing))
	for _, ad := range sharing {
		if caseOnly {
			entries = append(entries, fmt.Sprintf("%s (%s)", ad.target, ad.dir))
			continue
		}
		entries = append(entries, ad.dir)
	}
	sort.Strings(entries)
	msg := fmt.Sprintf("target %q is shared by %s", sharing[0].target, strings.Join(entries, ", "))
	if caseOnly {
		msg += " (differing only by case: one file where authoring happens, two where CI renders)"
	}
	return msg
}

// droppedTargets refuses a full render that would silently stop managing an
// artifact: one the existing lock records, that this run no longer renders, whose
// file is still on disk. Renaming an artifact dir without adding the matching
// `target` key produces exactly that — the old artifact is orphaned and the rebuilt
// lock forgets it, which for a markerless kind (json) no later check can detect,
// since lock membership was its only record of being managed. Dropping an artifact
// deliberately stays possible: delete its file along with its dir, or pass --prune
// to leave the file on disk unmanaged.
func droppedTargets(root string, existing *lockfile.Lock, rendered []string, prune bool) error {
	if existing == nil || prune {
		return nil
	}
	kept := make(map[string]bool, len(rendered))
	for _, target := range rendered {
		kept[target] = true
	}
	var dropped []string
	for _, target := range existing.Artifacts {
		if kept[target] {
			continue
		}
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(target)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("checking %s before dropping it from the lock: %w", target, err)
		}
		dropped = append(dropped, target)
	}
	if len(dropped) == 0 {
		return nil
	}
	sort.Strings(dropped)
	return fmt.Errorf("%w: %s (did an artifact dir get renamed without a `target` key? delete the file along with its dir, or pass --prune to leave it unmanaged)",
		guide.ErrDroppedArtifact, strings.Join(dropped, ", "))
}

// conflictingTargets folds one deterministic error over every unsafe target in a
// loaded batch: one that escapes the repo via "..", or one shared by two artifact
// dirs. Each dir's effective target comes from its parsed layout, so a `target`
// override colliding with another dir's path-derived target is caught here too.
//
// Sharing is judged case-insensitively, because the fleet authors on a
// case-insensitive filesystem and renders on a case-sensitive one: `.gitignore` and
// `.GITIGNORE` are two artifacts in CI and one file on the laptop that would silently
// clobber. Refusing both places keeps a repo's render portable.
func conflictingTargets(ads []*artifactDir) error {
	byTarget := map[string][]*artifactDir{} // case-folded target -> the dirs claiming it
	for _, ad := range ads {
		fold := strings.ToLower(ad.target)
		byTarget[fold] = append(byTarget[fold], ad)
	}

	var msgs []string
	for _, ad := range ads {
		if ad.target == ".." || strings.HasPrefix(ad.target, "../") || path.IsAbs(ad.target) {
			msgs = append(msgs, fmt.Sprintf("%q: target %q escapes the repo root", ad.dir, ad.target))
		}
	}
	for _, sharing := range byTarget {
		if len(sharing) < 2 {
			continue
		}
		msgs = append(msgs, collisionMessage(sharing))
	}
	if len(msgs) == 0 {
		return nil
	}
	sort.Strings(msgs)
	return fmt.Errorf("refusing to render: %s", strings.Join(msgs, "; "))
}
