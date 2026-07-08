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

	"github.com/yasyf/cc-guides/fragments"
	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/version"
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

// NewRootCmd builds the root command and registers its subcommands.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cc-guides",
		Short:         "Canonical agent guides as a shipped Go binary — render AGENTS.md, CLAUDE.md, and shell artifacts from embedded, versioned fragments",
		Version:       version.Bare(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(
		newRenderCmd(),
		newCheckCmd(),
		newInitCmd(),
		newListCmd(),
		newCatCmd(),
	)
	return root
}

// Execute runs the CLI and returns the process exit code: 0 ok · 1 drift · 2
// invalid input. Diagnostics go to stderr; machine output to stdout.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd()
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

// buildChain wires the override+embedded resolver chain rooted at the repo root
// of the current working directory.
func buildChain(fragmentsDir string) guide.Resolver {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	dir := fragmentsDir
	label := ".claude/fragments"
	if dir == "" {
		dir = filepath.Join(findRepoRoot(cwd), ".claude", "fragments")
	} else {
		label = filepath.ToSlash(dir)
	}
	return guide.NewChain(guide.NewDirResolver(dir, label), fragments.Resolver())
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

// collectSources returns the sources to operate on: the explicit args (validated
// as X.src.{md,sh}) or, when none are given, a pure discovery walk from cwd.
func collectSources(args []string) (explicit bool, sources []string, err error) {
	if len(args) > 0 {
		for _, a := range args {
			if !guide.IsSource(a) {
				return true, nil, fmt.Errorf("not a source file: %q (expected X.src.md or X.src.sh)", a)
			}
		}
		return true, args, nil
	}
	src, err := discoverSources()
	return false, src, err
}

// targetCollisions maps each source whose render target is unsafe to the reason.
// A target is unsafe when it is itself source-shaped (a source that would render
// onto a source), when two sources share one target, or when a target is also
// one of the selected sources. Detecting these before any write keeps a partial
// render from clobbering a source file.
func targetCollisions(sources []string) map[string]error {
	bad := map[string]error{}
	target := make(map[string]string, len(sources)) // source -> cleaned target
	byTarget := map[string][]string{}               // cleaned target -> sources
	srcSet := map[string]bool{}
	for _, s := range sources {
		srcSet[filepath.Clean(s)] = true
	}
	for _, s := range sources {
		a, err := guide.ArtifactPath(s)
		if err != nil {
			bad[s] = err
			continue
		}
		ca := filepath.Clean(a)
		target[s] = ca
		byTarget[ca] = append(byTarget[ca], s)
	}
	for _, s := range sources {
		ca, ok := target[s]
		if !ok {
			continue // already flagged with an ArtifactPath error
		}
		switch {
		case guide.IsSource(ca):
			bad[s] = fmt.Errorf("refusing to render %q: its target %q is itself a source file", s, ca)
		case len(byTarget[ca]) > 1:
			others := append([]string(nil), byTarget[ca]...)
			sort.Strings(others)
			bad[s] = fmt.Errorf("refusing to render %q: target %q is shared by %s", s, ca, strings.Join(others, ", "))
		case srcSet[ca]:
			bad[s] = fmt.Errorf("refusing to render %q: its target %q is also a selected source", s, ca)
		}
	}
	return bad
}

// collisionError folds targetCollisions into one deterministic error, or nil
// when every source has a safe, distinct target.
func collisionError(sources []string) error {
	bad := targetCollisions(sources)
	if len(bad) == 0 {
		return nil
	}
	keys := make([]string, 0, len(bad))
	for k := range bad {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	msgs := make([]string, len(keys))
	for i, k := range keys {
		msgs[i] = bad[k].Error()
	}
	return errors.New(strings.Join(msgs, "; "))
}

// discoverSources walks from cwd, skipping dot-dirs and symlinked dirs, and
// collects every *.src.md / *.src.sh, sorted.
func discoverSources() ([]string, error) {
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // WalkDir never descends symlinked dirs; skip symlink files too
		}
		if guide.IsSource(path) {
			out = append(out, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// bannerVersion resolves the effective banner version, warning once on stderr
// for a dev build.
func bannerVersion(override string, stderr io.Writer) string {
	v := override
	if v == "" {
		v = version.Bare()
	} else {
		v = strings.TrimPrefix(v, "v")
	}
	if v == "dev" {
		foutln(stderr, "cc-guides: warning: stamping a 'dev' banner version; artifacts will not match a released build (pass --banner-version or build with -ldflags)")
	}
	return v
}
