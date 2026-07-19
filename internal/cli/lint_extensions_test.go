package cli_test

import (
	"path/filepath"
	"testing"
)

func TestLintUnsupportedExtensionDiagnostic(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "txt", "notes.txt"), "notes\n")

	code, _, errout := exec("lint", dir)
	const want = "cc-guides: txt/notes.txt: unsupported extension (want .md, .sh, .json, .yml, .yaml, or .toml)\n"
	if code != 1 || errout != want {
		t.Fatalf("lint exit = %d, stderr = %q; want exit 1, stderr %q", code, errout, want)
	}
}
