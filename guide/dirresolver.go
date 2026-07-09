package guide

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirResolver resolves overrides from a directory: <dir>/<name>.<ext>. A hit is
// marked Local so the renderer wraps it in `local:` provenance markers.
type DirResolver struct {
	dir   string // filesystem directory to read from
	label string // path prefix shown in local: markers and list origins
}

// NewDirResolver builds a DirResolver reading from dir. label is the path prefix
// shown in `local:` markers and `list` origins (e.g. ".claude/fragments").
func NewDirResolver(dir, label string) *DirResolver {
	return &DirResolver{dir: dir, label: label}
}

// Resolve reads <dir>/<name><ext>; a missing file is a clean miss, not an error.
// CRLF in an override body is a hard error, just as it is for sources — a
// generated artifact must never carry CR bytes.
func (d *DirResolver) Resolve(name string, kind Kind) (Fragment, bool, error) {
	fname := name + kind.Ext()
	path := filepath.Join(d.dir, fname)
	// A v3 artifact dir (e.g. ".claude/fragments/AGENTS.md/") can share a name with
	// a would-be flat override file; a directory is a clean miss, not a read error.
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return Fragment{}, false, nil
	}
	body, err := os.ReadFile(path) // #nosec G304 -- reads an override fragment from the configured dir
	if err != nil {
		if os.IsNotExist(err) {
			return Fragment{}, false, nil
		}
		return Fragment{}, false, err
	}
	origin := filepath.ToSlash(filepath.Join(d.label, fname))
	if bytes.IndexByte(body, '\r') >= 0 {
		return Fragment{}, false, fmt.Errorf("%w: %s", ErrCRLF, origin)
	}
	return Fragment{
		Name:   name,
		Kind:   kind,
		Body:   body,
		Origin: origin,
		Local:  true,
	}, true, nil
}

// Entries lists the override fragments present in the directory.
func (d *DirResolver) Entries() []Entry {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		kind, err := KindFromExt(ext)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		if !nameRe.MatchString(name) {
			continue
		}
		out = append(out, Entry{
			Name:   name,
			Kind:   kind,
			Origin: filepath.ToSlash(filepath.Join(d.label, e.Name())),
			Local:  true,
		})
	}
	return out
}
