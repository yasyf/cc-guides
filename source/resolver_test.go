package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
}

func (f *fixtureFetcher) LsRemote(_ context.Context, _, _, _ string) (string, error) {
	f.lsCalls++
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
	// call — it must still resolve, pinned to the same sha, fully offline.
	offline := &fixtureFetcher{sha: fixtureSha, failTar: true}
	r2, _ := New(Options{Specs: specs, Pinned: map[string]string{"cc-skills": "abcdef012345"}, Fetcher: offline, CacheRoot: cache})
	if _, found, err := r2.Resolve(context.Background(), "cc-skills", "ccx", guide.KindMD); err != nil || !found {
		t.Fatalf("warm-cache offline resolve failed: found=%v err=%v", found, err)
	}
	if offline.tarCalls != 0 {
		t.Fatalf("warm cache must not fetch: tarCalls=%d", offline.tarCalls)
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
