package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A gitignore target renders by concatenation of its pattern fragments with a
// `#`-comment marker and 0o644 mode, round-trips through check, and a byte change to
// the managed artifact is STALE. Mirrors the toml e2e test.
func TestRenderCheckGitignore(t *testing.T) {
	repo(t)
	target := ".gitignore"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"base\", \"logs\"]\n")
	write(t, dir+"/base.fragment.gitignore", "# build output\nnode_modules/\n")
	write(t, dir+"/logs.fragment.gitignore", "*.log\n")

	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("gitignore render exit=%d: %s", code, errout)
	}
	disk, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(disk)
	if firstLine(s) != shMarker(dir) {
		t.Fatalf("bad gitignore marker: %q", firstLine(s))
	}
	want := "# build output\nnode_modules/\n\n*.log\n"
	if !strings.HasSuffix(s, want) {
		t.Fatalf("body:\n%s", s)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o644 {
		t.Errorf("gitignore artifact must be 0o644, got %v", info.Mode().Perm())
	}
	if code, out, errout := exec("check"); code != 0 || out != "OK\t"+target+"\n" {
		t.Fatalf("gitignore check: code=%d out=%q err=%s", code, out, errout)
	}
	// A byte change to the managed artifact (marker preserved) is STALE.
	write(t, target, s+"*.tmp\n")
	if code, out, _ := exec("check"); code != 1 || out != "STALE\t"+target+"\n" {
		t.Fatalf("gitignore tamper: code=%d out=%q", code, out)
	}
}

// A `target` key detaches the artifact from the dir's path: a dir named gitignore/
// renders .gitignore, so a repo need not carry a directory shadowing the file its
// tools read. The marker still names the real dir, and check round-trips.
func TestRenderCheckGitignoreTargetOverride(t *testing.T) {
	repo(t)
	target := ".gitignore"
	dir := ".claude/fragments/gitignore"
	write(t, dir+"/layout.toml", "target = \".gitignore\"\n\nfragments = [\"base\", \"logs\"]\n")
	write(t, dir+"/base.fragment.gitignore", "# build output\nnode_modules/\n")
	write(t, dir+"/logs.fragment.gitignore", "*.log\n")

	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("target-override render exit=%d: %s", code, errout)
	}
	disk, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(disk)
	if firstLine(s) != shMarker(dir) {
		t.Fatalf("marker must name the real dir: %q", firstLine(s))
	}
	want := "# build output\nnode_modules/\n\n*.log\n"
	if !strings.HasSuffix(s, want) {
		t.Fatalf("body:\n%s", s)
	}
	// The dir's own path is no longer a target, so nothing is written there.
	if _, err := os.Stat("gitignore"); !os.IsNotExist(err) {
		t.Fatalf("an overridden dir must not also render its own path: %v", err)
	}
	lock, _ := os.ReadFile(".claude/fragments/cc-guides.lock")
	if !strings.Contains(string(lock), `artifacts = [".gitignore"]`) {
		t.Fatalf("lock must register the overridden target:\n%s", lock)
	}
	if code, out, errout := exec("check"); code != 0 || out != "OK\t"+target+"\n" {
		t.Fatalf("target-override check: code=%d out=%q err=%s", code, out, errout)
	}
	// A byte change to the managed artifact (marker preserved) is STALE.
	write(t, target, s+"*.tmp\n")
	if code, out, _ := exec("check"); code != 1 || out != "STALE\t"+target+"\n" {
		t.Fatalf("target-override tamper: code=%d out=%q", code, out)
	}
}

// A nested gitignore target (docs/.gitignore) discovers, renders, and checks the same
// way — filepath.Ext(".gitignore") is the whole dotfile name, so dispatch is unchanged
// under a subdir.
func TestRenderCheckGitignoreNested(t *testing.T) {
	repo(t)
	target := "docs/.gitignore"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"base\"]\n")
	write(t, dir+"/base.fragment.gitignore", "_site/\n")

	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("nested gitignore render exit=%d: %s", code, errout)
	}
	disk, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(disk)
	if firstLine(s) != shMarker(dir) {
		t.Fatalf("bad nested gitignore marker: %q", firstLine(s))
	}
	if !strings.HasSuffix(s, "_site/\n") {
		t.Fatalf("body:\n%s", s)
	}
	if code, out, errout := exec("check"); code != 0 || out != "OK\t"+target+"\n" {
		t.Fatalf("nested gitignore check: code=%d out=%q err=%s", code, out, errout)
	}
}

// lint validates gitignore fragments: a well-formed pattern set (token-bearing
// included) under a gitignore/ pack subdir is clean.
func TestLintGitignore(t *testing.T) {
	good := t.TempDir()
	write(t, filepath.Join(good, "gitignore", "x.gitignore"), "node_modules/\n*.log\n")
	// {{token}} placeholders are tolerated (substituted at compose).
	write(t, filepath.Join(good, "gitignore", "token.gitignore"), "{{dir}}/\n")
	if code, _, errout := exec("lint", good); code != 0 {
		t.Fatalf("clean gitignore lint: code=%d err=%s", code, errout)
	}
}
