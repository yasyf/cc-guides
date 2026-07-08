package legacy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/fragments"
	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/legacy"
)

const sha40 = "db49875b4fcd1827f02cdeea691c1538a8deed2c"

func embBody(t *testing.T, name string) string {
	t.Helper()
	f, ok, err := fragments.Resolver().Resolve(name, guide.KindMD)
	if err != nil || !ok {
		t.Fatalf("embedded %s: ok=%v err=%v", name, ok, err)
	}
	return string(f.Body)
}

func stamp(name, sha string) string {
	return "<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/" + name + ".md@" + sha + " -->"
}

func endMarker(name string) string {
	return "<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/" + name + ".md -->"
}

// run writes content to <dir>/AGENTS.md and migrates it, returning the result,
// the artifact path, and any error.
func run(t *testing.T, content string, opts legacy.Options) (legacy.Result, string, error) {
	t.Helper()
	dir := t.TempDir()
	art := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(art, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if opts.Resolver == nil {
		opts.Resolver = fragments.Resolver()
	}
	if opts.Version == "" {
		opts.Version = "1.0.0"
	}
	res, err := legacy.Migrate(art, opts)
	return res, art, err
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test reads a temp-dir path it just wrote
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertRoundTrips re-renders the produced source and requires it to reproduce
// the migrated artifact on disk byte-for-byte.
func assertRoundTrips(t *testing.T, art string) {
	t.Helper()
	src := guide.SourcePath(art)
	raw, err := os.ReadFile(src) // #nosec G304 -- test reads a temp-dir path it just wrote
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	doc, err := guide.Parse(raw, guide.KindMD)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	body, err := guide.Render(doc, fragments.Resolver())
	if err != nil {
		t.Fatalf("render source: %v", err)
	}
	final := guide.AddBanner(guide.KindMD, "1.0.0", filepath.Base(src), body)
	if got := read(t, art); got != string(final) {
		t.Fatalf("re-render does not reproduce artifact\n got:\n%s\nwant:\n%s", got, final)
	}
}

func TestMigrateNoEndMarker(t *testing.T) {
	content := "# Repo\n\nIntro.\n\n" + stamp("ccx", sha40) + "\n" + embBody(t, "ccx") + "\nAfter.\n"
	res, art, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	src := read(t, guide.SourcePath(art))
	if !strings.Contains(src, "{{> ccx}}") {
		t.Fatalf("source missing directive:\n%s", src)
	}
	if strings.Contains(src, "canonical:") {
		t.Fatalf("stamp not stripped:\n%s", src)
	}
	if !hasVerified(res.Rows) {
		t.Fatalf("no VERIFIED row: %v", res.Rows)
	}
	assertRoundTrips(t, art)
}

func TestMigrateTwoFragments(t *testing.T) {
	content := "# Repo\n\n" +
		stamp("ccx", sha40) + "\n" + embBody(t, "ccx") + "\n" +
		"Middle.\n\n" +
		stamp("version-control", "pending") + "\n" + embBody(t, "version-control") + "\n" +
		"End.\n"
	_, art, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	src := read(t, guide.SourcePath(art))
	if !strings.Contains(src, "{{> ccx}}") || !strings.Contains(src, "{{> version-control}}") {
		t.Fatalf("both directives expected:\n%s", src)
	}
	assertRoundTrips(t, art)
}

func TestMigrateEnvelopedWithEndMarker(t *testing.T) {
	content := "# Repo\n\n" +
		stamp("ccx", "pending") + "\n" + embBody(t, "ccx") + endMarker("ccx") + "\n\nAfter.\n"
	_, art, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	src := read(t, guide.SourcePath(art))
	if strings.Contains(src, "canonical:") {
		t.Fatalf("stamp/end marker not stripped:\n%s", src)
	}
	if !strings.Contains(src, "{{> ccx}}") {
		t.Fatalf("directive expected:\n%s", src)
	}
	assertRoundTrips(t, art)
}

func TestMigrateTrailingWhitespaceJitter(t *testing.T) {
	jittered := addTrailingSpaces(embBody(t, "parallelize"))
	content := "# Repo\n\n" + stamp("parallelize", "pending") + "\n" + jittered + "\nAfter.\n"
	_, art, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("trailing-ws jitter should still match: %v", err)
	}
	if !strings.Contains(read(t, guide.SourcePath(art)), "{{> parallelize}}") {
		t.Fatal("directive expected")
	}
}

func TestMigrateMismatchRefusesByDefault(t *testing.T) {
	tampered := strings.Replace(embBody(t, "ccx"), "Compact Context", "Tampered Context", 1)
	content := "# Repo\n\n" + stamp("ccx", "pending") + "\n" + tampered + "\nAfter.\n"
	res, art, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	if !hasStatus(res.Rows, legacy.StatusMismatch) {
		t.Fatalf("want MISMATCH row: %v", res.Rows)
	}
	if _, statErr := os.Stat(guide.SourcePath(art)); statErr == nil {
		t.Fatal("mismatch must write nothing")
	}
}

func TestMigrateKeepMismatched(t *testing.T) {
	tampered := strings.Replace(embBody(t, "ccx"), "Compact Context", "Tampered Context", 1)
	content := "# Repo\n\n" +
		stamp("ccx", "pending") + "\n" + tampered + "\n" +
		stamp("version-control", "pending") + "\n" + embBody(t, "version-control") + "\nEnd.\n"
	res, art, err := run(t, content, legacy.Options{KeepMismatched: true})
	if err != nil {
		t.Fatalf("keep-mismatched should succeed: %v", err)
	}
	src := read(t, guide.SourcePath(art))
	if !strings.Contains(src, "{{> version-control}}") {
		t.Fatal("matched fragment should collapse")
	}
	if !strings.Contains(src, "Tampered Context") {
		t.Fatal("mismatched block should be left literal")
	}
	if !hasStatus(res.Rows, legacy.StatusMismatch) || !hasVerified(res.Rows) {
		t.Fatalf("want MISMATCH + VERIFIED rows: %v", res.Rows)
	}
	assertRoundTrips(t, art)
}

func TestMigrateKeepMismatchedShortBlockKeepsNextStamp(t *testing.T) {
	// A mismatched, end-marker-less ccx block whose actual body is SHORTER than
	// the embedded body, immediately followed by a valid parallelize stamp. The
	// short block must not swallow the following stamp into its guessed window.
	content := "# Repo\n\n" +
		stamp("ccx", "pending") + "\nOnly two\nlines here\n" +
		stamp("parallelize", "pending") + "\n" + embBody(t, "parallelize") + "End.\n"

	// With --keep-mismatched the parallelize block still collapses to its directive.
	res, art, err := run(t, content, legacy.Options{KeepMismatched: true})
	if err != nil {
		t.Fatalf("keep-mismatched should succeed: %v", err)
	}
	src := read(t, guide.SourcePath(art))
	if !strings.Contains(src, "{{> parallelize}}") {
		t.Fatalf("following stamp was swallowed; source:\n%s", src)
	}
	if strings.Contains(src, "## Parallelize Independent Work") {
		t.Fatal("parallelize body should have collapsed to a directive, not stayed literal")
	}
	if !strings.Contains(src, "Only two") {
		t.Fatal("the mismatched block should be left literal")
	}
	if !hasStatus(res.Rows, legacy.StatusMismatch) || !hasVerified(res.Rows) {
		t.Fatalf("want MISMATCH + VERIFIED rows: %v", res.Rows)
	}
	assertRoundTrips(t, art)

	// Without the flag the mismatch is drift: exit 1 semantics, nothing written.
	_, art2, err2 := run(t, content, legacy.Options{})
	if !errors.Is(err2, legacy.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err2)
	}
	if _, statErr := os.Stat(guide.SourcePath(art2)); statErr == nil {
		t.Fatal("default mismatch must write nothing")
	}
}

func TestMigrateUnknownFragment(t *testing.T) {
	content := "# Repo\n\n" + stamp("nonesuch", "pending") + "\nsome body line\n"
	res, _, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	if !hasStatus(res.Rows, legacy.StatusUnknown) {
		t.Fatalf("want UNKNOWN row: %v", res.Rows)
	}
}

func TestMigrateAlreadyBannered(t *testing.T) {
	content := "<!-- cc-guides 1.0.0 src=AGENTS.src.md | GENERATED — x -->\n# Repo\n"
	_, _, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrAlreadyBannered) {
		t.Fatalf("err = %v, want ErrAlreadyBannered", err)
	}
}

func TestMigratePreexistingSource(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "AGENTS.md")
	mustWrite(t, art, "# Repo\n\n"+stamp("ccx", "pending")+"\n"+embBody(t, "ccx")+"\n")
	mustWrite(t, guide.SourcePath(art), "preexisting\n")
	_, err := legacy.Migrate(art, legacy.Options{Version: "1.0.0", Resolver: fragments.Resolver()})
	if !errors.Is(err, legacy.ErrSourceExists) {
		t.Fatalf("err = %v, want ErrSourceExists", err)
	}
}

func TestMigrateCollisionScan(t *testing.T) {
	content := "# Repo\n{{> ccx}}\n\n" + stamp("version-control", "pending") + "\n" + embBody(t, "version-control") + "\n"
	_, _, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrCollision) {
		t.Fatalf("err = %v, want ErrCollision", err)
	}
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	content := "# Repo\n\n" + stamp("ccx", "pending") + "\n" + embBody(t, "ccx") + "\n"
	res, art, err := run(t, content, legacy.Options{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !hasVerified(res.Rows) {
		t.Fatalf("dry-run should report VERIFIED: %v", res.Rows)
	}
	if _, statErr := os.Stat(guide.SourcePath(art)); statErr == nil {
		t.Fatal("dry-run must not write the source")
	}
}

func TestMigrateNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "install.sh")
	mustWrite(t, art, "#!/bin/sh\n")
	_, err := legacy.Migrate(art, legacy.Options{Version: "1.0.0", Resolver: fragments.Resolver()})
	if !errors.Is(err, legacy.ErrNotMarkdown) {
		t.Fatalf("err = %v, want ErrNotMarkdown", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func addTrailingSpaces(body string) string {
	lines := strings.Split(body, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] += "   "
		}
	}
	return strings.Join(lines, "\n")
}

func hasVerified(rows []legacy.Row) bool { return hasStatus(rows, legacy.StatusVerified) }

func hasStatus(rows []legacy.Row, s legacy.Status) bool {
	for _, r := range rows {
		if r.Status == s {
			return true
		}
	}
	return false
}
