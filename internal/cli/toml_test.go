package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A toml target renders by concatenation of disjoint tables with a `#`-comment marker
// and 0o644 mode, discovers under a nested hidden dir, round-trips through check, and a
// byte change to the managed artifact is STALE. Mirrors the yaml e2e test.
func TestRenderCheckTOML(t *testing.T) {
	repo(t)
	target := ".claude/capt-hook.toml"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"fixes\", \"go\"]\n")
	write(t, dir+"/fixes.fragment.toml", "# the safety pack\n[packs.fixes]\nsource = \"builtin\"\n")
	write(t, dir+"/go.fragment.toml", "[packs.go]\nsource = \"builtin\"\n")

	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("toml render exit=%d: %s", code, errout)
	}
	disk, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	s := string(disk)
	if firstLine(s) != shMarker(dir) {
		t.Fatalf("bad toml marker: %q", firstLine(s))
	}
	want := "# the safety pack\n[packs.fixes]\nsource = \"builtin\"\n\n[packs.go]\nsource = \"builtin\"\n"
	if !strings.HasSuffix(s, want) {
		t.Fatalf("body:\n%s", s)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o644 {
		t.Errorf("toml artifact must be 0o644, got %v", info.Mode().Perm())
	}
	if code, out, errout := exec("check"); code != 0 || out != "OK\t"+target+"\n" {
		t.Fatalf("toml check: code=%d out=%q err=%s", code, out, errout)
	}
	// A byte change to the managed artifact (marker preserved) is STALE.
	write(t, target, s+"[packs.extra]\nsource = \"builtin\"\n")
	if code, out, _ := exec("check"); code != 1 || out != "STALE\t"+target+"\n" {
		t.Fatalf("toml tamper: code=%d out=%q", code, out)
	}
}

// Post-compose validation at render: two individually-valid toml fragments that
// redefine the same table fail with a message naming the target and the offending
// table, and nothing is written (the render refuses before any output).
func TestRenderTOMLDuplicateTableFails(t *testing.T) {
	repo(t)
	target := ".claude/capt-hook.toml"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"a\", \"b\"]\n")
	write(t, dir+"/a.fragment.toml", "[packs.general]\nsource = \"builtin\"\n")
	write(t, dir+"/b.fragment.toml", "[packs.general]\nsource = \"other\"\n")

	code, _, errout := exec("render")
	if code != 2 {
		t.Fatalf("duplicate-table render must exit 2, got %d (err=%q)", code, errout)
	}
	if !strings.Contains(errout, target) || !strings.Contains(errout, "packs.general") {
		t.Fatalf("error must name the target and the offending table, got %q", errout)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("a post-compose failure must not write the artifact")
	}
	if _, err := os.Stat(".claude/fragments/cc-guides.lock"); !os.IsNotExist(err) {
		t.Fatal("a post-compose failure must not advance the lock")
	}
}

// check runs the same post-compose validation render runs: a locked TOML artifact whose
// content fails the semantic check (a duplicate table across fragments) must fail check
// with a non-zero exit naming the offending table — never reporting OK where render
// refuses. The lock is bootstrapped by a valid render, then the fragments and the
// on-disk artifact are corrupted to the (invalid) recomposed body so a byte-compare
// alone would pass.
func TestCheckTOMLDuplicateTableFails(t *testing.T) {
	repo(t)
	target := ".claude/capt-hook.toml"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"a\", \"b\"]\n")
	write(t, dir+"/a.fragment.toml", "[packs.general]\nsource = \"builtin\"\n")
	write(t, dir+"/b.fragment.toml", "[packs.go]\nsource = \"builtin\"\n")
	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("bootstrap render exit=%d: %s", code, errout)
	}
	write(t, dir+"/b.fragment.toml", "[packs.general]\nsource = \"other\"\n")
	body := "[packs.general]\nsource = \"builtin\"\n\n[packs.general]\nsource = \"other\"\n"
	write(t, target, shMarker(dir)+"\n"+body)

	code, _, errout := exec("check")
	if code != 2 {
		t.Fatalf("check must exit 2 on a locked artifact that fails post-compose validation, got %d (err=%q)", code, errout)
	}
	if !strings.Contains(errout, "packs.general") {
		t.Fatalf("check error must name the offending table, got %q", errout)
	}
}

// The same for a locked YAML artifact whose content composes to a duplicate top-level
// key (yaml.v3 rejects it): check must fail rather than report OK.
func TestCheckYAMLDuplicateKeyFails(t *testing.T) {
	repo(t)
	target := ".github/workflows/x.yml"
	dir := ".claude/fragments/" + target
	write(t, dir+"/layout.toml", "fragments = [\"a\", \"b\"]\n")
	write(t, dir+"/a.fragment.yml", "name: CI\non: push\n")
	write(t, dir+"/b.fragment.yml", "other: x\n")
	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("bootstrap render exit=%d: %s", code, errout)
	}
	write(t, dir+"/b.fragment.yml", "name: Other\n")
	body := "name: CI\non: push\n\nname: Other\n"
	write(t, target, ymlMarker(dir)+"\n"+body)

	if code, _, errout := exec("check"); code != 2 {
		t.Fatalf("check must exit 2 on a locked yaml artifact with a duplicate root key, got %d (err=%q)", code, errout)
	}
}

// lint validates toml fragments: a well-formed table set (token-bearing included) is
// clean, and each impurity reddens the gate with its own message in isolation — a
// duplicate table WITHIN one fragment (which the grammar accepts but the decoder
// rejects) among them.
func TestLintTOML(t *testing.T) {
	good := t.TempDir()
	write(t, filepath.Join(good, "toml", "ok.toml"), "[packs.general]\nsource = \"builtin\"\n")
	// {{token}} placeholders are tolerated in a value or a whole-scalar position.
	write(t, filepath.Join(good, "toml", "token.toml"), "[tool]\nname = \"{{binary}}\"\ncount = {{n}}\n")
	if code, _, errout := exec("lint", good); code != 0 {
		t.Fatalf("clean toml lint: code=%d err=%s", code, errout)
	}
	for _, tc := range []struct{ name, content, want string }{
		{"duplicate table", "[a]\nx = 1\n[a]\ny = 2\n", "TOML"},
		{"malformed", "[a\nx = 1\n", "TOML"},
		{"crlf", "[a]\r\n", "CRLF"},
		{"double trailing newline", "[a]\nx = 1\n\n", "exactly one trailing newline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "toml", "bad.toml"), tc.content)
			if code, _, errout := exec("lint", dir); code != 1 || !strings.Contains(errout, tc.want) {
				t.Fatalf("code=%d err=%q, want %q", code, errout, tc.want)
			}
		})
	}
}

// A toml fragment cannot be imported into an artifact of another kind, and vice versa
// — the kind-mismatch diagnostic now covers toml too (AllKinds includes it).
func TestLintTOMLUnsupportedNesting(t *testing.T) {
	dir := t.TempDir()
	// A toml fragment must live at toml/<name>.toml directly under the pack root.
	write(t, filepath.Join(dir, "nested", "toml", "x.toml"), "[a]\nx = 1\n")
	code, _, errout := exec("lint", dir)
	if code != 1 || !strings.Contains(errout, "directly under the pack root") {
		t.Fatalf("nested toml kind dir lint: code=%d err=%q", code, errout)
	}
}
