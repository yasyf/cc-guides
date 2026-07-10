package source

import (
	"errors"
	"testing"
)

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in       string
		owner    string
		repo     string
		path     string
		ref      string
		manifest bool
		err      bool
	}{
		{in: "github:yasyf/cc-skills//guides@main", owner: "yasyf", repo: "cc-skills", path: "guides", ref: "main"},
		{in: "github:yasyf/cc-skills//guides", owner: "yasyf", repo: "cc-skills", path: "guides"},
		{in: "github:acme/repo//a/b/c@v1.2.3", owner: "acme", repo: "repo", path: "a/b/c", ref: "v1.2.3"},
		{in: "github:o/r//@sha", owner: "o", repo: "r", path: "", ref: "sha"},
		// Manifest form: no `//`, Manifest true, Path empty.
		{in: "github:yasyf/cc-skills", owner: "yasyf", repo: "cc-skills", manifest: true},
		{in: "github:yasyf/cc-skills@main", owner: "yasyf", repo: "cc-skills", ref: "main", manifest: true},
		{in: "github:acme/repo@v1.2.3", owner: "acme", repo: "repo", ref: "v1.2.3", manifest: true},
		// Manifest form splits owner/repo from ref at the FIRST '@', so a branch
		// literally named `release@2026` is kept whole as the ref.
		{in: "github:yasyf/cc-skills@release@2026", owner: "yasyf", repo: "cc-skills", ref: "release@2026", manifest: true},
		{in: "gitlab:o/r//p", err: true},       // wrong scheme
		{in: "github:o/r/p", err: true},        // no //, extra slash in repo segment
		{in: "github:/r//p", err: true},        // empty owner
		{in: "github:o//p", err: true},         // empty repo (o// -> owner o, then // splits, repo empty)
		{in: "github:o/r//p@", err: true},      // empty ref
		{in: "github:o/r//..@main", err: true}, // path traversal
		{in: "github:o/r@", err: true},         // manifest form, empty ref
		{in: "github:/r@main", err: true},      // manifest form, empty owner
	}
	for _, tc := range cases {
		s, err := ParseSpec(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("ParseSpec(%q) = %+v, want error", tc.in, s)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSpec(%q): %v", tc.in, err)
			continue
		}
		if s.Owner != tc.owner || s.Repo != tc.repo || s.Path != tc.path || s.Ref != tc.ref || s.Manifest != tc.manifest {
			t.Errorf("ParseSpec(%q) = %+v, want owner=%q repo=%q path=%q ref=%q manifest=%v", tc.in, s, tc.owner, tc.repo, tc.path, tc.ref, tc.manifest)
		}
	}
}

func TestVerbatimSha(t *testing.T) {
	full, ok := Spec{Ref: "abcdef0123456789abcdef0123456789abcdef01"}.verbatimSha()
	if !ok || full == "" {
		t.Fatal("full 40-char sha must be verbatim")
	}
	// Only a full 40-char sha is verbatim; an abbreviated hex ref resolves through
	// ls-remote (it might be a branch/tag literally named like a short sha).
	if _, ok := (Spec{Ref: "abcdef012345"}).verbatimSha(); ok {
		t.Fatal("a 12-char (abbreviated) sha must NOT be verbatim")
	}
	if _, ok := (Spec{Ref: "abcdef0123456789abcdef0123456789abcdef0"}).verbatimSha(); ok {
		t.Fatal("a 39-char hex ref must NOT be verbatim")
	}
	if _, ok := (Spec{Ref: "main"}).verbatimSha(); ok {
		t.Fatal("a branch name must not be treated as a verbatim sha")
	}
	if _, ok := (Spec{Ref: "abc"}).verbatimSha(); ok {
		t.Fatal("a <7-char hex ref must not be verbatim")
	}
}

func TestErrorsAreWrapped(t *testing.T) {
	if _, err := ParseSpec("nope"); !errors.Is(err, ErrBadSpec) {
		t.Fatalf("err = %v, want ErrBadSpec", err)
	}
}
