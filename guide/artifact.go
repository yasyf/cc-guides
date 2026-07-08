package guide

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ArtifactPath maps a source path (X.src.<ext>) to its sibling artifact (X.<ext>).
func ArtifactPath(src string) (string, error) {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	if _, err := KindFromExt(ext); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(base, ext)
	if !strings.HasSuffix(stem, ".src") {
		return "", fmt.Errorf("not a source file: %q (expected X.src%s)", src, ext)
	}
	return filepath.Join(dir, strings.TrimSuffix(stem, ".src")+ext), nil
}

// SourcePath maps an artifact path (X.<ext>) to its source (X.src.<ext>).
func SourcePath(artifact string) string {
	dir := filepath.Dir(artifact)
	base := filepath.Base(artifact)
	ext := filepath.Ext(base)
	return filepath.Join(dir, strings.TrimSuffix(base, ext)+".src"+ext)
}

// IsSource reports whether path looks like a renderable source (X.src.{md,sh}).
func IsSource(path string) bool {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if _, err := KindFromExt(ext); err != nil {
		return false
	}
	return strings.HasSuffix(strings.TrimSuffix(base, ext), ".src")
}
