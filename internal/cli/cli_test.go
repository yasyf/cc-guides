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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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

func TestVersionExit(t *testing.T) {
	code, out, _ := exec("--version")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if out != "dev\n" {
		t.Fatalf("version output = %q, want dev\\n", out)
	}
}

func TestRenderAndCheckRoundTrip(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Head\n\nIntro {{VAR}}.\n\n{{> ccx}}\n\nEnd.\n")

	if code, _, _ := exec("render"); code != 0 {
		t.Fatalf("render exit = %d", code)
	}
	disk, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatalf("artifact not written: %v", err)
	}
	if !strings.HasPrefix(string(disk), "<!-- cc-guides dev src=AGENTS.src.md | GENERATED") {
		t.Fatalf("bad banner: %q", firstLine(string(disk)))
	}
	if !strings.Contains(string(disk), "Intro {{VAR}}.") {
		t.Fatal("prose token must pass through")
	}

	code, out, _ := exec("check")
	if code != 0 {
		t.Fatalf("check exit = %d", code)
	}
	if out != "OK\tAGENTS.md\n" {
		t.Fatalf("check out = %q", out)
	}
}

func TestCheckStale(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Head\n\n{{> ccx}}\n")
	// Render with a different banner version so only the banner line differs.
	if code, _, _ := exec("render", "--banner-version", "9.9.9"); code != 0 {
		t.Fatalf("render exit = %d", code)
	}
	code, out, _ := exec("check")
	if code != 1 {
		t.Fatalf("check exit = %d, want 1", code)
	}
	if out != "STALE\tAGENTS.md\n" {
		t.Fatalf("check out = %q, want STALE", out)
	}
}

func TestCheckMissing(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Head\n\n{{> ccx}}\n")
	code, out, _ := exec("check")
	if code != 1 {
		t.Fatalf("check exit = %d, want 1", code)
	}
	if out != "MISSING\tAGENTS.md\n" {
		t.Fatalf("check out = %q, want MISSING", out)
	}
}

func TestCheckInvalidInputExit2(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "{{> _partials/ccx.md}}\n") // legacy path form
	code, _, errout := exec("check")
	if code != 2 {
		t.Fatalf("check exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "legacy path-form") {
		t.Fatalf("stderr = %q", errout)
	}
}

func TestCheckMixedRunTwoWins(t *testing.T) {
	repo(t)
	write(t, "good.src.md", "# g\n\n{{> ccx}}\n")
	exec("render") // makes good.md
	write(t, "bad.src.md", "{{> _partials/ccx.md}}\n")
	code, _, _ := exec("check")
	if code != 2 {
		t.Fatalf("mixed run exit = %d, want 2 (invalid wins over ok)", code)
	}
}

func TestCheckZeroSourcesNoticeExit0(t *testing.T) {
	repo(t)
	code, out, errout := exec("check")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if out != "" {
		t.Fatalf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errout, "no *.src.* sources found") {
		t.Fatalf("stderr = %q", errout)
	}
}

func TestRenderBannerlessOverwriteRefused(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Head\n\n{{> ccx}}\n")
	write(t, "AGENTS.md", "handwritten, no banner\n")
	code, _, errout := exec("render")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "without a cc-guides banner") {
		t.Fatalf("stderr = %q", errout)
	}
	// --force overrides.
	if code, _, _ := exec("render", "--force"); code != 0 {
		t.Fatalf("--force exit = %d", code)
	}
}

func TestRenderHandwrittenBannerOnLine2Refused(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# Head\n\n{{> ccx}}\n")
	// Handwritten prose whose SECOND line coincidentally matches the banner
	// regex. For .md the banner must be line 1, so the artifact is bannerless and
	// render must refuse to clobber it without --force.
	write(t, "AGENTS.md", "real handwritten prose\n<!-- cc-guides 1.0 src=AGENTS.src.md | GENERATED -->\nmore prose\n")
	code, _, errout := exec("render")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "without a cc-guides banner") {
		t.Fatalf("stderr = %q", errout)
	}
	// The handwritten file is untouched until --force is passed.
	disk, err := os.ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(disk), "real handwritten prose") {
		t.Fatalf("handwritten file was clobbered: %q", firstLine(string(disk)))
	}
	if code, _, _ := exec("render", "--force"); code != 0 {
		t.Fatalf("--force exit = %d", code)
	}
}

func TestRenderStdout(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# H\n\n{{> ccx}}\n")
	code, out, _ := exec("render", "--stdout")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat("AGENTS.md"); err == nil {
		t.Fatal("--stdout must not write a file")
	}
	if !strings.HasPrefix(out, "<!-- cc-guides dev") {
		t.Fatalf("stdout = %q", firstLine(out))
	}
}

func TestInitDriftExit1(t *testing.T) {
	repo(t)
	// A stamped block whose body does not match the embedded fragment.
	write(t, "AGENTS.md", "# R\n\n<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/ccx.md@pending -->\ntotally wrong body\n")
	code, out, _ := exec("init", "AGENTS.md")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "MISMATCH\tccx") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestInitAlreadyBanneredExit2(t *testing.T) {
	repo(t)
	write(t, "AGENTS.md", "<!-- cc-guides 1.0 src=AGENTS.src.md | GENERATED -->\n# R\n")
	code, _, _ := exec("init", "AGENTS.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestInitNoArgExit2(t *testing.T) {
	repo(t)
	if code, _, _ := exec("init"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestListEmbedded(t *testing.T) {
	repo(t)
	code, out, _ := exec("list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{
		"ccx\tmd\tembedded",
		"install-binary-pinned\tsh\tembedded",
		"install-binary-latest\tsh\tembedded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q\n%s", want, out)
		}
	}
}

func TestCatEmbeddedAndOverride(t *testing.T) {
	dir := repo(t)
	code, out, _ := exec("cat", "ccx")
	if code != 0 || !strings.HasPrefix(out, "## Compact Context (ccx)") {
		t.Fatalf("cat ccx: code=%d out=%q", code, firstLine(out))
	}

	// Local override wins by default; --embedded ignores it.
	write(t, filepath.Join(dir, ".claude", "fragments", "ccx.md"), "## Local ccx\n")
	_, out, _ = exec("cat", "ccx")
	if !strings.HasPrefix(out, "## Local ccx") {
		t.Fatalf("override should win: %q", firstLine(out))
	}
	_, out, _ = exec("cat", "ccx", "--embedded")
	if !strings.HasPrefix(out, "## Compact Context") {
		t.Fatalf("--embedded should ignore override: %q", firstLine(out))
	}
}

func TestCatUnknownExit2(t *testing.T) {
	repo(t)
	if code, _, _ := exec("cat", "nonesuch"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestUnknownCommandExit2(t *testing.T) {
	repo(t)
	if code, _, _ := exec("frobnicate"); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRenderShellFragmentE2E(t *testing.T) {
	repo(t)
	write(t, "install-binary.src.sh", "{{> install-binary-pinned binary=foo repo=o/r brew=o/tap/foo plugin=fooplug}}\n")
	code, _, _ := exec("render")
	if code != 0 {
		t.Fatalf("render exit = %d", code)
	}
	b, err := os.ReadFile("install-binary.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	lines := strings.Split(s, "\n")
	if lines[0] != "#!/bin/sh" {
		t.Fatalf("line 1 = %q, want shebang", lines[0])
	}
	if !strings.HasPrefix(lines[1], "# cc-guides dev src=install-binary.src.sh | GENERATED") {
		t.Fatalf("line 2 = %q, want banner", lines[1])
	}
	for _, want := range []string{`NAME="foo"`, `REPO="o/r"`, `BREW_PKG="o/tap/foo"`, "fooplug plugin"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing substitution %q", want)
		}
	}
	// New .sh artifact is executable.
	info, _ := os.Stat("install-binary.sh")
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("new .sh artifact must be executable, mode = %v", info.Mode().Perm())
	}
	// Round-trips.
	if code, out, _ := exec("check"); code != 0 {
		t.Fatalf("check exit = %d out = %q", code, out)
	}
}

func TestRenderOverrideCRLFRejected(t *testing.T) {
	repo(t)
	write(t, "AGENTS.src.md", "# H\n\n{{> ccx}}\n")
	// A local override fragment with CRLF line endings must hard-error just like a
	// CRLF source, rather than rendering CR bytes into the artifact.
	write(t, ".claude/fragments/ccx.md", "## Local\r\nbody\r\n")
	code, _, errout := exec("render")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "CRLF") {
		t.Fatalf("stderr = %q, want CRLF error", errout)
	}
	if _, err := os.Stat("AGENTS.md"); err == nil {
		t.Fatal("nothing must be written when an override is CRLF")
	}
}

func TestRenderSourceShapedTargetRejected(t *testing.T) {
	repo(t)
	// x.src.src.md renders onto x.src.md, which is itself source-shaped.
	write(t, "x.src.src.md", "# x\n\n{{> ccx}}\n")
	code, _, errout := exec("render")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "source file") {
		t.Fatalf("stderr = %q", errout)
	}
	// Preflight fails before any write, so the source-shaped target is not created.
	if _, err := os.Stat("x.src.md"); err == nil {
		t.Fatal("nothing must be written on preflight failure")
	}
}

func TestRenderDuplicateTargetArgsRejected(t *testing.T) {
	repo(t)
	write(t, "foo.src.md", "# f\n\n{{> ccx}}\n")
	// Two spellings of the same source resolve to one target.
	code, _, errout := exec("render", "foo.src.md", "./foo.src.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errout, "shared by") {
		t.Fatalf("stderr = %q", errout)
	}
	if _, err := os.Stat("foo.md"); err == nil {
		t.Fatal("nothing must be written on preflight failure")
	}
}

func TestCheckSourceShapedTargetInvalid(t *testing.T) {
	repo(t)
	write(t, "x.src.src.md", "# x\n\n{{> ccx}}\n")
	code, out, errout := exec("check")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if out != "" {
		t.Fatalf("a colliding source must be reported invalid, not compared; stdout = %q", out)
	}
	if !strings.Contains(errout, "source file") {
		t.Fatalf("stderr = %q", errout)
	}
}

func TestRenderMissingTokensDeterministic(t *testing.T) {
	repo(t)
	write(t, "install-binary.src.sh", "{{> install-binary-pinned}}\n")
	code1, _, err1 := exec("render")
	code2, _, err2 := exec("render")
	if code1 != 2 || code2 != 2 {
		t.Fatalf("exit = %d/%d, want 2/2", code1, code2)
	}
	// Every missing token, sorted, must appear in one deterministic message.
	if !strings.Contains(err1, "{{binary}}, {{brew}}, {{plugin}}, {{repo}}") {
		t.Fatalf("stderr must name all four missing tokens in sorted order: %q", err1)
	}
	if err1 != err2 {
		t.Fatalf("missing-token error is nondeterministic:\n1: %q\n2: %q", err1, err2)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
