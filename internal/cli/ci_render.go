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

	"github.com/yasyf/cc-guides/internal/version"
	"github.com/yasyf/cc-guides/lockfile"
)

// Default git identity for the re-render commit — the GitHub Actions bot, matching
// the historical re-render.yml. Overridable via flags or the CC_GUIDES_GIT_NAME /
// CC_GUIDES_GIT_EMAIL environment variables.
const (
	defaultGitName  = "github-actions[bot]"
	defaultGitEmail = "41898282+github-actions[bot]@users.noreply.github.com"
)

// ciRenderAttempts bounds the render → commit → push loop: a rejected push refetches
// and retries so a concurrent push cannot deadlock the fleet re-render.
const ciRenderAttempts = 3

type ciRenderOpts struct {
	gitName  string
	gitEmail string
}

func newCIRenderCmd(ctx context.Context) *cobra.Command {
	var o ciRenderOpts
	cmd := &cobra.Command{
		Use:   "ci-render",
		Short: "Render, gate on a pins-only lock diff, then commit and push (CI use)",
		Long: "Re-render every artifact, then reconcile the tree the way the fleet\n" +
			"re-render loop does: nothing changed -> exit 0; only the lock's\n" +
			"source commit pins moved -> revert and skip (the committed lock still\n" +
			"reproduces the artifacts byte-for-byte); otherwise commit with the\n" +
			"canonical message and push, refetching and retrying a rejected push up to\n" +
			"three times. Intended for CI: reads GITHUB_REF_NAME for the push branch and\n" +
			"records a `pushed` step output for the workflow's conditional self-check.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCIRender(ctx, cmd, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.gitName, "git-name", envOr("CC_GUIDES_GIT_NAME", defaultGitName), "git author/committer name for the re-render commit")
	f.StringVar(&o.gitEmail, "git-email", envOr("CC_GUIDES_GIT_EMAIL", defaultGitEmail), "git author/committer email for the re-render commit")
	return cmd
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runCIRender(ctx context.Context, cmd *cobra.Command, o ciRenderOpts) error {
	stderr := cmd.ErrOrStderr()
	root := repoRoot()
	if err := mustGit(ctx, root, "config", "user.name", o.gitName); err != nil {
		return exit(1, err)
	}
	if err := mustGit(ctx, root, "config", "user.email", o.gitEmail); err != nil {
		return exit(1, err)
	}

	pushed := false
	landed := false
	for attempt := 1; attempt <= ciRenderAttempts; attempt++ {
		// A full, authoritative re-render — identical to `cc-guides render` with no
		// path arguments. A render error aborts the whole command (matching set -e).
		if err := runRender(ctx, cmd, nil, renderOpts{}); err != nil {
			return err
		}
		// Stage everything so a first render's new, untracked lock counts too.
		if err := mustGit(ctx, root, "add", "-A", "--", "."); err != nil {
			return exit(1, err)
		}
		staged, err := hasStagedChanges(ctx, root)
		if err != nil {
			return exit(1, err)
		}
		if !staged {
			foutln(stderr, "already current — nothing to commit")
			landed = true
			break
		}

		lockOnly, err := stagedIsLockOnly(ctx, root)
		if err != nil {
			return exit(1, err)
		}
		if lockOnly {
			// Lock-only change. Skip (and revert) ONLY if every changed lock line is a
			// `commit = ` pin. Any version/spec/schema/artifacts/header change is
			// semantically required and must commit.
			diff, derr := gitCapture(ctx, root, "diff", "--cached", "-U0", "--", lockfile.Path)
			if derr != nil {
				return exit(1, derr)
			}
			if lockDiffPinsOnly(diff) {
				foutln(stderr, "only the lock's source commit pins moved and artifacts are byte-identical — skipping commit; the committed lock still checks green")
				if err := revertRender(ctx, root); err != nil {
					return exit(1, err)
				}
				landed = true
				break
			}
		}

		// Source pins for the commit message, read from the freshly written lock
		// (alias@sha12); an import-free layout locks no sources, so omit them (none).
		lockBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(lockfile.Path))) // #nosec G304 -- reads the repo's own freshly-rendered lock
		if err != nil {
			return exit(1, err)
		}
		pins := sourcePins(lockBytes)
		if pins == "" {
			pins = "none"
		}
		msg := fmt.Sprintf("chore: re-render guides (cc-guides %s, sources %s)", version.Bare(), pins)
		if err := mustGit(ctx, root, "commit", "-m", msg); err != nil {
			return exit(1, err)
		}
		ok, err := gitPush(ctx, root)
		if err != nil {
			return exit(1, err)
		}
		if ok {
			pushed = true
			landed = true
			break
		}
		fout(stderr, "push rejected (attempt %d) — refetching\n", attempt)
		if err := mustGit(ctx, root, "fetch", "origin"); err != nil {
			return exit(1, err)
		}
		if err := mustGit(ctx, root, "reset", "--hard", "origin/"+os.Getenv("GITHUB_REF_NAME")); err != nil {
			return exit(1, err)
		}
	}
	if !landed {
		return exit(1, fmt.Errorf("could not land the re-render after %d attempts", ciRenderAttempts))
	}
	if err := writePushedOutput(pushed); err != nil {
		return exit(1, err)
	}
	return nil
}

// lockDiffPinsOnly reports whether every content line of a `git diff --cached -U0`
// over the lock is a source commit pin change. Any other added or removed line,
// including a version change, makes it false. This deliberately diverges from the
// historical awk gate: version-field movement must land so a poisoned or regressed
// consumer lock self-heals on the next scheduled re-render. Removing the
// CI-forbidden local commit pin must also land. File and hunk headers are not
// content.
func lockDiffPinsOnly(diff string) bool {
	for _, line := range strings.Split(diff, "\n") {
		if line == "" || (line[0] != '+' && line[0] != '-') {
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if line == `-commit = "local"` {
			return false
		}
		if strings.HasPrefix(line, "+commit = ") || strings.HasPrefix(line, "-commit = ") {
			continue
		}
		return false
	}
	return true
}

// sourcePins builds the commit-message source-pin summary from a rendered lock:
// `alias@sha12` for each [sources.<alias>] table's commit, comma-joined in file
// order (the lock writes aliases sorted). An import-free lock yields "". Mirrors the
// historical awk over the lock (fields split on '"', commit truncated to 12 chars).
func sourcePins(lock []byte) string {
	var b strings.Builder
	alias := ""
	n := 0
	for _, line := range strings.Split(string(lock), "\n") {
		if strings.HasPrefix(line, "[sources.") {
			alias = strings.TrimSuffix(strings.TrimPrefix(line, "[sources."), "]")
			continue
		}
		if strings.HasPrefix(line, "commit = ") {
			parts := strings.Split(line, "\"")
			if len(parts) < 2 {
				continue
			}
			sha := parts[1]
			if len(sha) > 12 {
				sha = sha[:12]
			}
			if n > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%s@%s", alias, sha)
			n++
		}
	}
	return b.String()
}

// revertRender undoes a skipped re-render: unstage everything, restore tracked
// files, and clean an untracked lock (a first render's new lock is untracked, so
// checkout alone would leave it behind).
func revertRender(ctx context.Context, root string) error {
	if err := mustGit(ctx, root, "restore", "--staged", "--", "."); err != nil {
		return err
	}
	if err := mustGit(ctx, root, "checkout", "--", "."); err != nil {
		return err
	}
	return mustGit(ctx, root, "clean", "-fq", "--", lockfile.Path)
}

// writePushedOutput records the `pushed` step output (1 when a re-render commit was
// pushed, 0 otherwise) so the workflow can gate its self-check on it, matching the
// historical `echo "pushed=$pushed" >> "$GITHUB_OUTPUT"`. A no-op outside CI.
func writePushedOutput(pushed bool) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	v := "0"
	if pushed {
		v = "1"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) // #nosec G304 G302 G703 -- appends to the GitHub Actions step-output file named by the trusted GITHUB_OUTPUT env
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "pushed=%s\n", v)
	return err
}

// runGit executes git in dir, capturing stdout and stderr. err is non-nil only when
// git could not be started; a non-zero exit is reported through code with err nil, so
// callers can treat an exit status as a soft branch condition.
func runGit(ctx context.Context, dir string, args ...string) (stdout, stderr string, code int, err error) {
	c := exec.CommandContext(ctx, "git", args...) // #nosec G204 G702 -- fixed "git" command run without a shell; args are cc-guides' own constants, the lock path, and the trusted GITHUB_REF_NAME as a refspec
	c.Dir = dir
	var outb, errb bytes.Buffer
	c.Stdout = &outb
	c.Stderr = &errb
	runErr := c.Run()
	stdout, stderr = outb.String(), errb.String()
	if runErr == nil {
		return stdout, stderr, 0, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return stdout, stderr, ee.ExitCode(), nil
	}
	return stdout, stderr, -1, fmt.Errorf("running git %s: %w", strings.Join(args, " "), runErr)
}

// mustGit runs git and fails on a non-zero exit or a start error.
func mustGit(ctx context.Context, dir string, args ...string) error {
	stdout, stderr, code, err := runGit(ctx, dir, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		diag := strings.TrimSpace(stderr)
		if diag == "" {
			diag = strings.TrimSpace(stdout)
		}
		return fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), code, diag)
	}
	return nil
}

// gitCapture returns git's stdout, failing on a non-zero exit.
func gitCapture(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, stderr, code, err := runGit(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), code, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

// hasStagedChanges reports whether the index differs from HEAD via
// `git diff --cached --quiet` (exit 0 clean, exit 1 dirty).
func hasStagedChanges(ctx context.Context, dir string) (bool, error) {
	_, stderr, code, err := runGit(ctx, dir, "diff", "--cached", "--quiet")
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("git diff --cached --quiet exited %d: %s", code, strings.TrimSpace(stderr))
	}
}

// stagedIsLockOnly reports whether the only staged path is the cc-guides lock.
func stagedIsLockOnly(ctx context.Context, dir string) (bool, error) {
	out, err := gitCapture(ctx, dir, "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == lockfile.Path, nil
}

// gitPush pushes the current branch. ok is true on a clean push; a rejected push
// (any non-zero exit) returns ok false so the caller can refetch and retry. err is
// non-nil only when git could not be started.
func gitPush(ctx context.Context, dir string) (ok bool, err error) {
	_, _, code, err := runGit(ctx, dir, "push")
	if err != nil {
		return false, err
	}
	return code == 0, nil
}
