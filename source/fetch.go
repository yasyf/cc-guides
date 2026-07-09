package source

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// lsRemoteTimeout bounds the git ls-remote round-trip.
const lsRemoteTimeout = 30 * time.Second

// Fetcher is the network surface: resolve a ref to a sha, and stream a commit's
// tree as a gzip'd tarball. The real implementation shells out to git and hits
// codeload; tests substitute a fixture so nothing touches the network.
type Fetcher interface {
	LsRemote(ctx context.Context, owner, repo, ref string) (sha string, err error)
	Tarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error)
}

// netFetcher is the production Fetcher.
type netFetcher struct{}

// LsRemote resolves ref to a commit sha via `git ls-remote`, preferring a branch
// over a tag, and HEAD for the default branch. Shelling out (rather than
// reimplementing smart-HTTP) inherits the user's credential helpers and proxies;
// GIT_TERMINAL_PROMPT=0 keeps it from blocking on a credential prompt.
func (netFetcher) LsRemote(ctx context.Context, owner, repo, ref string) (string, error) {
	url := "https://github.com/" + owner + "/" + repo + ".git"
	ctx, cancel := context.WithTimeout(ctx, lsRemoteTimeout)
	defer cancel()

	args := []string{"ls-remote", url}
	if ref == "" {
		args = append(args, "HEAD")
	} else {
		args = append(args, "refs/heads/"+ref, "refs/tags/"+ref, "refs/tags/"+ref+"^{}", ref)
	}
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- fixed git subcommand; url/ref come from a validated github: spec, not a shell
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w %s@%s: %w", ErrResolveRef, url, refOrDefault(ref), err)
	}
	refs := parseLsRemote(out)

	var order []string
	if ref == "" {
		order = []string{"HEAD"}
	} else {
		// A branch wins over a tag; an annotated tag's dereferenced commit (^{})
		// wins over the tag object itself.
		order = []string{"refs/heads/" + ref, "refs/tags/" + ref + "^{}", "refs/tags/" + ref, ref}
	}
	for _, key := range order {
		if sha, ok := refs[key]; ok {
			return sha, nil
		}
	}
	return "", fmt.Errorf("%w %s@%s: no matching ref", ErrResolveRef, url, refOrDefault(ref))
}

// Tarball GETs the codeload tar.gz for a sha, adding a Bearer token only when
// GITHUB_TOKEN is set (public repos need none).
func (netFetcher) Tarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error) {
	url := "https://codeload.github.com/" + owner + "/" + repo + "/tar.gz/" + sha
	req, err := newHTTPRequest(ctx, url)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %w", ErrFetch, url, err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: GET %s -> %s", ErrFetch, url, resp.Status)
	}
	return resp.Body, nil
}

func refOrDefault(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return ref
}

// parseLsRemote maps each `<sha>\t<refname>` line to refname -> sha.
func parseLsRemote(out []byte) map[string]string {
	refs := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || sha == "" {
			continue
		}
		refs[strings.TrimSpace(name)] = sha
	}
	return refs
}

// extractSubpath extracts only the entries under subpath from a codeload tarball
// into dest. codeload wraps everything in a single `<repo>-<sha>/` top component,
// which is stripped; a subpath of "" extracts the whole tree. Path traversal
// (absolute paths, `..`) is rejected; non-regular entries (symlinks, devices) are
// skipped so a hostile archive cannot escape dest.
func extractSubpath(r io.Reader, subpath, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("%w: gzip: %w", ErrFetch, err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	prefix := ""
	if subpath != "" {
		prefix = subpath + "/"
	}
	wrote := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: tar: %w", ErrFetch, err)
		}
		rel := stripFirstComponent(hdr.Name)
		if rel == "" {
			continue
		}
		var within string
		switch {
		case subpath == "":
			within = rel
		case rel == subpath:
			within = ""
		case strings.HasPrefix(rel, prefix):
			within = strings.TrimPrefix(rel, prefix)
		default:
			continue
		}
		clean := path.Clean(within)
		if within != "" && (clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean)) {
			return fmt.Errorf("%w: unsafe tar path %q", ErrFetch, hdr.Name)
		}
		target := dest
		if within != "" {
			target = filepath.Join(dest, filepath.FromSlash(clean))
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fmt.Errorf("%w: %w", ErrFetch, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return fmt.Errorf("%w: %w", ErrFetch, err)
			}
			if err := writeFile(target, tr, fileMode(hdr.Mode)); err != nil {
				return err
			}
			wrote = true
		default:
			// Skip symlinks, hardlinks, devices, fifos.
		}
	}
	if !wrote {
		return fmt.Errorf("%w: subpath %q not found in tarball", ErrFetch, subpath)
	}
	return nil
}

// stripFirstComponent removes codeload's `<repo>-<sha>/` wrapper directory.
func stripFirstComponent(name string) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if i := strings.IndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// fileMode keeps only the executable bit off the archive mode; everything is
// written world-readable (0644) or executable (0755), never wider.
func fileMode(mode int64) os.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func writeFile(target string, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) // #nosec G304 -- target is under the process cache dir, path-sanitized above
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFetch, err)
	}
	if _, err := io.Copy(f, r); err != nil { // #nosec G110 -- fragment tarballs are small, trusted content from codeload
		_ = f.Close()
		return fmt.Errorf("%w: %w", ErrFetch, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w: %w", ErrFetch, err)
	}
	return nil
}
