// Package cli builds the cobra command tree and owns exit-code mapping.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/version"
	"github.com/yasyf/cc-guides/layout"
)

// ExitError carries a specific process exit code out of a command. A nil Err
// prints nothing (the command already wrote its own machine output).
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

func exit(code int, err error) *ExitError { return &ExitError{Code: code, Err: err} }
func silent(code int) *ExitError          { return &ExitError{Code: code} }

// fout / foutln write to a CLI stream. A write error to stdout/stderr is
// unrecoverable, so it is deliberately ignored here (keeps call sites terse and
// errcheck-clean).
func fout(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func foutln(w io.Writer, a ...any)              { _, _ = fmt.Fprintln(w, a...) }

// NewRootCmd builds the root command and registers its subcommands. ctx is
// captured by the context-taking subcommands' RunE closures, so the request
// context flows as a parameter from Execute all the way down.
func NewRootCmd(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:           "cc-guides",
		Short:         "Compose AGENTS.md, CLAUDE.md, and shell artifacts from .claude/fragments layouts and shared, imported guides",
		Version:       version.Bare(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		newRenderCmd(ctx),
		newCheckCmd(ctx),
		newLintCmd(),
		newListCmd(),
		newCatCmd(ctx),
	)
	return root
}

// Execute runs the CLI and returns the process exit code: 0 ok · 1 drift · 2
// invalid input. Diagnostics go to stderr; machine output to stdout.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd(ctx)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		if ee.Err != nil {
			foutln(stderr, "cc-guides:", ee.Err)
		}
		return ee.Code
	}
	// Cobra usage/flag errors and any unexpected failure are invalid input.
	foutln(stderr, "cc-guides:", err)
	return 2
}

// repoRoot resolves the repo root from cwd (nearest ancestor with .git), falling
// back to cwd. All artifact-dir paths are anchored here and stored repo-relative
// (slash form), so the marker `src=` and lock entries match on any machine.
func repoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return findRepoRoot(cwd)
}

// findRepoRoot walks up from start to the nearest ancestor containing .git; it
// returns start when none is found.
func findRepoRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// parseSourceOverrides parses repeated `--source alias=spec` flags into a map. A
// spec beginning `github:` pins by sha; any other value is a local directory read
// in place (dev/E2E), which stamps `fragments=local`.
func parseSourceOverrides(flags []string) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range flags {
		alias, spec, ok := strings.Cut(f, "=")
		if !ok || alias == "" || spec == "" {
			return nil, fmt.Errorf("--source must be alias=spec, got %q", f)
		}
		if !guide.ValidName(alias) {
			return nil, fmt.Errorf("--source alias %q is invalid", alias)
		}
		out[alias] = spec
	}
	return out, nil
}

// discoverArtifactDirs walks <repoRoot>/.claude/fragments explicitly (the default
// walk skips dot-dirs, so .claude would be invisible) and returns every dir that
// holds a layout.toml, repo-relative and slash-formed. An artifact dir is not
// descended into: it must be flat, and its contents are validated separately.
func discoverArtifactDirs(root string) ([]string, error) {
	base := filepath.Join(root, filepath.FromSlash(guide.FragmentsRoot))
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, "layout.toml")); statErr == nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			dirs = append(dirs, filepath.ToSlash(rel))
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

// collectDirs resolves the artifact dirs for render/check: the explicit args
// (each must be an artifact dir) or, with no args, a discovery from the repo root.
func collectDirs(root string, args []string) ([]string, error) {
	if len(args) == 0 {
		return discoverArtifactDirs(root)
	}
	var dirs []string
	for _, a := range args {
		rel := filepath.ToSlash(strings.TrimSuffix(a, "/"))
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel), "layout.toml")); statErr != nil {
			return nil, fmt.Errorf("%q is not an artifact dir (no layout.toml)", a)
		}
		dirs = append(dirs, rel)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// bannerVersion resolves the effective version recorded in the lock, warning once
// on stderr for a dev build.
func bannerVersion(override string, stderr io.Writer) string {
	v := override
	if v == "" {
		v = version.Bare()
	} else {
		v = strings.TrimPrefix(v, "v")
	}
	if v == "dev" {
		foutln(stderr, "cc-guides: warning: recording a 'dev' version in the lock; artifacts will not match a released build (pass --banner-version or build with -ldflags)")
	}
	return v
}

// discoveredSpecs unions the [sources.*] of every artifact dir in the repo with
// --source overrides, so an inspection command (cat) resolves an alias exactly as
// render would. A malformed layout.toml is a hard error.
func discoveredSpecs(root string, overrides map[string]string) (map[string]string, error) {
	dirs, err := discoverArtifactDirs(root)
	if err != nil {
		return nil, err
	}
	layouts := map[string]*layout.Layout{}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(dir), "layout.toml")) // #nosec G304 -- reads the layout.toml of a discovered artifact dir
		if err != nil {
			return nil, err
		}
		lay, err := layout.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		layouts[dir] = lay
	}
	return unionSpecs(layouts, overrides)
}

// unionSpecs merges every artifact dir's [sources.*] into one spec map so a single
// resolver serves the whole run (resolve-once-per-process). A conflicting alias
// (same name, different spec across dirs) is a hard error. --source overrides win.
func unionSpecs(layouts map[string]*layout.Layout, overrides map[string]string) (map[string]string, error) {
	specs := map[string]string{}
	for dir, lay := range layouts {
		for alias, spec := range lay.Sources {
			if prev, ok := specs[alias]; ok && prev != spec {
				return nil, fmt.Errorf("source alias %q maps to different specs across artifact dirs (%q vs %q, e.g. in %s)", alias, prev, spec, dir)
			}
			specs[alias] = spec
		}
	}
	for alias, spec := range overrides {
		specs[alias] = spec
	}
	return specs, nil
}
