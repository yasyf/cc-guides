// Package source resolves shared-fragment imports: it parses a
// `github:<owner>/<repo>//<path>[@<ref>]` spec, resolves a ref to an immutable
// commit sha (shelling out to `git ls-remote`), fetches that commit's tree as a
// codeload tarball, and caches the extracted subpath under the user cache dir.
// Every fragment in one process pins the same sha per alias, and tests drive it
// entirely through a fixture Fetcher so they never touch the network.
package source

import (
	"fmt"
	"regexp"
	"strings"
)

// fullShaRe matches a full 40-char hex commit sha. Only a full sha is used
// verbatim (no ls-remote); a shorter hex ref is resolved like any other named ref,
// so a branch or tag literally named e.g. `deadbeef` still resolves.
var fullShaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// hexRefRe matches a 7-to-40-char hex ref — the shape of a (possibly abbreviated)
// commit sha. It gates the cache-path safety check on a caller-supplied pin, and
// the "did you mean the full sha" hint when an abbreviated ref fails ls-remote.
var hexRefRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// ownerRepoRe restricts an owner or repo segment to GitHub's own charset, with a
// leading alphanumeric. It excludes `/`, `\`, and a leading `.`, so a segment can
// never be `.`, `..`, or otherwise traverse when joined into the on-disk cache
// path.
var ownerRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Spec is a parsed github source spec, either explicit-path
// (github:<owner>/<repo>//<path>[@<ref>]) or manifest form
// (github:<owner>/<repo>[@<ref>], no `//`, Manifest true — the resolver follows
// the target repo's cc-guides.toml). Ref is "" for the default branch; Path is ""
// for the repo root or a manifest spec.
type Spec struct {
	Owner    string
	Repo     string
	Path     string
	Ref      string
	Manifest bool
	Raw      string
}

// ParseSpec parses a github source spec in either the explicit-path or manifest
// form. Only the github: scheme is supported.
func ParseSpec(spec string) (Spec, error) {
	rest, ok := strings.CutPrefix(spec, "github:")
	if !ok {
		return Spec{}, fmt.Errorf("%w: %q (want github:<owner>/<repo>[@<ref>] or github:<owner>/<repo>//<path>[@<ref>])", ErrBadSpec, spec)
	}
	if ownerRepo, pathRef, ok := strings.Cut(rest, "//"); ok {
		// Explicit-path form: github:<owner>/<repo>//<path>[@<ref>].
		owner, repo, ok := strings.Cut(ownerRepo, "/")
		if !ok || !ownerRepoRe.MatchString(owner) || !ownerRepoRe.MatchString(repo) {
			return Spec{}, fmt.Errorf("%w: %q has a malformed or unsafe <owner>/<repo>", ErrBadSpec, spec)
		}
		s := Spec{Owner: owner, Repo: repo, Raw: spec}
		// The ref (if any) follows the last '@'; owner/repo/path never contain '@'.
		if at := strings.LastIndexByte(pathRef, '@'); at >= 0 {
			s.Path = pathRef[:at]
			s.Ref = pathRef[at+1:]
			if s.Ref == "" {
				return Spec{}, fmt.Errorf("%w: %q has an empty ref after `@`", ErrBadSpec, spec)
			}
		} else {
			s.Path = pathRef
		}
		s.Path = strings.Trim(s.Path, "/")
		if strings.Contains(s.Path, "..") {
			return Spec{}, fmt.Errorf("%w: %q path may not contain `..`", ErrBadSpec, spec)
		}
		return s, nil
	}
	// Manifest form: github:<owner>/<repo>[@<ref>], no `//path`. The ref (if any)
	// follows the FIRST '@' (owner/repo never contain '@'), so a branch literally
	// named e.g. `release@2026` is kept whole as the ref.
	ownerRepo := rest
	var ref string
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		ownerRepo = rest[:at]
		ref = rest[at+1:]
		if ref == "" {
			return Spec{}, fmt.Errorf("%w: %q has an empty ref after `@`", ErrBadSpec, spec)
		}
	}
	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok || !ownerRepoRe.MatchString(owner) || !ownerRepoRe.MatchString(repo) {
		return Spec{}, fmt.Errorf("%w: %q has a malformed or unsafe <owner>/<repo>", ErrBadSpec, spec)
	}
	return Spec{Owner: owner, Repo: repo, Ref: ref, Manifest: true, Raw: spec}, nil
}

// verbatimSha reports whether the spec's ref is a hex sha usable without a
// network round-trip, returning it when so.
func (s Spec) verbatimSha() (string, bool) {
	if s.Ref != "" && fullShaRe.MatchString(s.Ref) {
		return s.Ref, true
	}
	return "", false
}
