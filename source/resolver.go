package source

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/yasyf/cc-guides/guide"
)

// LocalPin is the sentinel pin recorded for an alias resolved from a local
// directory (a `--source alias=<dir>` override or a test fixture) rather than a
// pinned commit — it renders `fragments=local`, which the action refuses.
const LocalPin = "local"

// Importer resolves shared-fragment imports (`alias:name`) to bodies and records
// the pin — a 12-char sha or LocalPin — of every alias it resolves, for the
// banner. CLI render/check and the migrator consume this interface; tests provide
// a fixture implementation.
type Importer interface {
	// Resolve returns the body for alias:name of kind. found=false means the
	// alias resolves but has no such fragment (the caller probes the other kind to
	// tell a kind mismatch from a genuinely unknown name). A non-nil error is a
	// hard failure (bad spec, unreachable source, impure body).
	Resolve(ctx context.Context, alias, name string, kind guide.Kind) (body []byte, found bool, err error)
	// Pin returns the recorded pin for an alias once it has been resolved.
	Pin(alias string) (pin string, ok bool)
}

// Options configures a Resolver.
type Options struct {
	// Specs maps alias -> spec (layout [sources.*] merged with --source overrides).
	// A spec beginning `github:` is fetched by sha; any other value is a local
	// directory read directly (dev/E2E) and pinned as LocalPin.
	Specs map[string]string
	// Pinned maps alias -> commit sha to use verbatim (check mode, off the banner),
	// skipping ls-remote. Absent aliases resolve their ref fresh.
	Pinned map[string]string
	// Fetcher is the network surface; nil uses the production git+codeload fetcher.
	Fetcher Fetcher
	// CacheRoot overrides the on-disk cache location (tests point it at a tempdir).
	CacheRoot string
}

// Resolver resolves imports for a run, pinning each alias's sha ONCE so every
// artifact in the run composes against the same content.
type Resolver struct {
	specs     map[string]string
	pinned    map[string]string
	fetcher   Fetcher
	cacheRoot string

	mu      sync.Mutex
	sources map[string]*resolved // alias -> resolved source (memoized)
	used    map[string]string    // alias -> recorded pin
}

// resolved is a memoized per-alias source: either a pinned github tree extracted
// under dir, or a local directory read in place.
type resolved struct {
	dir string
	pin string
}

// New builds a Resolver. An empty CacheRoot defaults to
// os.UserCacheDir()/cc-guides/fragments.
func New(opts Options) (*Resolver, error) {
	root := opts.CacheRoot
	if root == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("%w: locating user cache dir: %w", ErrFetch, err)
		}
		root = filepath.Join(base, "cc-guides", "fragments")
	}
	f := opts.Fetcher
	if f == nil {
		f = netFetcher{}
	}
	return &Resolver{
		specs:     opts.Specs,
		pinned:    opts.Pinned,
		fetcher:   f,
		cacheRoot: root,
		sources:   map[string]*resolved{},
		used:      map[string]string{},
	}, nil
}

// Resolve implements Importer.
func (r *Resolver) Resolve(ctx context.Context, alias, name string, kind guide.Kind) ([]byte, bool, error) {
	src, err := r.source(ctx, alias)
	if err != nil {
		return nil, false, err
	}
	r.mu.Lock()
	r.used[alias] = src.pin
	r.mu.Unlock()

	fname := filepath.Join(src.dir, kind.String(), name+kind.Ext())
	body, err := os.ReadFile(fname) // #nosec G304 -- reads a fragment from the process cache/fixture dir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("%w: reading %s:%s: %w", ErrFetch, alias, name, err)
	}
	if err := purity(body, alias+":"+name); err != nil {
		return nil, false, err
	}
	return body, true, nil
}

// Pin implements Importer.
func (r *Resolver) Pin(alias string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pin, ok := r.used[alias]
	return pin, ok
}

// source resolves and memoizes the source for an alias (once per process).
func (r *Resolver) source(ctx context.Context, alias string) (*resolved, error) {
	r.mu.Lock()
	if s, ok := r.sources[alias]; ok {
		r.mu.Unlock()
		return s, nil
	}
	r.mu.Unlock()

	spec, ok := r.specs[alias]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAlias, alias)
	}

	var res *resolved
	if !strings.HasPrefix(spec, "github:") {
		// A local directory read in place (dev/E2E override or test fixture).
		res = &resolved{dir: spec, pin: LocalPin}
	} else {
		sp, err := ParseSpec(spec)
		if err != nil {
			return nil, err
		}
		sha, err := r.resolveSha(ctx, alias, sp)
		if err != nil {
			return nil, err
		}
		dir, err := r.ensureExtracted(ctx, sp, sha)
		if err != nil {
			return nil, err
		}
		res = &resolved{dir: dir, pin: sha12(sha)}
	}

	r.mu.Lock()
	r.sources[alias] = res
	r.mu.Unlock()
	return res, nil
}

// resolveSha picks the sha for an alias: a caller-pinned sha (check mode) or a
// verbatim hex ref, both offline; otherwise git ls-remote.
func (r *Resolver) resolveSha(ctx context.Context, alias string, sp Spec) (string, error) {
	if p, ok := r.pinned[alias]; ok && p != "" && p != "none" && p != LocalPin {
		// A pin comes off a banner string; validate it is a hex sha before it
		// becomes a cache-path segment, so a corrupted banner can't escape the cache.
		if !hexRefRe.MatchString(p) {
			return "", fmt.Errorf("%w: pinned sha %q for alias %q is not a hex commit sha", ErrResolveRef, p, alias)
		}
		return p, nil
	}
	if sha, ok := sp.verbatimSha(); ok {
		return sha, nil
	}
	return r.fetcher.LsRemote(ctx, sp.Owner, sp.Repo, sp.Ref)
}

// ensureExtracted returns the cache dir for (spec, sha), fetching and extracting
// the subpath on a cold cache. The extraction lands in a sibling tempdir and is
// renamed into place so a partial fetch never leaves a half-populated cache dir.
func (r *Resolver) ensureExtracted(ctx context.Context, sp Spec, sha string) (string, error) {
	dir := r.cacheDir(sp, sha)
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return "", fmt.Errorf("%w: %w", ErrFetch, err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFetch, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	body, err := r.fetcher.Tarball(ctx, sp.Owner, sp.Repo, sha)
	if err != nil {
		return "", fmt.Errorf("%w (source %s): %w", ErrOfflineMiss, sp.Raw, err)
	}
	defer func() { _ = body.Close() }()

	if err := extractSubpath(body, sp.Path, tmp); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dir); err != nil {
		// A concurrent process may have populated the dir first; accept it.
		if st, statErr := os.Stat(dir); statErr == nil && st.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("%w: %w", ErrFetch, err)
	}
	return dir, nil
}

// cacheDir keys the cache on (owner, repo, sha12, sanitized-subpath) under the
// user cache dir. The subpath is a cache dimension because two specs can pin the
// same commit but import different subtrees.
func (r *Resolver) cacheDir(sp Spec, sha string) string {
	return filepath.Join(r.cacheRoot, sp.Owner, sp.Repo, sha12(sha), sanitizeSubpath(sp.Path))
}

// sha12 truncates a sha to the 12-char form recorded in the banner and cache key.
func sha12(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// sanitizeSubpath turns a slash subpath into one safe path segment; "" becomes
// the sentinel "%root". The encoding is injective — the escape char is escaped
// first, then the separators — so two distinct subpaths (e.g. "a/b" and "a_b")
// never collapse to the same cache key. The empty-path sentinel is provably
// outside the escaper's image: every '%' the escaper emits is immediately
// followed by 25/2F/5C, so "%r" can never be produced — hence a literal subpath
// "%root" escapes to "%25root" and can never alias the empty-path key.
func sanitizeSubpath(p string) string {
	if p == "" {
		return "%root"
	}
	return strings.NewReplacer("%", "%25", "/", "%2F", "\\", "%5C").Replace(p)
}

// purity rejects a fragment body that carries CR bytes, is empty, or is
// whitespace-only — the load-time half of the composition purity contract. A
// whitespace-only body would survive TrimRight and inject stray blank lines.
func purity(body []byte, origin string) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("%w: %s is empty or whitespace-only", ErrFetch, origin)
	}
	if bytes.IndexByte(body, '\r') >= 0 {
		return fmt.Errorf("%w: %s", guide.ErrCRLF, origin)
	}
	return nil
}
