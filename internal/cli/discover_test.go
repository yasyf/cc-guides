package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func mkFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverArtifactDirs(t *testing.T) {
	root := t.TempDir()
	mkFile(t, root, ".claude/fragments/AGENTS.md/layout.toml", "fragments=[\"x\"]\n")
	mkFile(t, root, ".claude/fragments/AGENTS.md/x.fragment.md", "x\n")
	mkFile(t, root, ".claude/fragments/plugin/scripts/install.sh/layout.toml", "fragments=[\"cc-skills:y\"]\n")
	// An intermediate dir with no layout.toml is not an artifact dir.
	mkFile(t, root, ".claude/fragments/plugin/README.md", "note")

	got, err := discoverArtifactDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".claude/fragments/AGENTS.md", ".claude/fragments/plugin/scripts/install.sh"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

func TestDiscoverArtifactDirsNoFragmentsRoot(t *testing.T) {
	got, err := discoverArtifactDirs(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("got %v err %v, want nil/nil", got, err)
	}
}
