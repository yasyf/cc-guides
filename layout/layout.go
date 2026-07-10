// Package layout parses and validates a `.claude/fragments/<target>/layout.toml`
// file — the per-repo config surface that composes an ordered list of local
// fragments and imports of shared fragments from another repo. The schema mirrors
// capt-hook's packs.toml idiom: a single ordered, heterogeneous `fragments` array
// (string shorthand or `{ use, args }` inline tables) plus optional
// `[sources.<alias>]` tables.
//
// Unknown keys are a hard error, never a silent empty render: a typo'd key must
// fail loudly. Every imported alias must be declared by a `[sources.<alias>]`
// table — there is no baked-in default source; a layout addresses its packs
// explicitly, exactly like packs.toml.
package layout

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/yasyf/cc-guides/guide"
)

// Sentinel errors. All denote invalid input (CLI exit 2).
var (
	// ErrUnknownKey is any key the schema does not recognize (strict rejection).
	ErrUnknownKey = errors.New("unknown key in layout.toml")
	// ErrFragmentsNested is the top-level `fragments` array written after a
	// [sources.<alias>] header, so TOML nests it under that table.
	ErrFragmentsNested = errors.New("the `fragments` array must be a top-level key, above any [sources.*] table")
	// ErrEmptyCompose is a layout with no fragments to compose.
	ErrEmptyCompose = errors.New("layout.toml composes no fragments (the `fragments` array is empty or missing)")
	// ErrBadEntry is a fragments entry that is neither a string nor a { use, args } table.
	ErrBadEntry = errors.New("invalid fragments entry")
	// ErrBadName is a fragment name or source alias that violates ^[a-z0-9][a-z0-9-]*$.
	ErrBadName = errors.New("invalid fragment name or source alias")
	// ErrBadArg is a malformed argument key or value.
	ErrBadArg = errors.New("invalid entry argument")
	// ErrUndeclaredAlias is an import whose alias no [sources.*] table declares.
	ErrUndeclaredAlias = errors.New("import references an undeclared source alias")
	// ErrEmptySource is a [sources.<alias>] table with no `source` spec string.
	ErrEmptySource = errors.New("[sources.<alias>] is missing its `source` spec")
)

// Entry is one composed fragment: a local prose piece (Alias == "") or an import
// of a shared fragment (Alias != "").
type Entry struct {
	Alias string            // "" for a local fragment; else the source alias
	Name  string            // fragment name
	Args  map[string]string // nil unless the entry declared args
	Keys  []string          // arg keys, sorted, for deterministic diagnostics
}

// IsImport reports whether the entry imports a shared fragment.
func (e Entry) IsImport() bool { return e.Alias != "" }

// Ref returns the entry's canonical reference (`alias:name` or `name`).
func (e Entry) Ref() string {
	if e.Alias != "" {
		return e.Alias + ":" + e.Name
	}
	return e.Name
}

// Layout is a parsed, validated layout.toml.
type Layout struct {
	Sources map[string]string // alias -> spec string
	Entries []Entry
}

type rawLayout struct {
	Sources   map[string]rawSource `toml:"sources"`
	Fragments []toml.Primitive     `toml:"fragments"`
}

type rawSource struct {
	Source string `toml:"source"`
}

type rawEntry struct {
	Use  string            `toml:"use"`
	Args map[string]string `toml:"args"`
}

// Parse decodes and validates layout.toml bytes.
func Parse(data []byte) (*Layout, error) {
	// CRLF anywhere is a hard error, consistent with fragment/source purity — a
	// generated artifact's config must never carry CR bytes.
	if bytes.IndexByte(data, '\r') >= 0 {
		return nil, guide.ErrCRLF
	}
	var raw rawLayout
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("layout.toml: %w", err)
	}

	lay := &Layout{Sources: map[string]string{}}
	for alias, s := range raw.Sources {
		if !guide.ValidName(alias) {
			return nil, fmt.Errorf("%w: source alias %q", ErrBadName, alias)
		}
		if s.Source == "" {
			return nil, fmt.Errorf("%w: [sources.%s]", ErrEmptySource, alias)
		}
		lay.Sources[alias] = s.Source
	}

	for i, prim := range raw.Fragments {
		e, err := decodeEntry(&md, prim, i)
		if err != nil {
			return nil, err
		}
		lay.Entries = append(lay.Entries, e)
	}

	// Strict unknown-key rejection, after every primitive is decoded so their
	// sub-keys count as decoded. A typo'd key must hard-error, not render empty.
	if und := md.Undecoded(); len(und) > 0 {
		// A common footgun: writing the top-level `fragments` array below a
		// [sources.<alias>] header nests it under that table. Point straight at it.
		for _, k := range und {
			if len(k) > 1 && k[len(k)-1] == "fragments" {
				return nil, ErrFragmentsNested
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrUnknownKey, undecodedList(und))
	}

	if len(lay.Entries) == 0 {
		return nil, ErrEmptyCompose
	}

	for _, e := range lay.Entries {
		if e.IsImport() {
			if _, ok := lay.Sources[e.Alias]; !ok {
				return nil, fmt.Errorf("%w: %q (declare [sources.%s] or fix the alias)", ErrUndeclaredAlias, e.Alias, e.Alias)
			}
		}
	}
	return lay, nil
}

// decodeEntry decodes one heterogeneous fragments entry: a bare string, or an
// inline `{ use, args }` table. PrimitiveDecode into a string fails cleanly for a
// table (and vice versa) without marking keys decoded, so the string-first probe
// is safe.
func decodeEntry(md *toml.MetaData, prim toml.Primitive, idx int) (Entry, error) {
	var s string
	if err := md.PrimitiveDecode(prim, &s); err == nil {
		return parseRef(s, nil, idx)
	}
	var re rawEntry
	if err := md.PrimitiveDecode(prim, &re); err != nil {
		return Entry{}, fmt.Errorf("%w: fragments[%d] must be a string or a { use, args } table", ErrBadEntry, idx)
	}
	if re.Use == "" {
		return Entry{}, fmt.Errorf("%w: fragments[%d] table needs a `use` key", ErrBadEntry, idx)
	}
	return parseRef(re.Use, re.Args, idx)
}

// parseRef splits a `name` or `alias:name` reference and validates it plus any args.
func parseRef(ref string, args map[string]string, idx int) (Entry, error) {
	e := Entry{}
	if colon := strings.IndexByte(ref, ':'); colon >= 0 {
		e.Alias = ref[:colon]
		e.Name = ref[colon+1:]
		if !guide.ValidName(e.Alias) {
			return Entry{}, fmt.Errorf("%w: fragments[%d] alias %q", ErrBadName, idx, e.Alias)
		}
	} else {
		e.Name = ref
	}
	if !guide.ValidName(e.Name) {
		return Entry{}, fmt.Errorf("%w: fragments[%d] name %q", ErrBadName, idx, e.Name)
	}
	if len(args) > 0 {
		e.Args = map[string]string{}
		for k, v := range args {
			if !guide.ValidArgKey(k) {
				return Entry{}, fmt.Errorf("%w: fragments[%d] arg key %q", ErrBadArg, idx, k)
			}
			if !guide.ValidArgValue(v) {
				return Entry{}, fmt.Errorf("%w: fragments[%d] arg %q=%q (allowed value characters: letters, digits, and ._/@:+-)", ErrBadArg, idx, k, v)
			}
			e.Args[k] = v
			e.Keys = append(e.Keys, k)
		}
		sort.Strings(e.Keys)
	}
	return e, nil
}

// UsedAliases returns the set of source aliases the entries actually import,
// sorted — the aliases whose pins appear in the banner.
func (l *Layout) UsedAliases() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range l.Entries {
		if e.IsImport() && !seen[e.Alias] {
			seen[e.Alias] = true
			out = append(out, e.Alias)
		}
	}
	sort.Strings(out)
	return out
}

func undecodedList(keys []toml.Key) string {
	s := make([]string, len(keys))
	for i, k := range keys {
		s[i] = k.String()
	}
	sort.Strings(s)
	return strings.Join(s, ", ")
}
