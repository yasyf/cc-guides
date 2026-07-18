package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
)

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <dir>",
		Short: "Check a shared-fragments dir for purity (LF, single trailing newline, kind, sh shebang, json object, yaml validity)",
		Long: "Verify every fragment under <dir> (e.g. a content repo's guides/) is pure:\n" +
			"LF-only, exactly one trailing newline, non-empty, an extension matching its\n" +
			"kind subdir, markdown token-free, shell fragments carrying a shebang and no\n" +
			"leftover mustache markers, json fragments a well-formed object, and yaml\n" +
			"fragments well-formed YAML (tokens allowed in both). Exit 1 on any violation.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, args[0])
		},
	}
}

func runLint(cmd *cobra.Command, dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return exit(2, err)
	}
	if !info.IsDir() {
		return exit(2, fmt.Errorf("lint expects a directory, got %q", dir))
	}
	var violations []string
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		violations = append(violations, lintFile(dir, path)...)
		return nil
	})
	if walkErr != nil {
		return exit(2, walkErr)
	}
	if len(violations) == 0 {
		return nil
	}
	sort.Strings(violations)
	stderr := cmd.ErrOrStderr()
	for _, v := range violations {
		foutln(stderr, "cc-guides:", v)
	}
	return silent(1)
}

// lintFile returns every purity violation for one file. The kind is fixed by the
// extension; a fragment whose extension has no kind, or whose parent subdir does
// not match its kind (md/ vs sh/), is itself a violation.
func lintFile(root, path string) []string {
	rel, _ := filepath.Rel(root, path)
	// README.md is reserved documentation for the pack, never a fragment. At the
	// pack root it is a legitimate doc and is skipped; anywhere below it (e.g.
	// md/README.md) it is rejected so it can never be resolved as the fragment
	// named "README" (source.Resolver reads <dir>/<kind>/<name><ext>). The name is
	// matched case-insensitively — README.MD / readme.md and friends must hit the
	// reservation too, so a case-insensitive filesystem can't smuggle a resolvable
	// readme into a kind dir. Only a genuine pack-root README (no directory
	// component, any case) is exempt.
	if strings.EqualFold(filepath.Base(rel), "README.md") {
		if filepath.Dir(rel) == "." {
			return nil
		}
		return []string{fmt.Sprintf("%s: README.md is reserved documentation, not a fragment", rel)}
	}
	kind, err := guide.KindForPath(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: unsupported extension (want .md, .sh, .json, or .yml)", rel)}
	}
	// The resolver reads a fragment ONLY at <pack>/<kind>/<name><ext>, so a kind
	// dir must sit directly under the pack root: a fragment's path relative to the
	// root must be exactly <kind>/<file>. A file at the root, or nested any deeper
	// (e.g. nested/json/x.json), is unreachable and rejected — a stricter check
	// than merely matching the immediate parent dir name.
	if dir := filepath.Dir(rel); dir != kind.String() {
		return []string{fmt.Sprintf("%s: a %s fragment must live at %s/<name>%s directly under the pack root, not %s/", rel, kind, kind, kind.Ext(), dir)}
	}
	body, readErr := os.ReadFile(path) // #nosec G304 -- lint reads the user-named content dir
	if readErr != nil {
		return []string{fmt.Sprintf("%s: %v", rel, readErr)}
	}

	var vs []string
	add := func(msg string) { vs = append(vs, rel+": "+msg) }
	if len(body) == 0 {
		add("empty fragment")
		return vs
	}
	if bytes.IndexByte(body, '\r') >= 0 {
		add("CRLF line endings (must be LF)")
	}
	if body[len(body)-1] != '\n' {
		add("must end with a newline")
	} else if bytes.HasSuffix(body, []byte("\n\n")) {
		add("must end with exactly one trailing newline")
	}
	for _, msg := range kind.Lint(body) {
		add(msg)
	}
	return vs
}
