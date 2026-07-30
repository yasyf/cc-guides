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

// TOML allows a quoted table-header key, so a hand-edited or badly merged lock can
// carry an alias holding anything — a raw newline included. Parse once accepted that
// and Encode re-emitted it raw into `[sources.<alias>]`, writing a lock nothing could
// load: the same corruption class as a control character in a target, at an injection
// point the target guard never covered. A render cannot originate one (layout.Parse
// and --source both gate on ValidName), so an alias reaching here means the lock is
// already corrupt, and accepting it would let the next scoped render copy it through
// Merge and rewrite the lock unparseable.
func TestParseRejectsInvalidSourceAlias(t *testing.T) {
	tests := []struct {
		name  string
		alias string // as written inside a TOML quoted key
	}{
		{"newline escape", `evil\nalias`},
		{"carriage return escape", `evil\ralias`},
		{"embedded quote", `ev\"il`},
		{"closing bracket", `a]x`},
		{"uppercase", `CcSkills`},
		{"dot", `cc.skills`},
		{"empty", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "schema = 1\nversion = \"1.0.0\"\nartifacts = []\n\n" +
				"[sources.\"" + tt.alias + "\"]\n" +
				"spec = \"github:acme/x\"\n" +
				"commit = \"" + strings.Repeat("0", 40) + "\"\n"
			_, err := lockfile.Parse([]byte(raw))
			if err == nil {
				t.Fatalf("Parse accepted alias %q", tt.alias)
			}
			if !strings.Contains(err.Error(), "source alias") {
				t.Fatalf("alias %q must be refused as an alias, got: %v", tt.alias, err)
			}
		})
	}
}

// Encode crashes on an invalid alias rather than writing a header nothing can parse.
// A table header cannot go through quote without rewriting every committed lock, and
// Parse refuses to produce such a Lock, so reaching this means one was built by hand.
func TestEncodePanicsOnInvalidAlias(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Encode must panic on an alias it cannot write safely")
		}
	}()
	lk := &lockfile.Lock{
		Schema: 1, Version: "1.0.0",
		Sources: map[string]lockfile.SourcePin{"evil\nalias": {Spec: "github:acme/x", Commit: "local"}},
	}
	_ = lk.Encode()
}

// The fleet's committed locks must not churn: a valid alias still writes a bare,
// unquoted table header, byte for byte as before.
func TestEncodeKeepsAliasHeaderBare(t *testing.T) {
	got := string(sampleLock().Encode())
	for _, want := range []string{"\n[sources.cc-skills]\n", "\n[sources.team]\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("alias header must stay bare and unquoted, missing %q:\n%s", want, got)
		}
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
