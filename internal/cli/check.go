package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/lockfile"
	"github.com/yasyf/cc-guides/source"
)

// errLockStale is a lock whose recorded source spec for an imported alias no
// longer matches the layout (or is missing) — the repo must re-render.
var errLockStale = errors.New("lock out of date — run cc-guides render")

// errNoLock is an artifact dir with no entry in the repo lock file — check has
// nothing to pin against, so the repo must render first.
var errNoLock = errors.New("no cc-guides.lock entry — run 'cc-guides render'")

// dirResult is one artifact dir's load outcome: the loaded dir, or the error that
// replaced it. check reports a load failure as that dir's row and carries on, so a
// repo with several dirs in flight surfaces every one of them per invocation.
type dirResult struct {
	dir string
	ad  *artifactDir
	err error
}

type checkOpts struct {
	diff    bool
	sources []string
}

func newCheckCmd(ctx context.Context) *cobra.Command {
	var o checkOpts
	cmd := &cobra.Command{
		Use:   "check [paths...]",
		Short: "Verify artifacts are in sync with their layouts (TSV STATUS on stdout)",
		Long: "Re-compose each artifact in memory — pinned to the commits the lock\n" +
			"records — and byte-compare it against the file on disk. Emit one TSV row\n" +
			"per artifact: OK, STALE, or MISSING. Exit 1 on any drift, 2 on invalid\n" +
			"input. With no paths, discover artifact dirs from the repo root.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(ctx, cmd, args, o)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.diff, "diff", false, "print a unified diff to stderr for each STALE artifact")
	f.StringArrayVar(&o.sources, "source", nil, "override a source alias: --source alias=<github:spec|localdir> (repeatable)")
	return cmd
}

func runCheck(ctx context.Context, cmd *cobra.Command, args []string, o checkOpts) error {
	stderr := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()
	root := repoRoot()
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
	// Load here rather than through render's preflight: a dir that fails to load is
	// one row of the report, not the end of the run, so N broken dirs surface in one
	// invocation. The target gate still sees the whole batch before any row prints.
	loaded := make([]dirResult, 0, len(dirs))
	for _, dir := range dirs {
		ad, lerr := loadArtifactDir(root, dir)
		loaded = append(loaded, dirResult{dir: dir, ad: ad, err: lerr})
	}
	if err := conflictingTargets(resolved(loaded)); err != nil {
		return exit(2, err)
	}

	lock, _, err := lockfile.Load(root)
	if err != nil {
		return exit(2, err)
	}

	worst := 0
	bump := func(code int) {
		if worst < code {
			worst = code
		}
	}
	for _, r := range loaded {
		if r.err != nil {
			// A load error already leads with its dir; record's label would repeat it.
			fout(stderr, "cc-guides: %v\n", r.err)
			bump(2)
			continue
		}
		status, path, invalid, cerr := checkV3Dir(ctx, root, r.ad, overrides, lock, o.diff, stderr)
		record(out, stderr, r.dir, status, path, invalid, cerr, bump)
	}
	for _, target := range orphanedTargets(lock, resolved(loaded)) {
		fout(out, "ORPHANED\t%s\n", target)
		bump(1)
	}
	if worst == 0 {
		return nil
	}
	return silent(worst)
}

// orphanedTargets returns the lock's artifacts that no artifact dir renders, sorted.
// The lock and the tree must agree: a target with nothing left to render it is an
// artifact cc-guides still claims but can no longer reproduce — a renamed dir whose
// `target` key was never added, or a hand-edited lock. Reporting it is what keeps
// check from certifying that state OK, which it did while it only ever walked dirs.
func orphanedTargets(lock *lockfile.Lock, ads []*artifactDir) []string {
	if lock == nil {
		return nil
	}
	rendered := make(map[string]bool, len(ads))
	for _, ad := range ads {
		rendered[ad.target] = true
	}
	var orphaned []string
	for _, target := range lock.Artifacts {
		if !rendered[target] {
			orphaned = append(orphaned, target)
		}
	}
	sort.Strings(orphaned)
	return orphaned
}

// resolved returns the dirs that loaded, the batch the target gate can judge.
func resolved(results []dirResult) []*artifactDir {
	ads := make([]*artifactDir, 0, len(results))
	for _, r := range results {
		if r.err == nil {
			ads = append(ads, r.ad)
		}
	}
	return ads
}

// record emits one row (or an invalid-input diagnostic naming the artifact dir that
// produced it) and updates the worst code.
func record(out, stderr io.Writer, label, status, path string, invalid bool, err error, bump func(int)) {
	if invalid {
		fout(stderr, "cc-guides: %s: %v\n", label, err)
		bump(2)
		return
	}
	fout(out, "%s\t%s\n", status, path)
	if status != "OK" {
		bump(1)
	}
}

// checkV3Dir re-composes one artifact dir and byte-compares it to disk. The lock
// is the only pinning mechanism: a target the lock records is checked against its
// recorded commits; a target absent from the lock is invalid input (the repo must
// render to create the lock entry).
func checkV3Dir(ctx context.Context, root string, ad *artifactDir, overrides map[string]string, lock *lockfile.Lock, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	abs := filepath.Join(root, filepath.FromSlash(ad.target))
	disk, err := os.ReadFile(abs) // #nosec G304 -- reads the artifact target of a discovered dir
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING", ad.target, false, nil
		}
		return "", ad.target, true, err
	}
	if lock == nil || !lock.HasArtifact(ad.target) {
		return "", ad.target, true, fmt.Errorf("%w (%s)", errNoLock, ad.target)
	}
	return checkV3Locked(ctx, ad, disk, lock, overrides, diff, stderr)
}

// checkV3Locked pins every imported alias to the lock's recorded commit, hard-
// erroring if the layout's effective spec disagrees with (or is absent from) the
// lock. The disk comparison strips the marker (md/sh) or uses the raw body (json).
func checkV3Locked(ctx context.Context, ad *artifactDir, disk []byte, lock *lockfile.Lock, overrides map[string]string, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	specs := mergeSpecs(ad.lay.Sources, overrides)
	pinned := map[string]string{}
	for _, alias := range ad.lay.UsedAliases() {
		lp, ok := lock.Sources[alias]
		if !ok {
			return "", ad.target, true, fmt.Errorf("%w (%s imports %q with no lock entry)", errLockStale, ad.target, alias)
		}
		if lp.Spec != specs[alias] {
			return "", ad.target, true, fmt.Errorf("%w (%s source %q is %q, lock has %q)", errLockStale, ad.target, alias, specs[alias], lp.Spec)
		}
		pinned[alias] = lp.Commit
	}
	resolver, err := source.New(source.Options{Specs: specs, Pinned: pinned})
	if err != nil {
		return "", ad.target, true, err
	}
	body, err := ad.compose(ctx, resolver)
	if err != nil {
		return "", ad.target, true, err
	}
	// Run the same post-compose validation render runs, so a locked artifact whose
	// composed content fails a semantic check (a duplicate TOML table, a duplicate YAML
	// root key) fails check with render's message and exit code — never reporting OK
	// where render would refuse.
	if err := ad.kind.PostComposeValidate(body); err != nil {
		return "", ad.target, true, fmt.Errorf("%s: %w", ad.target, err)
	}
	diskBody := disk
	if ad.kind.Markered() {
		diskBody, _ = guide.StripMarker(ad.kind, disk)
	}
	return compareBodies(ad.target, diskBody, body, diff, stderr), ad.target, false, nil
}

// compareBodies byte-compares the artifact's on-disk body-after-marker against the
// recomposed body — only the body is compared, the marker is trusted.
func compareBodies(label string, diskBody, composed []byte, diff bool, stderr io.Writer) string {
	if bytes.Equal(diskBody, composed) {
		return "OK"
	}
	if diff {
		fout(stderr, "%s", guide.UnifiedDiff(label, diskBody, composed))
	}
	return "STALE"
}

// mergeSpecs overlays --source overrides onto a layout's sources.
func mergeSpecs(sources, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(sources)+len(overrides))
	for k, v := range sources {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}
