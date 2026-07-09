package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/migrate"
)

// TestWriteMigrationReportsAllRemoveFailures verifies that a removeSource failure
// does not abort the loop: the layout dir + artifact are still written, every
// failing path is reported together, and the call still returns an error.
func TestWriteMigrationReportsAllRemoveFailures(t *testing.T) {
	root := t.TempDir() // not a git repo, so removeSource falls back to os.Remove
	built := migrate.Output{
		LayoutDir:     ".claude/fragments/AGENTS.md",
		LayoutTOML:    []byte("# layout\n"),
		FragmentFiles: map[string][]byte{"core.fragment.md": []byte("core\n")},
		Artifact:      []byte("# rendered\n"),
	}
	// Both paths are absent, so git rm and the os.Remove fallback each fail.
	remove := []string{"gone-a.md", "gone-b.md"}

	err := writeMigration(root, built, "AGENTS.md", guide.KindMD, remove)
	if err == nil {
		t.Fatal("writeMigration = nil, want error reporting the failed removes")
	}
	for _, want := range remove {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention failed remove %q", err, want)
		}
	}

	// The functional migration must be on disk despite the remove failures.
	for _, rel := range []string{
		filepath.Join(".claude/fragments/AGENTS.md", "layout.toml"),
		filepath.Join(".claude/fragments/AGENTS.md", "core.fragment.md"),
		"AGENTS.md",
	} {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); statErr != nil {
			t.Errorf("expected %s written, stat error: %v", rel, statErr)
		}
	}
}
