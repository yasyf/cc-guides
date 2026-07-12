package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/spf13/cobra"

	"github.com/yasyf/cc-guides/guide"
)

// lowerTokenRe matches a lowercase `{{token}}` substitution token.
var lowerTokenRe = regexp.MustCompile(`\{\{[a-z][a-z0-9-]*\}\}`)

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <dir>",
		Short: "Check a shared-fragments dir for purity (LF, single trailing newline, kind, sh shebang, json object)",
		Long: "Verify every fragment under <dir> (e.g. a content repo's guides/) is pure:\n" +
			"LF-only, exactly one trailing newline, non-empty, an extension matching its\n" +
			"kind subdir, markdown token-free, shell fragments carrying a shebang and no\n" +
			"leftover mustache markers, and json fragments a well-formed object (tokens\n" +
			"allowed). Exit 1 on any violation.",
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
	// named "README" (source.Resolver reads <dir>/<kind>/<name><ext>).
	if filepath.Base(rel) == "README.md" {
		if rel == "README.md" {
			return nil
		}
		return []string{fmt.Sprintf("%s: README.md is reserved documentation, not a fragment", rel)}
	}
	kind, err := guide.KindForPath(path)
	if err != nil {
		return []string{fmt.Sprintf("%s: unsupported extension (want .md or .sh)", rel)}
	}
	if parent := filepath.Base(filepath.Dir(path)); parent != kind.String() {
		return []string{fmt.Sprintf("%s: a %s fragment must live under a %s/ subdir, not %s/", rel, kind, kind, parent)}
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
	switch kind {
	case guide.KindMD:
		if m := lowerTokenRe.Find(body); m != nil {
			add(fmt.Sprintf("markdown fragment must be token-free, found %q", m))
		}
	case guide.KindSH:
		if !bytes.HasPrefix(body, []byte("#!/bin/sh\n")) {
			add("shell fragment must start with a #!/bin/sh shebang")
		}
		if bytes.Contains(body, []byte("{{#")) || bytes.Contains(body, []byte("{{/")) {
			add("leftover mustache block markers ({{# / {{/)")
		}
	case guide.KindJSON:
		if err := guide.LintJSON(body); err != nil {
			add(err.Error())
		}
	}
	return vs
}
