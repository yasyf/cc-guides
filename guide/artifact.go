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
	if err := ValidateArtifactPath(rel); err != nil {
		return "", 0, fmt.Errorf("layout dir %q has an unsafe target %q: %w", dir, rel, err)
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

// ValidateRepoPath reports why p cannot be a repo-relative path that stays inside the
// repo, or nil. It is the one path-safety rule in this module, shared by every place a
// path arrives from outside: a layout's `target` key, an artifact the lock records,
// and a manifest's `guides` dir. p may name a directory — "." passes — so a caller
// needing a file uses ValidateArtifactPath instead.
//
// The Clean comparison catches every traversal Clean can normalize away, leaving the
// forms that survive it — an absolute path and a leading ".." — to be rejected by
// name.
//
// The character rule is deliberately not a completeness check, and nothing should be
// built on it as one. A bare double quote, a C1 control, and a bidi override all pass
// and reach the lock; what keeps the lock parseable is tomlstr.Quote, which escapes
// every value unconditionally and is verified over every rune. What this rejects is
// what has no place in a real filename: a C0 control or DEL, and a backslash, which is
// not a separator on any platform cc-guides ships for and so marks a Windows path
// someone wrote by mistake.
func ValidateRepoPath(p string) error {
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control character %q", ErrUnsafePathChar, r)
		}
		if r == '\\' {
			return fmt.Errorf("%w: backslash (paths are slash-separated)", ErrUnsafePathChar)
		}
	}
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrUnsafePath)
	}
	if p != path.Clean(p) || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("%w: %q is not a clean path inside the repo", ErrUnsafePath, p)
	}
	return nil
}

// ValidateArtifactPath reports why p cannot be a rendered artifact's path, or nil: the
// shared repo-path rule, plus naming a file rather than a directory. A bare "." would
// otherwise reach the extension check and be turned back by an accident of
// filepath.Ext rather than by a guard.
func ValidateArtifactPath(p string) error {
	if err := ValidateRepoPath(p); err != nil {
		return err
	}
	if p == "." {
		return fmt.Errorf("%w: %q names a directory, not an artifact", ErrUnsafePath, p)
	}
	return nil
}
