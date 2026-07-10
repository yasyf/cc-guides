package guide

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Kind is the comment style / file family of an artifact or fragment. The set is
// deliberately small and closed; an unknown extension is a loud error, never a
// silent passthrough.
type Kind int

const (
	// KindMD is a Markdown artifact (`<!-- … -->` comments).
	KindMD Kind = iota
	// KindSH is a POSIX shell artifact (`# …` comments).
	KindSH
	// KindJSON is a JSON artifact (deep-merged, no comment marker).
	KindJSON
)

// AllKinds enumerates every supported kind; used to probe the alternate kind when
// building a kind-mismatch diagnostic.
var AllKinds = []Kind{KindMD, KindSH, KindJSON}

// String returns the short name used in TSV output, the kind subdir, and diagnostics.
func (k Kind) String() string {
	switch k {
	case KindMD:
		return "md"
	case KindSH:
		return "sh"
	case KindJSON:
		return "json"
	default:
		return "unknown"
	}
}

// Ext returns the file extension (with leading dot) for the kind.
func (k Kind) Ext() string {
	switch k {
	case KindMD:
		return ".md"
	case KindSH:
		return ".sh"
	case KindJSON:
		return ".json"
	default:
		return ""
	}
}

// KindFromExt maps a file extension (with leading dot) to its Kind.
func KindFromExt(ext string) (Kind, error) {
	switch strings.ToLower(ext) {
	case ".md":
		return KindMD, nil
	case ".sh":
		return KindSH, nil
	case ".json":
		return KindJSON, nil
	default:
		return 0, fmt.Errorf("%w: %q (supported: .md, .sh, .json)", ErrUnknownExt, ext)
	}
}

// KindForPath derives a Kind from a path's extension.
func KindForPath(path string) (Kind, error) {
	return KindFromExt(filepath.Ext(path))
}
