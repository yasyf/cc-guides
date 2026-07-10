package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/layout"
	"github.com/yasyf/cc-guides/source"
)

// artifactDir is a loaded, validated v3 artifact dir.
type artifactDir struct {
	dir    string // repo-relative slash, e.g. ".claude/fragments/AGENTS.md"
	abs    string // absolute filesystem path
	target string // repo-relative target artifact, e.g. "AGENTS.md"
	kind   guide.Kind
	lay    *layout.Layout
}

// loadArtifactDir resolves and validates one artifact dir: the target path guards
// (TargetForLayoutDir), a parsed layout.toml, and the dir-contents guards
// (flat dir, no stray files, every *.fragment.* referenced and every referenced
// local fragment present).
func loadArtifactDir(root, dir string) (*artifactDir, error) {
	target, kind, err := guide.TargetForLayoutDir(dir)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(root, filepath.FromSlash(dir))
	raw, err := os.ReadFile(filepath.Join(abs, "layout.toml")) // #nosec G304 -- reads the layout.toml of a discovered artifact dir
	if err != nil {
		return nil, err
	}
	lay, err := layout.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	ad := &artifactDir{dir: dir, abs: abs, target: target, kind: kind, lay: lay}
	if err := ad.validateFiles(); err != nil {
		return nil, err
	}
	return ad, nil
}

// validateFiles enforces the dir-contents guards. A typo'd entry name (referencing
// a missing file) or an unreferenced fragment file (a rename left behind) must
// error, so no prose is silently dropped.
func (ad *artifactDir) validateFiles() error {
	referenced := map[string]bool{}
	for _, e := range ad.lay.Entries {
		if !e.IsImport() {
			referenced[e.Name+".fragment"+ad.kind.Ext()] = true
		}
	}
	entries, err := os.ReadDir(ad.abs)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, de := range entries {
		name := de.Name()
		if de.IsDir() {
			return fmt.Errorf("%s: artifact dir must be flat, but %q is a subdirectory (nested layout dirs are not allowed)", ad.dir, name)
		}
		if name == "layout.toml" {
			continue
		}
		if idx := strings.Index(name, ".fragment."); idx >= 0 {
			if !strings.HasSuffix(name, ".fragment"+ad.kind.Ext()) {
				return fmt.Errorf("%s: fragment %q has the wrong extension for a %s artifact (want .fragment%s)", ad.dir, name, ad.kind, ad.kind.Ext())
			}
			if !referenced[name] {
				return fmt.Errorf("%s: fragment file %q is not referenced by layout.toml (a typo'd name would silently drop it)", ad.dir, name)
			}
			present[name] = true
			continue
		}
		return fmt.Errorf("%s: stray file %q (an artifact dir holds only layout.toml and *.fragment.%s files)", ad.dir, name, ad.kind)
	}
	var missing []string
	for f := range referenced {
		if !present[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%s: layout.toml references local fragment file(s) that do not exist: %s", ad.dir, strings.Join(missing, ", "))
	}
	return nil
}

// compose resolves every layout entry to a piece and composes the artifact body
// (without a marker). Local pieces are read from the dir; imports come from the
// resolver, which pins each alias's sha.
func (ad *artifactDir) compose(ctx context.Context, imp source.Importer) ([]byte, error) {
	pieces := make([]guide.Piece, 0, len(ad.lay.Entries))
	for _, e := range ad.lay.Entries {
		if !e.IsImport() {
			p, err := ad.localPiece(e)
			if err != nil {
				return nil, err
			}
			pieces = append(pieces, p)
			continue
		}
		p, err := importPiece(ctx, imp, e, ad.kind)
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, p)
	}
	if ad.kind == guide.KindJSON {
		return guide.ComposeJSON(pieces)
	}
	return guide.Compose(ad.kind, pieces)
}

// localPiece reads a local fragment file, enforcing load-time purity (LF,
// non-empty, not whitespace-only) and carrying the entry's args so a local piece
// declaring args is token-substituted with the same two-way strictness as an
// import.
func (ad *artifactDir) localPiece(e layout.Entry) (guide.Piece, error) {
	fname := e.Name + ".fragment" + ad.kind.Ext()
	body, err := os.ReadFile(filepath.Join(ad.abs, fname)) // #nosec G304 -- reads a validated local fragment of this artifact dir
	if err != nil {
		return guide.Piece{}, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return guide.Piece{}, fmt.Errorf("%s: local fragment %q is empty or whitespace-only", ad.dir, fname)
	}
	if bytes.IndexByte(body, '\r') >= 0 {
		return guide.Piece{}, fmt.Errorf("%w: %s/%s", guide.ErrCRLF, ad.dir, fname)
	}
	p := guide.Piece{Body: body, Origin: fname}
	if len(e.Args) > 0 {
		p.Args = e.Args
		p.Keys = e.Keys
	}
	return p, nil
}

// importPiece resolves a shared-fragment import, distinguishing a genuinely
// unknown name from a kind mismatch by probing the other kind.
func importPiece(ctx context.Context, imp source.Importer, e layout.Entry, kind guide.Kind) (guide.Piece, error) {
	body, found, err := imp.Resolve(ctx, e.Alias, e.Name, kind)
	if err != nil {
		return guide.Piece{}, err
	}
	if !found {
		for _, other := range guide.AllKinds {
			if other == kind {
				continue
			}
			if _, ok, oerr := imp.Resolve(ctx, e.Alias, e.Name, other); oerr == nil && ok {
				return guide.Piece{}, fmt.Errorf("%w: %s is a %s fragment, cannot import it into a %s artifact", guide.ErrKindMismatch, e.Ref(), other, kind)
			}
		}
		return guide.Piece{}, fmt.Errorf("%w: %s (kind %s)", guide.ErrUnknownFragment, e.Ref(), kind)
	}
	pin, _ := imp.Pin(e.Alias)
	var args map[string]string
	if len(e.Args) > 0 {
		args = e.Args
	}
	return guide.Piece{Body: body, Args: args, Keys: e.Keys, Origin: e.Ref() + "@" + pin}, nil
}
