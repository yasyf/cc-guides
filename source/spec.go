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
	"unicode"
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
//
// ParseSpec is the only thing that builds one in non-test code, and downstream
// relies on that: Path reaches cache-path construction, where sanitizeSubpath
// escapes the separators but NOT a literal "..", so Path being "..-free" is
// ParseSpec's check and nothing else's. A second constructor takes on that check
// too — this is written down because the alias bug was exactly this shape, one
// parser's rule assumed to cover a second reader that never applied it.
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
			if err := validRef(s.Ref); err != nil {
				return Spec{}, fmt.Errorf("%w: %q: %w", ErrBadSpec, spec, err)
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
		if err := validRef(ref); err != nil {
			return Spec{}, fmt.Errorf("%w: %q: %w", ErrBadSpec, spec, err)
		}
	}
	owner, repo, ok := strings.Cut(ownerRepo, "/")
	if !ok || !ownerRepoRe.MatchString(owner) || !ownerRepoRe.MatchString(repo) {
		return Spec{}, fmt.Errorf("%w: %q has a malformed or unsafe <owner>/<repo>", ErrBadSpec, spec)
	}
	return Spec{Owner: owner, Repo: repo, Ref: ref, Manifest: true, Raw: spec}, nil
}

// validRef rejects a ref that could read as an option on the git command line, or
// that no legitimate ref carries. It is deliberately not a reimplementation of
// `git check-ref-format`: real refs routinely hold `/`, `.`, `-`, and `_`
// (`feature/foo`, `v1.0.0`, `release-2.1`), so a charset strict enough to feel safe
// would reject specs people actually write, which is worse than what it guards.
//
// A leading `-` is inert today — `git ls-remote` stops recognizing options after its
// first positional, so such a ref is a pattern matching nothing — but that safety is
// a property of git's parser across versions and transports, stated nowhere in this
// code. This states it here instead. It also completes the field set: ParseSpec
// validates owner, repo, and path, and a fourth field left unchecked is the one a
// later audit does not think to look at.
func validRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q may not begin with `-` (it would read as a git option)", ref)
	}
	for _, r := range ref {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("ref %q may not contain a control character (%q)", ref, r)
		}
		if unicode.IsSpace(r) {
			return fmt.Errorf("ref %q may not contain whitespace (%q)", ref, r)
		}
	}
	return nil
}

// verbatimSha reports whether the spec's ref is a hex sha usable without a
// network round-trip, returning it when so.
func (s Spec) verbatimSha() (string, bool) {
	if s.Ref != "" && fullShaRe.MatchString(s.Ref) {
		return s.Ref, true
	}
	return "", false
}
