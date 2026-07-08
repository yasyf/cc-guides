package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSourcesSkipsDotDirsAndSymlinks(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mk("AGENTS.src.md", "a")
	mk("sub/install-binary.src.sh", "b")
	mk("normal/CLAUDE.src.md", "c")
	mk(".hidden/H.src.md", "skip me")  // dot-dir: skipped
	mk(".git/G.src.md", "skip me")     // dot-dir: skipped
	mk("README.md", "not a source")    // not a source
	mk("notes.src.txt", "unsupported") // unsupported ext

	// A symlinked directory whose target lives OUTSIDE the walk root; its sources
	// must never be discovered because WalkDir does not descend symlinks.
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "E.src.md"), []byte("e"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	t.Chdir(root)
	got, err := discoverSources()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"AGENTS.src.md", "normal/CLAUDE.src.md", "sub/install-binary.src.sh"}
	if len(got) != len(want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("discovered %v, want %v", got, want)
		}
	}
}
