package guide

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// FragmentsRoot is the repo-relative directory under which every v3 artifact dir
// lives. A dir under it that holds a layout.toml is an artifact dir, and its
// relpath below this root IS the target artifact path.
const FragmentsRoot = ".claude/fragments"

// TargetForLayoutDir maps a layout directory (repo-relative, slash-separated,
// e.g. ".claude/fragments/plugin/scripts/install-binary.sh") to the artifact it
// renders (e.g. "plugin/scripts/install-binary.sh") and that artifact's kind. It
// enforces the discovery guards: the dir must sit under FragmentsRoot, the target
// may not escape via "..", must carry a supported extension, and must not land
// back under FragmentsRoot (a doubly-nested fragments tree).
func TargetForLayoutDir(dir string) (target string, kind Kind, err error) {
	clean := path.Clean(dir)
	rel := strings.TrimPrefix(clean, FragmentsRoot+"/")
	if rel == clean || rel == "" {
		return "", 0, fmt.Errorf("layout dir %q is not under %s/", dir, FragmentsRoot)
	}
	if rel != path.Clean(rel) || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", 0, fmt.Errorf("layout dir %q has an unsafe target %q", dir, rel)
	}
	kind, err = KindForPath(rel)
	if err != nil {
		return "", 0, fmt.Errorf("layout dir %q: target %q must end in .md or .sh: %w", dir, rel, err)
	}
	if rel == FragmentsRoot || strings.HasPrefix(rel, FragmentsRoot+"/") {
		return "", 0, fmt.Errorf("layout dir %q: target %q must not land back under %s", dir, rel, FragmentsRoot)
	}
	return rel, kind, nil
}

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
