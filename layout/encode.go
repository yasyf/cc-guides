package layout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/internal/tomlstr"
)

// Encode renders a Layout to canonical layout.toml bytes: a `target` override when
// one is set and the top-level `fragments` array, both before any table header (so
// TOML keeps them top-level), then one `[sources.<alias>]` table per declared
// source, alias-sorted. Every source is emitted — there is no baked-in default to
// omit — so a repo's layout.toml self-describes exactly which pack each import
// resolves against.
func Encode(l *Layout) []byte {
	var b strings.Builder
	if l.Target != "" {
		b.WriteString("target = ")
		b.WriteString(quote(l.Target))
		b.WriteString("\n\n")
	}
	b.WriteString("fragments = [\n")
	for _, e := range l.Entries {
		b.WriteString("  ")
		b.WriteString(encodeEntry(e))
		b.WriteString(",\n")
	}
	b.WriteString("]\n")

	aliases := make([]string, 0, len(l.Sources))
	for alias := range l.Sources {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		// A table header cannot go through quote, so an alias is written raw. Parse
		// gates it on ValidName, which makes an invalid one here a Layout built by
		// hand — impossible state, crashed on rather than emitted as broken TOML.
		// lockfile.Encode refuses the same way for the same reason.
		if !guide.ValidName(alias) {
			panic(fmt.Sprintf("layout: source alias %q is invalid; Parse rejects these, so this Layout was constructed by hand", alias))
		}
		b.WriteString("\n[sources.")
		b.WriteString(alias)
		b.WriteString("]\nsource = ")
		b.WriteString(quote(l.Sources[alias]))
		b.WriteString("\n")
	}
	return []byte(b.String())
}

func encodeEntry(e Entry) string {
	if len(e.Args) == 0 {
		return quote(e.Ref())
	}
	var b strings.Builder
	b.WriteString("{ use = ")
	b.WriteString(quote(e.Ref()))
	b.WriteString(", args = { ")
	keys := e.Keys
	if keys == nil {
		keys = sortedKeys(e.Args)
	}
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		// A bare key is written raw for the same reason a table header is, and Parse
		// gates it on ValidArgKey.
		if !guide.ValidArgKey(k) {
			panic(fmt.Sprintf("layout: argument key %q is invalid; Parse rejects these, so this Entry was constructed by hand", k))
		}
		b.WriteString(k)
		b.WriteString(" = ")
		b.WriteString(quote(e.Args[k]))
	}
	b.WriteString(" } }")
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// quote wraps a value in a TOML basic string. Only some of what reaches it is
// validated: fragment names and source aliases by guide.ValidName, argument keys and
// values by guide.ValidArgKey and guide.ValidArgValue. A source spec is not — Parse
// checks only that it is non-empty — and a target is arbitrary until
// TargetForLayoutDir judges it. The escaper is what covers the difference, so it
// escapes unconditionally instead of trusting any caller's character set.
func quote(s string) string { return tomlstr.Quote(s) }
