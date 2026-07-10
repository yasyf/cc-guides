package lockfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/lockfile"
)

func sampleLock() *lockfile.Lock {
	return &lockfile.Lock{
		Schema:    1,
		Version:   "0.1.87",
		Artifacts: []string{"CLAUDE.md", "AGENTS.md"},
		Sources: map[string]lockfile.SourcePin{
			"team":      {Spec: "github:acme/guides//g@v1", Commit: "0123456789abcdef0123456789abcdef01234567"},
			"cc-skills": {Spec: "github:yasyf/cc-skills@main", Commit: "abcdef0123456789abcdef0123456789abcdef01"},
		},
	}
}

func TestLockRoundTrip(t *testing.T) {
	lk := sampleLock()
	back, err := lockfile.Parse(lk.Encode())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if back.Schema != 1 || back.Version != "0.1.87" {
		t.Fatalf("scalars lost: %+v", back)
	}
	if len(back.Artifacts) != 2 || len(back.Sources) != 2 {
		t.Fatalf("collections lost: %+v", back)
	}
	if back.Sources["cc-skills"].Commit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("source pin lost: %+v", back.Sources)
	}
}

func TestLockEncodeDeterministic(t *testing.T) {
	a := string(sampleLock().Encode())
	b := string(sampleLock().Encode())
	if a != b {
		t.Fatalf("non-deterministic encode:\n%s\n---\n%s", a, b)
	}
	// Artifacts and source tables are sorted, and the file leads with the guard.
	if !strings.HasPrefix(a, "# Written by 'cc-guides render' — do not edit.\n") {
		t.Fatalf("missing header:\n%s", a)
	}
	if !strings.Contains(a, `artifacts = ["AGENTS.md", "CLAUDE.md"]`) {
		t.Fatalf("artifacts not sorted:\n%s", a)
	}
	if strings.Index(a, "[sources.cc-skills]") > strings.Index(a, "[sources.team]") {
		t.Fatalf("source tables not alias-sorted:\n%s", a)
	}
	if !strings.HasSuffix(a, "\n") || strings.HasSuffix(a, "\n\n\n") {
		t.Fatalf("bad trailing newline:\n%q", a)
	}
}

func TestLockMerge(t *testing.T) {
	existing := &lockfile.Lock{
		Schema:    1,
		Version:   "0.1.80",
		Artifacts: []string{"AGENTS.md", "plugin/x.sh"},
		Sources: map[string]lockfile.SourcePin{
			"cc-skills": {Spec: "github:yasyf/cc-skills@main", Commit: "aaaa"},
			"team":      {Spec: "github:acme/g//g", Commit: "bbbb"},
		},
	}
	fresh := &lockfile.Lock{
		Schema:    1,
		Version:   "0.1.87",
		Artifacts: []string{"AGENTS.md"},
		Sources: map[string]lockfile.SourcePin{
			"cc-skills": {Spec: "github:yasyf/cc-skills@main", Commit: "cccc"},
		},
	}
	m := lockfile.Merge(existing, fresh)
	if m.Version != "0.1.87" {
		t.Fatalf("fresh version must win: %q", m.Version)
	}
	if len(m.Artifacts) != 2 || m.Artifacts[0] != "AGENTS.md" || m.Artifacts[1] != "plugin/x.sh" {
		t.Fatalf("artifacts union wrong: %v", m.Artifacts)
	}
	if m.Sources["cc-skills"].Commit != "cccc" {
		t.Fatalf("touched source must overwrite: %+v", m.Sources["cc-skills"])
	}
	if m.Sources["team"].Commit != "bbbb" {
		t.Fatalf("untouched source must be preserved: %+v", m.Sources["team"])
	}
	if lockfile.Merge(nil, fresh) != fresh {
		t.Fatal("Merge(nil, fresh) must return fresh")
	}
}

// Parse rejects a source pin whose commit is neither a full 40-char sha nor the
// literal "local"; empty, "none", abbreviated, and non-hex are all parse errors.
func TestParseValidatesCommit(t *testing.T) {
	good := "0123456789abcdef0123456789abcdef01234567"
	lockWith := func(commit string) []byte {
		return []byte("schema = 1\nversion = \"1.0\"\nartifacts = [\"AGENTS.md\"]\n\n" +
			"[sources.cc-skills]\nspec = \"github:yasyf/cc-skills@main\"\ncommit = \"" + commit + "\"\n")
	}
	for _, bad := range []string{"", "none", "abc123", good[:39], strings.ToUpper(good), "0123456789abcdef0123456789abcdef012345678"} {
		if _, err := lockfile.Parse(lockWith(bad)); err == nil {
			t.Errorf("Parse accepted invalid commit %q", bad)
		}
	}
	for _, ok := range []string{good, "local"} {
		if _, err := lockfile.Parse(lockWith(ok)); err != nil {
			t.Errorf("Parse rejected valid commit %q: %v", ok, err)
		}
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	if _, present, err := lockfile.Load(root); err != nil || present {
		t.Fatalf("missing lock: present=%v err=%v", present, err)
	}
	p := filepath.Join(root, filepath.FromSlash(lockfile.Path))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, sampleLock().Encode(), 0o600); err != nil {
		t.Fatal(err)
	}
	lk, present, err := lockfile.Load(root)
	if err != nil || !present {
		t.Fatalf("load: present=%v err=%v", present, err)
	}
	if !lk.HasArtifact("AGENTS.md") || lk.HasArtifact("nope.md") {
		t.Fatalf("HasArtifact wrong: %v", lk.Artifacts)
	}
}
