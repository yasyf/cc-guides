package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

const fixtureSha = "abcdef0123456789abcdef0123456789abcdef01"

// fixtureFetcher serves a canned sha and builds a codeload-shaped tar.gz from an
// in-memory file map, counting calls so tests can assert resolve-once and
// warm-cache-offline behavior.
type fixtureFetcher struct {
	sha      string
	files    map[string]string // repo-relative path -> body
	lsCalls  int
	tarCalls int
	failTar  bool
	failLs   bool
}

func (f *fixtureFetcher) LsRemote(_ context.Context, _, _, _ string) (string, error) {
	f.lsCalls++
	if f.failLs {
		return "", fmt.Errorf("%w: no matching ref", ErrResolveRef)
	}
	return f.sha, nil
}

func (f *fixtureFetcher) Tarball(_ context.Context, _, repo, sha string) (io.ReadCloser, error) {
	f.tarCalls++
	if f.failTar {
		return nil, errors.New("network down")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	top := repo + "-" + sha
	for path, body := range f.files {
		hdr := &tar.Header{Name: top + "/" + path, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(&buf), nil
}

func fixtureFiles() map[string]string {
	return map[string]string{
		"guides/md/ccx.md":     "## Compact Context\nbody\n",
		"guides/sh/install.sh": "#!/bin/sh\necho hi\n",
		"README.md":            "not a guide\n",
	}
}

func newResolver(t *testing.T, f Fetcher, specs, pinned map[string]string) *Resolver {
	t.Helper()
	r, err := New(Options{Specs: specs, Pinned: pinned, Fetcher: f, CacheRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolveGithubAndCache(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, nil)

	body, found, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD)
	if err != nil || !found {
		t.Fatalf("resolve ccx: found=%v err=%v", found, err)
	}
	if string(body) != "## Compact Context\nbody\n" {
		t.Fatalf("body = %q", body)
	}
	// A second fragment from the same alias reuses the sha (resolve-once) and the
	// warm cache (one fetch).
	if _, found, err := r.Resolve(context.Background(), "cc-skills", "install", guide.KindSH); err != nil || !found {
		t.Fatalf("resolve install: found=%v err=%v", found, err)
	}
	if f.lsCalls != 1 || f.tarCalls != 1 {
		t.Fatalf("lsCalls=%d tarCalls=%d, want 1/1 (resolve-once, warm cache)", f.lsCalls, f.tarCalls)
	}
	if pin, ok := r.Pin("cc-skills"); !ok || pin != "abcdef012345" {
		t.Fatalf("pin = %q ok=%v, want abcdef012345", pin, ok)
	}
}

func TestResolvePinnedSkipsLsRemote(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, map[string]string{"cc-skills": "abcdef012345"})
	if _, found, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if f.lsCalls != 0 {
		t.Fatalf("lsCalls = %d, want 0 (pinned sha skips ls-remote)", f.lsCalls)
	}
	if f.tarCalls != 1 {
		t.Fatalf("tarCalls = %d, want 1", f.tarCalls)
	}
	if pin, _ := r.Pin("cc-skills"); pin != "abcdef012345" {
		t.Fatalf("pin = %q", pin)
	}
}

func TestResolveWarmCacheOffline(t *testing.T) {
	cache := t.TempDir()
	specs := map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}
	// First resolver populates the cache.
	warm := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r1, _ := New(Options{Specs: specs, Fetcher: warm, CacheRoot: cache})
	if _, found, err := r1.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("prime: found=%v err=%v", found, err)
	}
	// Second resolver shares the cache and has a fetcher that fails every network
	// call — it must still resolve, pinned to an abbreviated 12-char sha, fully
	// offline (the cache keys on the full sha, so lookup matches by prefix).
	offline := &fixtureFetcher{sha: fixtureSha, failTar: true}
	r2, _ := New(Options{Specs: specs, Pinned: map[string]string{"cc-skills": "abcdef012345"}, Fetcher: offline, CacheRoot: cache})
	if _, found, err := r2.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("warm-cache offline resolve failed: found=%v err=%v", found, err)
	}
	if offline.tarCalls != 0 {
		t.Fatalf("warm cache must not fetch: tarCalls=%d", offline.tarCalls)
	}
	// A full-sha pin hits the same warm cache directly (exact key), also offline.
	offline2 := &fixtureFetcher{sha: fixtureSha, failTar: true}
	r3, _ := New(Options{Specs: specs, Pinned: map[string]string{"cc-skills": fixtureSha}, Fetcher: offline2, CacheRoot: cache})
	if _, found, err := r3.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("full-pin warm-cache offline resolve failed: found=%v err=%v", found, err)
	}
	if offline2.tarCalls != 0 {
		t.Fatalf("full-pin warm cache must not fetch: tarCalls=%d", offline2.tarCalls)
	}
}

// An abbreviated pin that prefix-matches two cached commits is a hard error, not a
// silent pick of one.
func TestCachePrefixAmbiguousIsFatal(t *testing.T) {
	base := t.TempDir()
	sp := Spec{Owner: "o", Repo: "r", Path: "guides"}
	r := newResolver(t, &fixtureFetcher{}, nil, nil)
	r.cacheRoot = base
	for _, sha := range []string{"abcdef0123450000000000000000000000000000", "abcdef0123459999999999999999999999999999"} {
		if err := os.MkdirAll(r.cacheDir(sp, sha), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.cachePrefixLookup(sp, "abcdef012345"); !errors.Is(err, ErrResolveRef) {
		t.Fatalf("ambiguous prefix must be fatal, got err=%v", err)
	}
}

func TestResolveLocalDir(t *testing.T) {
	dir := t.TempDir()
	mustWriteFixture(t, dir)
	r := newResolver(t, &fixtureFetcher{}, map[string]string{"cc-skills": dir}, nil)
	body, found, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD)
	if err != nil || !found {
		t.Fatalf("local resolve: found=%v err=%v", found, err)
	}
	if string(body) != "## Compact Context\nbody\n" {
		t.Fatalf("body = %q", body)
	}
	if pin, _ := r.Pin("cc-skills"); pin != LocalPin {
		t.Fatalf("local source pin = %q, want %q", pin, LocalPin)
	}
}

func TestResolveUnknownNameIsNotFound(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, nil)
	if _, found, err := r.Resolve(context.Background(), "cc-skills", "nonesuch", guide.KindMD); err != nil || found {
		t.Fatalf("found=%v err=%v, want not-found no-error", found, err)
	}
}

func TestResolveUnknownAlias(t *testing.T) {
	r := newResolver(t, &fixtureFetcher{}, map[string]string{}, nil)
	if _, _, err := r.Resolve(context.Background(), "nope", "x", guide.KindMD); !errors.Is(err, ErrUnknownAlias) {
		t.Fatalf("err = %v, want ErrUnknownAlias", err)
	}
}

func TestResolveCRLFImpure(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: map[string]string{"guides/md/bad.md": "a\r\nb\n"}}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, nil)
	if _, _, err := r.Resolve(context.Background(), "cc-skills", "bad", guide.KindMD); !errors.Is(err, guide.ErrCRLF) {
		t.Fatalf("err = %v, want ErrCRLF", err)
	}
}

// A 7–39-char hex ref is not a verbatim sha: it goes through ls-remote, so a
// branch or tag literally named like an abbreviated sha still resolves.
func TestAbbrevHexRefResolvesAsNamedRef(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@abcdef012345"}, nil)
	if _, found, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("abbrev-hex named ref: found=%v err=%v", found, err)
	}
	if f.lsCalls != 1 {
		t.Fatalf("an abbreviated hex ref must go through ls-remote: lsCalls=%d", f.lsCalls)
	}
}

// An abbreviated sha that fails ls-remote gets a hint to use the full 40-char sha.
func TestAbbrevHexRefFailureHintsFullSha(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, failLs: true, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@abcdef012345"}, nil)
	_, _, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD)
	if !errors.Is(err, ErrResolveRef) || !strings.Contains(err.Error(), "40-char") {
		t.Fatalf("err = %v, want ErrResolveRef with a full-sha hint", err)
	}
}

// The extraction cache keys on the full sha, so two commits that share a 12-char
// prefix never collide (the lock records full shas, the sha12 is display-only).
func TestCacheDirKeysOnFullSha(t *testing.T) {
	r := newResolver(t, &fixtureFetcher{}, nil, nil)
	sp := Spec{Owner: "o", Repo: "r", Path: "guides"}
	shaA := strings.Repeat("a", 40)
	shaB := strings.Repeat("a", 12) + strings.Repeat("b", 28)
	if r.cacheDir(sp, shaA) == r.cacheDir(sp, shaB) {
		t.Fatal("distinct full shas sharing a sha12 prefix must not collide in the cache")
	}
	if !strings.Contains(r.cacheDir(sp, shaA), shaA) {
		t.Fatalf("cache dir must key on the full sha: %q", r.cacheDir(sp, shaA))
	}
}

func TestSha12AndSanitize(t *testing.T) {
	if sha12("abcdef0123456789") != "abcdef012345" {
		t.Fatalf("sha12 = %q", sha12("abcdef0123456789"))
	}
	if sha12("short") != "short" {
		t.Fatalf("sha12 short = %q", sha12("short"))
	}
	if sanitizeSubpath("") != "%root" {
		t.Fatalf("empty subpath = %q", sanitizeSubpath(""))
	}
	// Injective: distinct subpaths must never collapse to the same cache key.
	if sanitizeSubpath("a/b") == sanitizeSubpath("a_b") {
		t.Fatalf("subpath cache-key collision: %q == %q", sanitizeSubpath("a/b"), sanitizeSubpath("a_b"))
	}
	if sanitizeSubpath("a/b") == sanitizeSubpath("a%2Fb") {
		t.Fatalf("subpath cache-key collision on the escape char")
	}
	// The empty-path sentinel must not alias any representable literal subpath —
	// "_root" (legal in a spec) and "%root" both once collided with "".
	if sanitizeSubpath("") == sanitizeSubpath("_root") {
		t.Fatalf("empty-path sentinel collides with literal %q", "_root")
	}
	if sanitizeSubpath("") == sanitizeSubpath("%root") {
		t.Fatalf("empty-path sentinel collides with literal %q", "%root")
	}
}

func TestParseSpecRejectsTraversalOwnerRepo(t *testing.T) {
	for _, spec := range []string{
		"github:../repo//guides@abcdef012345",
		"github:owner/..//guides@abcdef012345",
		"github:./x//guides",
		"github:a/b\\c//guides",
	} {
		if _, err := ParseSpec(spec); !errors.Is(err, ErrBadSpec) {
			t.Errorf("ParseSpec(%q) err = %v, want ErrBadSpec", spec, err)
		}
	}
}

func TestResolveRejectsNonHexPin(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: fixtureFiles()}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, map[string]string{"cc-skills": "../../.."})
	if _, _, err := r.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); !errors.Is(err, ErrResolveRef) {
		t.Fatalf("err = %v, want ErrResolveRef for a non-hex pin", err)
	}
	if f.tarCalls != 0 {
		t.Fatalf("a bad pin must not reach a fetch: tarCalls=%d", f.tarCalls)
	}
}

func TestResolveWhitespaceOnlyImpure(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: map[string]string{"guides/md/blank.md": " \n \n"}}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills//guides@main"}, nil)
	if _, _, err := r.Resolve(context.Background(), "cc-skills", "blank", guide.KindMD); err == nil {
		t.Fatal("a whitespace-only fragment must be rejected")
	}
}

func manifestFixtureFiles(manifestPath string) map[string]string {
	return map[string]string{
		manifestPath:            "name = \"cc-skills\"\ndescription = \"shared\"\nguides = \"plugin/guides\"\n",
		"plugin/guides/md/x.md": "## X\nx body\n",
		"plugin/guides/sh/i.sh": "#!/bin/sh\necho hi\n",
		"README.md":             "not a guide\n",
	}
}

func TestResolveManifest(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: manifestFixtureFiles(".claude/cc-guides.toml")}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills@main"}, nil)

	body, found, err := r.Resolve(context.Background(), "cc-skills", "x", guide.KindMD)
	if err != nil || !found {
		t.Fatalf("manifest resolve: found=%v err=%v", found, err)
	}
	if string(body) != "## X\nx body\n" {
		t.Fatalf("body = %q", body)
	}
	// Pin is the sha12; FullPin is the full 40-char sha for the lock.
	if pin, _ := r.Pin("cc-skills"); pin != sha12(fixtureSha) {
		t.Fatalf("Pin = %q", pin)
	}
	if full, ok := r.FullPin("cc-skills"); !ok || full != fixtureSha {
		t.Fatalf("FullPin = %q ok=%v, want %q", full, ok, fixtureSha)
	}
}

func TestResolveManifestRootFallback(t *testing.T) {
	// No .claude/cc-guides.toml — the root cc-guides.toml is used.
	f := &fixtureFetcher{sha: fixtureSha, files: manifestFixtureFiles("cc-guides.toml")}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills@main"}, nil)
	if _, found, err := r.Resolve(context.Background(), "cc-skills", "x", guide.KindMD); err != nil || !found {
		t.Fatalf("root-fallback resolve: found=%v err=%v", found, err)
	}
}

func TestResolveManifestMissing(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: map[string]string{"plugin/guides/md/x.md": "## X\nx\n"}}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills@main"}, nil)
	if _, _, err := r.Resolve(context.Background(), "cc-skills", "x", guide.KindMD); !errors.Is(err, ErrNoManifest) {
		t.Fatalf("err = %v, want ErrNoManifest", err)
	}
}

func TestResolveManifestGuidesDirMissing(t *testing.T) {
	f := &fixtureFetcher{sha: fixtureSha, files: map[string]string{
		".claude/cc-guides.toml": "name = \"cc-skills\"\nguides = \"plugin/guides\"\n",
		"README.md":              "no guides dir\n",
	}}
	r := newResolver(t, f, map[string]string{"cc-skills": "github:yasyf/cc-skills@main"}, nil)
	if _, _, err := r.Resolve(context.Background(), "cc-skills", "x", guide.KindMD); !errors.Is(err, ErrManifestGuides) {
		t.Fatalf("err = %v, want ErrManifestGuides", err)
	}
}

func mustWriteFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "md"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sh"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "md", "ccx.md"), []byte("## Compact Context\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
