// Package lockfile reads and writes .claude/fragments/cc-guides.lock — the
// repo-level provenance record that moves version and commit pins out of artifact
// banners. render writes it; check and the CI action read it to pin every alias
// to the exact commit the artifacts were composed against.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Path is the repo-relative location of the lock file.
const Path = ".claude/fragments/cc-guides.lock"

// commitRe matches a full 40-char hex commit sha; a source pin's commit is either
// this or the literal "local" (a dev render off a local directory).
var commitRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validCommit(c string) bool { return c == "local" || commitRe.MatchString(c) }

const header = "# Written by 'cc-guides render' — do not edit.\n"

// SourcePin records the resolved commit for one alias plus the spec it resolved.
type SourcePin struct {
	Spec   string
	Commit string
}

// Lock is a parsed cc-guides.lock.
type Lock struct {
	Schema    int
	Version   string
	Artifacts []string
	Sources   map[string]SourcePin // alias -> pin
}

type rawLock struct {
	Schema    int                     `toml:"schema"`
	Version   string                  `toml:"version"`
	Artifacts []string                `toml:"artifacts"`
	Sources   map[string]rawSourcePin `toml:"sources"`
}

type rawSourcePin struct {
	Spec   string `toml:"spec"`
	Commit string `toml:"commit"`
}

// Parse decodes cc-guides.lock bytes.
func Parse(data []byte) (*Lock, error) {
	var raw rawLock
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("cc-guides.lock: %w", err)
	}
	lk := &Lock{Schema: raw.Schema, Version: raw.Version, Artifacts: raw.Artifacts, Sources: map[string]SourcePin{}}
	for alias, sp := range raw.Sources {
		if !validCommit(sp.Commit) {
			return nil, fmt.Errorf("cc-guides.lock: [sources.%s] commit %q must be a 40-char sha or \"local\"", alias, sp.Commit)
		}
		lk.Sources[alias] = SourcePin(sp)
	}
	return lk, nil
}

// Load reads and parses the lock under root. present is false (nil, false, nil)
// when the file does not exist.
func Load(root string) (lk *Lock, present bool, err error) {
	p := filepath.Join(root, filepath.FromSlash(Path))
	data, err := os.ReadFile(p) // #nosec G304 -- reads the repo's own lock file
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	lk, err = Parse(data)
	if err != nil {
		return nil, true, err
	}
	return lk, true, nil
}

// HasArtifact reports whether target is a managed artifact recorded in the lock.
func (l *Lock) HasArtifact(target string) bool {
	for _, a := range l.Artifacts {
		if a == target {
			return true
		}
	}
	return false
}

// Encode renders the lock to canonical, deterministic bytes: sorted artifacts,
// alias-sorted source tables, stable field order, a leading do-not-edit comment,
// and a single trailing newline.
func (l *Lock) Encode() []byte {
	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, "schema = %d\n", l.Schema)
	b.WriteString("version = " + quote(l.Version) + "\n")

	arts := append([]string(nil), l.Artifacts...)
	sort.Strings(arts)
	b.WriteString("artifacts = [")
	for i, a := range arts {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quote(a))
	}
	b.WriteString("]\n")

	aliases := make([]string, 0, len(l.Sources))
	for a := range l.Sources {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)
	for _, a := range aliases {
		sp := l.Sources[a]
		b.WriteString("\n[sources." + a + "]\n")
		b.WriteString("spec = " + quote(sp.Spec) + "\n")
		b.WriteString("commit = " + quote(sp.Commit) + "\n")
	}
	return []byte(b.String())
}

// Merge folds a freshly-rendered lock into an existing one for a partial render:
// the fresh version wins, artifacts union, sources touched this run overwrite,
// and untouched aliases are preserved. A nil existing lock returns fresh.
func Merge(existing, fresh *Lock) *Lock {
	if existing == nil {
		return fresh
	}
	out := &Lock{Schema: fresh.Schema, Version: fresh.Version, Sources: map[string]SourcePin{}}
	set := map[string]bool{}
	for _, a := range existing.Artifacts {
		set[a] = true
	}
	for _, a := range fresh.Artifacts {
		set[a] = true
	}
	for a := range set {
		out.Artifacts = append(out.Artifacts, a)
	}
	sort.Strings(out.Artifacts)
	for a, sp := range existing.Sources {
		out.Sources[a] = sp
	}
	for a, sp := range fresh.Sources {
		out.Sources[a] = sp
	}
	return out
}

// quote wraps a value in a TOML basic string, escaping the two characters that
// could break it. Lock values are specs, shas, versions, and slash paths.
func quote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return "\"" + s + "\""
}
