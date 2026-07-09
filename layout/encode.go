package layout

import (
	"sort"
	"strings"
)

// Encode renders a Layout to canonical layout.toml bytes: the top-level
// `fragments` array first (so TOML keeps it top-level), then one
// `[sources.<alias>]` table per NON-default source. The baked-in
// cc-skills → DefaultSourceSpec source is omitted — it is injected on parse — so a
// migrated repo's layout.toml carries only its fragments.
func Encode(l *Layout) []byte {
	var b strings.Builder
	b.WriteString("fragments = [\n")
	for _, e := range l.Entries {
		b.WriteString("  ")
		b.WriteString(encodeEntry(e))
		b.WriteString(",\n")
	}
	b.WriteString("]\n")

	aliases := make([]string, 0, len(l.Sources))
	for alias, spec := range l.Sources {
		if alias == DefaultAlias && spec == DefaultSourceSpec {
			continue
		}
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
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

// quote wraps a value in a TOML basic string. Every value that reaches here is a
// spec, alias:name ref, or an argument value already constrained to a quote-free,
// backslash-free character set, so a plain double-quote wrap is exact.
func quote(s string) string {
	return `"` + s + `"`
}
