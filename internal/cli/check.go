package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	if err := preflightTargets(dirs); err != nil {
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
	for _, dir := range dirs {
		status, path, invalid, cerr := checkV3Dir(ctx, root, dir, overrides, lock, o.diff, stderr)
		record(out, stderr, dir, status, path, invalid, cerr, bump)
	}
	if worst == 0 {
		return nil
	}
	return silent(worst)
}

// record emits one row (or an invalid-input diagnostic) and updates the worst code.
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
func checkV3Dir(ctx context.Context, root, dir string, overrides map[string]string, lock *lockfile.Lock, diff bool, stderr io.Writer) (status, path string, invalid bool, err error) {
	ad, err := loadArtifactDir(root, dir)
	if err != nil {
		return "", dir, true, err
	}
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
	diskBody := disk
	if ad.kind != guide.KindJSON {
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
