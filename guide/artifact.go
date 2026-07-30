package guide

import (
	"fmt"
	"path"
	"strings"
)

// FragmentsRoot is the repo-relative directory under which every v3 artifact dir
// lives. A dir under it that holds a layout.toml is an artifact dir, and its
// relpath below this root IS the target artifact path unless that layout.toml
// names another with a `target` key.
const FragmentsRoot = ".claude/fragments"

// TargetForLayoutDir maps a layout directory (repo-relative, slash-separated,
// e.g. ".claude/fragments/plugin/scripts/install-binary.sh") to the artifact it
// renders (e.g. "plugin/scripts/install-binary.sh") and that artifact's kind. A
// non-empty override — the layout's `target` key — replaces the path-derived target,
// so a dir named `gitignore/` renders `.gitignore` rather than forcing the repo to
// carry a directory that shadows the file. It enforces the discovery guards on
// whichever target wins: the dir must sit under FragmentsRoot, the target must name
// a file inside the repo (neither absolute, nor an escape via "..", nor a bare "."
// or ".."), must hold no control character or backslash, must carry a supported
// extension, and must not land back under FragmentsRoot (a doubly-nested fragments
// tree).
func TargetForLayoutDir(dir, override string) (target string, kind Kind, err error) {
	clean := path.Clean(dir)
	rel := strings.TrimPrefix(clean, FragmentsRoot+"/")
	if rel == clean || rel == "" {
		return "", 0, fmt.Errorf("layout dir %q is not under %s/", dir, FragmentsRoot)
	}
	if override != "" {
		rel = override
	}
	if err := targetCharset(rel); err != nil {
		return "", 0, fmt.Errorf("layout dir %q has an unsafe target %q: %w", dir, rel, err)
	}
	if unsafeTarget(rel) {
		return "", 0, fmt.Errorf("layout dir %q has an unsafe target %q", dir, rel)
	}
	kind, err = KindForPath(rel)
	if err != nil {
		return "", 0, fmt.Errorf("layout dir %q: target %q must end in %s: %w", dir, rel, SupportedExtensions(), err)
	}
	if rel == FragmentsRoot || strings.HasPrefix(rel, FragmentsRoot+"/") {
		return "", 0, fmt.Errorf("layout dir %q: target %q must not land back under %s", dir, rel, FragmentsRoot)
	}
	return rel, kind, nil
}

// unsafeTarget reports whether rel names anything but a file inside the repo. The
// Clean comparison catches every traversal Clean can normalize away, leaving the
// forms that survive it — an absolute path, a leading "..", and the bare "." and
// ".." — to be rejected by name. Those last two would otherwise reach the extension
// check and be turned back by an accident of filepath.Ext rather than by a guard.
func unsafeTarget(rel string) bool {
	return rel != path.Clean(rel) ||
		path.IsAbs(rel) ||
		rel == "." || rel == ".." ||
		strings.HasPrefix(rel, "../")
}

// targetCharset rejects the characters an artifact path may not hold. A control
// character reaching the lock writes TOML that cannot be parsed back, bricking every
// later render and check; a backslash is not a separator on any platform cc-guides
// ships for, so a target carrying one is a Windows path someone wrote by mistake.
func targetCharset(rel string) error {
	for _, r := range rel {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character %q", ErrUnsafeTargetChar, r)
		}
		if r == '\\' {
			return fmt.Errorf("%w: backslash (targets are slash-separated)", ErrUnsafeTargetChar)
		}
	}
	return nil
}
