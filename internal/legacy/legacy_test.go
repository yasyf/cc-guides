package legacy_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/legacy"
)

const sha40 = "db49875b4fcd1827f02cdeea691c1538a8deed2c"

// mapResolver is a minimal guide.Resolver keyed by name+kind, standing in for the
// remote cc-skills source in v3.
type mapResolver map[string]guide.Fragment

func (m mapResolver) Resolve(name string, kind guide.Kind) (guide.Fragment, bool, error) {
	f, ok := m[name+"|"+kind.String()]
	return f, ok, nil
}

func frag(name, body string) guide.Fragment {
	return guide.Fragment{Name: name, Kind: guide.KindMD, Body: []byte(body), Origin: "cc-skills:" + name}
}

func fixtureResolver() guide.Resolver {
	return mapResolver{
		"ccx|md":             frag("ccx", "## Compact Context\nccx line one\nccx line two\n"),
		"parallelize|md":     frag("parallelize", "## Parallelize\npar line one\npar line two\npar line three\n"),
		"version-control|md": frag("version-control", "## Version Control\nvc line\n"),
	}
}

func body(t *testing.T, name string) string {
	t.Helper()
	f, ok, _ := fixtureResolver().Resolve(name, guide.KindMD)
	if !ok {
		t.Fatalf("no fixture %s", name)
	}
	return strings.TrimSuffix(string(f.Body), "\n")
}

func stamp(name, sha string) string {
	return "<!-- canonical: cc-skills/plugins/repo-bootstrap/_partials/" + name + ".md@" + sha + " -->"
}

func endMarker(name string) string {
	return "<!-- /canonical: cc-skills/plugins/repo-bootstrap/_partials/" + name + ".md -->"
}

func run(t *testing.T, content string, opts legacy.Options) (legacy.Result, error) {
	t.Helper()
	dir := t.TempDir()
	art := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(art, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if opts.Resolver == nil {
		opts.Resolver = fixtureResolver()
	}
	return legacy.ToV1Source(art, opts)
}

func TestToV1SourceCollapsesStamp(t *testing.T) {
	content := "# Repo\n\nIntro.\n\n" + stamp("ccx", sha40) + "\n" + body(t, "ccx") + "\nAfter.\n"
	res, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("ToV1Source: %v", err)
	}
	src := string(res.SourceBytes)
	if !strings.Contains(src, "{{> ccx}}") {
		t.Fatalf("source missing directive:\n%s", src)
	}
	if strings.Contains(src, "canonical:") {
		t.Fatalf("stamp not stripped:\n%s", src)
	}
	if !hasVerified(res.Rows) {
		t.Fatalf("no VERIFIED row: %v", res.Rows)
	}
}

func TestToV1SourceEndMarker(t *testing.T) {
	content := "# Repo\n\n" + stamp("ccx", "pending") + "\n" + body(t, "ccx") + "\n" + endMarker("ccx") + "\n\nAfter.\n"
	res, err := run(t, content, legacy.Options{})
	if err != nil {
		t.Fatalf("ToV1Source: %v", err)
	}
	if strings.Contains(string(res.SourceBytes), "canonical:") {
		t.Fatalf("stamp/end marker not stripped:\n%s", res.SourceBytes)
	}
}

func TestToV1SourceMismatchRefuses(t *testing.T) {
	tampered := strings.Replace(body(t, "ccx"), "Compact Context", "Tampered", 1)
	content := "# Repo\n\n" + stamp("ccx", "pending") + "\n" + tampered + "\nAfter.\n"
	res, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	if !hasStatus(res.Rows, legacy.StatusMismatch) {
		t.Fatalf("want MISMATCH row: %v", res.Rows)
	}
}

// Regression: a mismatched, end-marker-less block SHORTER than the embedded body,
// followed by a valid stamp, must not swallow the following stamp into its window.
func TestKeepMismatchedShortBlockKeepsNextStamp(t *testing.T) {
	content := "# Repo\n\n" +
		stamp("ccx", "pending") + "\nOnly two\nlines here\n" +
		stamp("parallelize", "pending") + "\n" + body(t, "parallelize") + "\nEnd.\n"

	res, err := run(t, content, legacy.Options{KeepMismatched: true})
	if err != nil {
		t.Fatalf("keep-mismatched should succeed: %v", err)
	}
	src := string(res.SourceBytes)
	if !strings.Contains(src, "{{> parallelize}}") {
		t.Fatalf("following stamp was swallowed; source:\n%s", src)
	}
	if strings.Contains(src, "## Parallelize") {
		t.Fatal("parallelize body should have collapsed to a directive")
	}
	if !strings.Contains(src, "Only two") {
		t.Fatal("the mismatched block should be left literal")
	}
	if !hasStatus(res.Rows, legacy.StatusMismatch) || !hasVerified(res.Rows) {
		t.Fatalf("want MISMATCH + VERIFIED rows: %v", res.Rows)
	}
}

func TestUnknownFragment(t *testing.T) {
	content := "# Repo\n\n" + stamp("nonesuch", "pending") + "\nsome body line\n"
	res, err := run(t, content, legacy.Options{})
	if !errors.Is(err, legacy.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	if !hasStatus(res.Rows, legacy.StatusUnknown) {
		t.Fatalf("want UNKNOWN row: %v", res.Rows)
	}
}

func TestAlreadyBannered(t *testing.T) {
	content := "<!-- cc-guides 1.0.0 src=AGENTS.src.md | GENERATED — x -->\n# Repo\n"
	if _, err := run(t, content, legacy.Options{}); !errors.Is(err, legacy.ErrAlreadyBannered) {
		t.Fatalf("err = %v, want ErrAlreadyBannered", err)
	}
}

func TestCollisionScan(t *testing.T) {
	content := "# Repo\n{{> ccx}}\n\n" + stamp("version-control", "pending") + "\n" + body(t, "version-control") + "\n"
	if _, err := run(t, content, legacy.Options{}); !errors.Is(err, legacy.ErrCollision) {
		t.Fatalf("err = %v, want ErrCollision", err)
	}
}

func TestNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	art := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(art, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ToV1Source(art, legacy.Options{Resolver: fixtureResolver()}); !errors.Is(err, legacy.ErrNotMarkdown) {
		t.Fatalf("err = %v, want ErrNotMarkdown", err)
	}
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
