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

// hexRefRe matches a 7-to-40-char hex ref used verbatim (no ls-remote) — an
// abbreviated or full commit sha, which keeps `check` offline on a warm cache.
var hexRefRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// ownerRepoRe restricts an owner or repo segment to GitHub's own charset, with a
// leading alphanumeric. It excludes `/`, `\`, and a leading `.`, so a segment can
// never be `.`, `..`, or otherwise traverse when joined into the on-disk cache
// path.
var ownerRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Spec is a parsed `github:<owner>/<repo>//<path>[@<ref>]` source spec. Ref is ""
// for the default branch; Path is "" for the repo root.
type Spec struct {
	Owner string
	Repo  string
	Path  string
	Ref   string
	Raw   string
}

// ParseSpec parses a github source spec. Only the github: scheme is supported.
func ParseSpec(spec string) (Spec, error) {
	rest, ok := strings.CutPrefix(spec, "github:")
	if !ok {
		return Spec{}, fmt.Errorf("%w: %q (want github:<owner>/<repo>//<path>[@<ref>])", ErrBadSpec, spec)
	}
	ownerRepo, pathRef, ok := strings.Cut(rest, "//")
	if !ok {
		return Spec{}, fmt.Errorf("%w: %q is missing the `//` before the path", ErrBadSpec, spec)
	}
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

// verbatimSha reports whether the spec's ref is a hex sha usable without a
// network round-trip, returning it when so.
func (s Spec) verbatimSha() (string, bool) {
	if s.Ref != "" && hexRefRe.MatchString(s.Ref) {
		return s.Ref, true
	}
	return "", false
}
