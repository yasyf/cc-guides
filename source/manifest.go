package source

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/yasyf/cc-guides/guide"
)

// Manifest-file locations, checked in order: .claude/ preferred, then repo root
// (mirrors capt-hook's manifest_in).
const (
	ManifestPathPreferred = ".claude/cc-guides.toml"
	ManifestPathRoot      = "cc-guides.toml"
)

// Manifest is a parsed cc-guides.toml pack manifest — the well-known file a
// manifest-form spec resolves through to locate the pack's guides dir.
type Manifest struct {
	Name        string
	Description string
	Guides      string // repo-relative dir holding md/, sh/, json/ subdirs
}

type rawManifest struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Guides      string `toml:"guides"`
}

// ParseManifest decodes and validates cc-guides.toml bytes, rejecting unknown
// keys, a CRLF body, an invalid name, or a guides path that is empty, absolute,
// or escapes via `..`.
func ParseManifest(data []byte) (*Manifest, error) {
	if bytes.IndexByte(data, '\r') >= 0 {
		return nil, guide.ErrCRLF
	}
	var raw rawManifest
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("%w: unknown key(s): %s", ErrBadManifest, undecodedList(und))
	}
	if !guide.ValidName(raw.Name) {
		return nil, fmt.Errorf("%w: name %q must match ^[a-z0-9][a-z0-9-]*$", ErrBadManifest, raw.Name)
	}
	g := raw.Guides
	if g == "" {
		return nil, fmt.Errorf("%w: `guides` is required", ErrBadManifest)
	}
	if path.IsAbs(g) || g != path.Clean(g) || g == ".." || strings.HasPrefix(g, "../") || strings.Contains(g, "/../") {
		return nil, fmt.Errorf("%w: `guides` %q must be a clean relative path with no `..`", ErrBadManifest, g)
	}
	return &Manifest{Name: raw.Name, Description: raw.Description, Guides: g}, nil
}

// LoadManifestFrom locates and parses the pack manifest within an extracted tree
// dir, preferring .claude/cc-guides.toml over root cc-guides.toml. It returns
// ErrNoManifest when neither exists.
func LoadManifestFrom(treeDir string) (*Manifest, error) {
	for _, rel := range []string{ManifestPathPreferred, ManifestPathRoot} {
		p := filepath.Join(treeDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(p) // #nosec G304 -- reads a manifest from the process cache/fixture tree
		if err == nil {
			return ParseManifest(data)
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: reading %s: %w", ErrBadManifest, rel, err)
		}
	}
	return nil, ErrNoManifest
}

func undecodedList(keys []toml.Key) string {
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = k.String()
	}
	sort.Strings(s)
	return strings.Join(s, ", ")
}
