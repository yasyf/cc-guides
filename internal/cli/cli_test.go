package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/internal/cli"
)

func exec(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = cli.Execute(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G304 G703 -- test writes under t.TempDir(), not an attacker path
		t.Fatal(err)
	}
}

// repo sets up a temp repo (with .git) and chdirs into it.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

// guidesFixture writes a local guides dir usable as a --source override.
func guidesFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "md", "ccx.md"), "## Compact Context\nccx body\n")
	write(t, filepath.Join(dir, "sh", "install.sh"), "#!/bin/sh\nNAME=\"{{binary}}\"\necho \"$NAME\"\n")
	return dir
}

// srcFlag builds the --source override for the local guides fixture.
func srcFlag(fixture string) string { return "cc-skills=" + fixture }

func TestVersionExit(t *testing.T) {
	code, out, _ := exec("--version")
	if code != 0 || out != "dev\n" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
}

func TestRenderCheckV3RoundTrip(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\n  \"intro\",\n  \"cc-skills:ccx\",\n]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# Repo\n\nIntro prose.\n")

	if code, _, errout := exec("render", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("render exit = %d: %s", code, errout)
	}
	disk, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	s := string(disk)
	if !strings.HasPrefix(s, "<!-- cc-guides dev src=.claude/fragments/AGENTS.md fragments=local | GENERATED") {
		t.Fatalf("bad banner: %q", firstLine(s))
	}
	want := "# Repo\n\nIntro prose.\n\n## Compact Context\nccx body\n"
	if !strings.HasSuffix(s, want) {
		t.Fatalf("body:\n%s", s)
	}

	code, out, errout := exec("check", "--source", srcFlag(fixture))
	if code != 0 {
		t.Fatalf("check exit = %d: %s", code, errout)
	}
	if out != "OK\tAGENTS.md\n" {
		t.Fatalf("check out = %q", out)
	}
}

func TestCheckStaleAndMissing(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"cc-skills:ccx\"]\n")

	// MISSING before render.
	if code, out, _ := exec("check", "--source", srcFlag(fixture)); code != 1 || out != "MISSING\tAGENTS.md\n" {
		t.Fatalf("missing: code=%d out=%q", code, out)
	}
	if code, _, _ := exec("render", "--source", srcFlag(fixture)); code != 0 {
		t.Fatal("render failed")
	}
	// Corrupt the body -> STALE.
	disk, _ := os.ReadFile("AGENTS.md")
	write(t, "AGENTS.md", string(disk)+"tampered\n")
	if code, out, _ := exec("check", "--source", srcFlag(fixture)); code != 1 || out != "STALE\tAGENTS.md\n" {
		t.Fatalf("stale: code=%d out=%q", code, out)
	}
}

// Self-pinning: an artifact stamped by one binary version must check OK under a
// different binary version — check reproduces the banner's own version verbatim,
// so only the body is really compared.
func TestCheckSelfPinningCrossVersion(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"cc-skills:ccx\"]\n")

	if code, _, errout := exec("render", "--banner-version", "9.9.9", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("render exit = %d: %s", code, errout)
	}
	disk, _ := os.ReadFile("AGENTS.md")
	if !strings.HasPrefix(string(disk), "<!-- cc-guides 9.9.9 ") {
		t.Fatalf("banner: %q", firstLine(string(disk)))
	}
	// The running test binary is version "dev"; check must NOT false-STALE.
	if code, out, errout := exec("check", "--source", srcFlag(fixture)); code != 0 || out != "OK\tAGENTS.md\n" {
		t.Fatalf("cross-version check: code=%d out=%q err=%s", code, out, errout)
	}
}

// A layout with only local fragments (no imports) stamps fragments=none and needs
// no source at all.
func TestRenderFragmentsNone(t *testing.T) {
	repo(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# Repo\n\nOnly local prose.\n")
	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("render exit = %d: %s", code, errout)
	}
	disk, _ := os.ReadFile("AGENTS.md")
	if !strings.Contains(firstLine(string(disk)), "fragments=none") {
		t.Fatalf("banner must record fragments=none: %q", firstLine(string(disk)))
	}
	if code, out, _ := exec("check"); code != 0 || out != "OK\tAGENTS.md\n" {
		t.Fatalf("check: code=%d out=%q", code, out)
	}
}

func TestRenderShellArtifactWithArgs(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/plugin/scripts/install.sh/layout.toml",
		"fragments = [ { use = \"cc-skills:install\", args = { binary = \"slop-cop\" } } ]\n")

	if code, _, errout := exec("render", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("render exit = %d: %s", code, errout)
	}
	disk, err := os.ReadFile("plugin/scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(disk), "\n")
	if lines[0] != "#!/bin/sh" {
		t.Fatalf("line 1 = %q, want shebang", lines[0])
	}
	if !strings.HasPrefix(lines[1], "# cc-guides dev src=.claude/fragments/plugin/scripts/install.sh") {
		t.Fatalf("line 2 = %q, want banner", lines[1])
	}
	if !strings.Contains(string(disk), `NAME="slop-cop"`) {
		t.Fatalf("substitution missing:\n%s", disk)
	}
	info, _ := os.Stat("plugin/scripts/install.sh")
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("new .sh artifact must be executable, mode=%v", info.Mode().Perm())
	}
	if code, out, errout := exec("check", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("check exit=%d out=%q err=%s", code, out, errout)
	}
}

func TestDiscoveryGuardStrayFile(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# I\n")
	write(t, ".claude/fragments/AGENTS.md/stray.txt", "junk\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "stray file") {
		t.Fatalf("code=%d err=%q, want stray file error", code, errout)
	}
}

func TestDiscoveryGuardUnreferencedFragment(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# I\n")
	write(t, ".claude/fragments/AGENTS.md/orphan.fragment.md", "dropped\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "not referenced") {
		t.Fatalf("code=%d err=%q, want unreferenced error", code, errout)
	}
}

func TestDiscoveryGuardMissingLocalFragment(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\"]\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "do not exist") {
		t.Fatalf("code=%d err=%q, want missing-file error", code, errout)
	}
}

func TestDiscoveryGuardNestedSubdir(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"cc-skills:ccx\"]\n")
	write(t, ".claude/fragments/AGENTS.md/nested/layout.toml", "fragments = [\"cc-skills:ccx\"]\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "flat") {
		t.Fatalf("code=%d err=%q, want flatness error", code, errout)
	}
}

func TestBannerlessOverwriteRefused(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"cc-skills:ccx\"]\n")
	write(t, "AGENTS.md", "handwritten, no banner\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "without a cc-guides banner") {
		t.Fatalf("code=%d err=%q", code, errout)
	}
	if code, _, _ := exec("render", "--source", srcFlag(fixture), "--force"); code != 0 {
		t.Fatalf("--force exit = %d", code)
	}
}

func TestLint(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "md", "ok.md"), "## Clean\nbody\n")
	write(t, filepath.Join(dir, "sh", "ok.sh"), "#!/bin/sh\necho hi\n")
	if code, _, _ := exec("lint", dir); code != 0 {
		t.Fatalf("clean lint exit = %d", code)
	}
	// A CRLF fragment reddens the gate.
	write(t, filepath.Join(dir, "md", "bad.md"), "line\r\nbody\n")
	code, _, errout := exec("lint", dir)
	if code != 1 || !strings.Contains(errout, "CRLF") {
		t.Fatalf("dirty lint: code=%d err=%q", code, errout)
	}
}

func TestLintShellMustHaveShebang(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "sh", "noshebang.sh"), "echo hi\n")
	code, _, errout := exec("lint", dir)
	if code != 1 || !strings.Contains(errout, "shebang") {
		t.Fatalf("code=%d err=%q", code, errout)
	}
}

func TestMigrateRoundTrip(t *testing.T) {
	root := repo(t)
	fixture := guidesFixture(t)
	// A v1-shaped source and its deployed (v1-bannered) artifact.
	src := "# Repo\n\nIntro prose.\n\n{{> ccx}}\n\nMore prose.\n"
	write(t, "AGENTS.src.md", src)
	body := "# Repo\n\nIntro prose.\n\n## Compact Context\nccx body\n\nMore prose.\n"
	write(t, "AGENTS.md", "<!-- cc-guides 0.1.7 src=AGENTS.src.md | GENERATED — do not edit -->\n"+body)

	code, out, errout := exec("migrate", "--source", srcFlag(fixture), "AGENTS.src.md")
	if code != 0 {
		t.Fatalf("migrate exit=%d out=%q err=%s", code, out, errout)
	}
	if !strings.Contains(out, "MIGRATED") {
		t.Fatalf("migrate out = %q", out)
	}
	// The layout dir + fragment files exist; the .src is gone.
	if _, err := os.Stat(filepath.Join(root, ".claude/fragments/AGENTS.md/layout.toml")); err != nil {
		t.Fatalf("layout.toml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.src.md")); !os.IsNotExist(err) {
		t.Fatalf("source not removed: %v", err)
	}
	lt, _ := os.ReadFile(filepath.Join(root, ".claude/fragments/AGENTS.md/layout.toml")) // #nosec G304 -- test reads a temp-repo path it just wrote
	if !strings.Contains(string(lt), "cc-skills:ccx") {
		t.Fatalf("layout.toml:\n%s", lt)
	}
	// The re-rendered artifact byte-preserves the body and checks OK.
	art, _ := os.ReadFile(filepath.Join(root, "AGENTS.md")) // #nosec G304 -- test reads a temp-repo path it just wrote
	if !strings.HasSuffix(string(art), body) {
		t.Fatalf("migrated artifact body changed:\n%s", art)
	}
	if code, out, errout := exec("check", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("post-migrate check exit=%d out=%q err=%s", code, out, errout)
	}
}

func TestMigrateMismatchWritesNothing(t *testing.T) {
	root := repo(t)
	fixture := guidesFixture(t)
	write(t, "AGENTS.src.md", "# Repo\n\n{{> ccx}}\n")
	// Deployed artifact whose body does NOT match what compose would produce.
	write(t, "AGENTS.md", "<!-- cc-guides 0.1.7 src=AGENTS.src.md | GENERATED -->\n# Repo\n\nWRONG BODY\n")
	code, out, errout := exec("migrate", "--source", srcFlag(fixture), "AGENTS.src.md")
	if code != 1 || !strings.Contains(out, "MISMATCH") {
		t.Fatalf("code=%d out=%q, want MISMATCH exit 1", code, out)
	}
	// The self-verify diff must reach stderr so an operator can hand-fix.
	if !strings.Contains(errout, "WRONG BODY") || !strings.Contains(errout, "(rendered)") {
		t.Fatalf("mismatch diff not printed to stderr: %q", errout)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/fragments/AGENTS.md/layout.toml")); !os.IsNotExist(err) {
		t.Fatal("mismatch must write nothing")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.src.md")); err != nil {
		t.Fatal("mismatch must not remove the source")
	}
}

func TestCatImportAndLocal(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	code, out, _ := exec("cat", "--source", srcFlag(fixture), "cc-skills:ccx")
	if code != 0 || !strings.HasPrefix(out, "## Compact Context") {
		t.Fatalf("cat import: code=%d out=%q", code, firstLine(out))
	}
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# Local Intro\nbody\n")
	code, out, _ = exec("cat", "intro")
	if code != 0 || !strings.HasPrefix(out, "# Local Intro") {
		t.Fatalf("cat local: code=%d out=%q", code, firstLine(out))
	}
	if code, _, _ := exec("cat", "--source", srcFlag(fixture), "cc-skills:nonesuch"); code != 2 {
		t.Fatalf("unknown import exit = %d, want 2", code)
	}
}

func TestList(t *testing.T) {
	repo(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\", \"cc-skills:ccx\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# I\n")
	code, out, _ := exec("list")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	if !strings.Contains(out, "AGENTS.md\tmd\tintro,cc-skills:ccx") {
		t.Fatalf("list out = %q", out)
	}
}

func TestRenderV1Transitional(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, "AGENTS.src.md", "# Repo\n\n{{> ccx}}\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 0 {
		t.Fatalf("v1 render exit = %d: %s", code, errout)
	}
	if !strings.Contains(errout, "deprecated") {
		t.Fatalf("expected deprecation warning, got %q", errout)
	}
	disk, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(disk), "<!-- cc-guides dev src=AGENTS.src.md fragments=local | GENERATED") {
		t.Fatalf("v1 banner: %q", firstLine(string(disk)))
	}
	if code, out, errout := exec("check", "--source", srcFlag(fixture)); code != 0 {
		t.Fatalf("v1 check exit=%d out=%q err=%s", code, out, errout)
	}
}

func TestNoUnitsNotice(t *testing.T) {
	repo(t)
	code, out, errout := exec("render")
	if code != 0 || out != "" || !strings.Contains(errout, "no artifact dirs") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errout)
	}
}

func TestUnknownCommandExit2(t *testing.T) {
	repo(t)
	if code, _, _ := exec("frobnicate"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// #2: a LOCAL entry declaring args must be token-substituted with two-way strictness.
func TestRenderLocalFragmentArgs(t *testing.T) {
	repo(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [ { use = \"intro\", args = { name = \"repo\" } } ]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "Hello {{name}}.\n")
	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("render exit = %d: %s", code, errout)
	}
	disk, _ := os.ReadFile("AGENTS.md")
	if !strings.Contains(string(disk), "Hello repo.") || strings.Contains(string(disk), "{{name}}") {
		t.Fatalf("local args not substituted:\n%s", disk)
	}
}

func TestRenderLocalFragmentArgsStrict(t *testing.T) {
	repo(t)
	// An arg the local fragment never consumes must hard-error (two-way strictness).
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [ { use = \"intro\", args = { unused = \"x\" } } ]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "no tokens here\n")
	if code, _, errout := exec("render"); code != 2 || !strings.Contains(errout, "no matching {{token}}") {
		t.Fatalf("code=%d err=%q, want unused-arg error", code, errout)
	}
}

// #1: a whitespace-only local fragment is rejected, not rendered as stray blanks.
func TestRenderWhitespaceOnlyLocalFragmentRejected(t *testing.T) {
	repo(t)
	write(t, ".claude/fragments/AGENTS.md/layout.toml", "fragments = [\"intro\", \"body\"]\n")
	write(t, ".claude/fragments/AGENTS.md/intro.fragment.md", "# Head\n")
	write(t, ".claude/fragments/AGENTS.md/body.fragment.md", " \n \n")
	if code, _, errout := exec("render"); code != 2 || !strings.Contains(errout, "whitespace-only") {
		t.Fatalf("code=%d err=%q, want whitespace-only error", code, errout)
	}
}

// #3: an explicit v1 source arg whose target escapes the repo is rejected pre-write.
func TestRenderV1SourceEscapeRejected(t *testing.T) {
	repo(t)
	code, _, errout := exec("render", "../outside.src.md")
	if code != 2 || !strings.Contains(errout, "escapes the repo root") {
		t.Fatalf("code=%d err=%q, want escape error", code, errout)
	}
}

// #5: a directive-free v1 source renders fully offline (no cc-skills resolution).
func TestRenderDirectiveFreeV1Offline(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Just prose\n\nNo directives at all.\n")
	// No --source, no network: a directive-free source must still render.
	if code, _, errout := exec("render"); code != 0 {
		t.Fatalf("directive-free v1 render exit = %d: %s", code, errout)
	}
	disk, _ := os.ReadFile("AGENTS.md")
	if !strings.Contains(firstLine(string(disk)), "fragments=none") {
		t.Fatalf("directive-free banner must be fragments=none: %q", firstLine(string(disk)))
	}
}

// #11: a deployed artifact carrying a v1 banner (no fragments= field) must check OK,
// not STALE — the banner is echoed verbatim, never re-serialized as v2.
func TestCheckV1BannerVerbatimPassthrough(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, "AGENTS.src.md", "# Repo\n\n{{> ccx}}\n")
	// A genuine v1-bannered artifact whose body matches the composition.
	write(t, "AGENTS.md", "<!-- cc-guides 0.1.7 src=AGENTS.src.md | GENERATED — do not edit -->\n# Repo\n\n## Compact Context\nccx body\n")
	code, out, errout := exec("check", "--source", srcFlag(fixture))
	if code != 0 || out != "OK\tAGENTS.md\n" {
		t.Fatalf("v1 banner passthrough: code=%d out=%q err=%s", code, out, errout)
	}
}

// #14: a CRLF flat override in the transitional v1 render path is rejected.
func TestRenderV1CRLFOverrideRejected(t *testing.T) {
	repo(t)
	fixture := guidesFixture(t)
	write(t, "AGENTS.src.md", "# Repo\n\n{{> ccx}}\n")
	write(t, ".claude/fragments/ccx.md", "## Local\r\nbody\r\n")
	code, _, errout := exec("render", "--source", srcFlag(fixture))
	if code != 2 || !strings.Contains(errout, "CRLF") {
		t.Fatalf("code=%d err=%q, want CRLF error from the flat override", code, errout)
	}
	if _, err := os.Stat("AGENTS.md"); err == nil {
		t.Fatal("nothing must be written when a flat override is CRLF")
	}
}

// refuted-#8 (restored): a source whose render target is itself source-shaped is
// invalid input, and the preflight fails before any write.
func TestRenderSourceShapedTargetRejected(t *testing.T) {
	repo(t)
	write(t, "x.src.src.md", "# x\n\nprose only.\n")
	code, _, errout := exec("render")
	if code != 2 || !strings.Contains(errout, "source file") {
		t.Fatalf("code=%d err=%q, want source-shaped-target error", code, errout)
	}
	if _, err := os.Stat("x.src.md"); err == nil {
		t.Fatal("nothing must be written on preflight failure")
	}
}

func TestCheckSourceShapedTargetInvalid(t *testing.T) {
	repo(t)
	write(t, "x.src.src.md", "# x\n\nprose only.\n")
	code, out, errout := exec("check")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" || !strings.Contains(errout, "source file") {
		t.Fatalf("out=%q err=%q, want invalid not compared", out, errout)
	}
}

// #4/#9 end-to-end: migrate folds a flat v1 override into a local fragment and
// git-rm's the flat file.
func TestMigrateFoldsFlatOverride(t *testing.T) {
	root := repo(t)
	fixture := guidesFixture(t)
	write(t, "AGENTS.src.md", "# Repo\n\n{{> ccx}}\n")
	write(t, ".claude/fragments/ccx.md", "## Local ccx\nrepo-specific body\n")
	// The deployed artifact was v1-rendered with the override (local: markers).
	body := "# Repo\n\n<!-- local: .claude/fragments/ccx.md -->\n## Local ccx\nrepo-specific body\n<!-- /local: .claude/fragments/ccx.md -->\n"
	write(t, "AGENTS.md", "<!-- cc-guides 0.1.7 src=AGENTS.src.md | GENERATED -->\n"+body)

	code, out, errout := exec("migrate", "--source", srcFlag(fixture), "AGENTS.src.md")
	if code != 0 {
		t.Fatalf("migrate exit=%d out=%q err=%s", code, out, errout)
	}
	// The override became a local fragment; the flat file and the source are gone.
	frag, err := os.ReadFile(filepath.Join(root, ".claude/fragments/AGENTS.md/ccx.fragment.md")) // #nosec G304 -- test path
	if err != nil || !strings.Contains(string(frag), "repo-specific body") {
		t.Fatalf("override not folded to local fragment: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/fragments/ccx.md")); !os.IsNotExist(err) {
		t.Fatal("flat override file must be removed after folding")
	}
	lt, _ := os.ReadFile(filepath.Join(root, ".claude/fragments/AGENTS.md/layout.toml")) // #nosec G304 -- test path
	if strings.Contains(string(lt), "cc-skills:ccx") {
		t.Fatalf("folded override must not remain an import:\n%s", lt)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
