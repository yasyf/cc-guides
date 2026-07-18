package guide

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Kind is the comment style / file family of an artifact or fragment. The set is
// deliberately small and closed; an unknown extension is a loud error, never a
// silent passthrough. Each Kind indexes exactly one spec in the registry (spec.go),
// which is the single source of every kind-specific behavior.
type Kind int

const (
	// KindMD is a Markdown artifact (`<!-- … -->` comments).
	KindMD Kind = iota
	// KindSH is a POSIX shell artifact (`# …` comments).
	KindSH
	// KindJSON is a JSON artifact (deep-merged, no comment marker).
	KindJSON
	// KindYAML is a YAML artifact (`# …` comments, ordered text concatenation).
	KindYAML
)

// AllKinds enumerates every supported kind; used to probe the alternate kind when
// building a kind-mismatch diagnostic.
var AllKinds = []Kind{KindMD, KindSH, KindJSON, KindYAML}

// valid reports whether k indexes a registered spec.
func (k Kind) valid() bool { return int(k) >= 0 && int(k) < len(specs) }

// String returns the short name used in TSV output, the kind subdir, and diagnostics.
func (k Kind) String() string {
	if !k.valid() {
		return "unknown"
	}
	return specs[k].name
}

// Ext returns the primary file extension (with leading dot) for the kind.
func (k Kind) Ext() string {
	if !k.valid() {
		return ""
	}
	return specs[k].exts[0]
}

// KindFromExt maps a file extension (with leading dot) to its Kind.
func KindFromExt(ext string) (Kind, error) {
	if k, ok := extIndex[strings.ToLower(ext)]; ok {
		return k, nil
	}
	return 0, fmt.Errorf("%w: %q (supported: %s)", ErrUnknownExt, ext, supportedExts)
}

// KindForPath derives a Kind from a path's extension.
func KindForPath(path string) (Kind, error) {
	return KindFromExt(filepath.Ext(path))
}
