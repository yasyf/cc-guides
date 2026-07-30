// Package tomlstr encodes Go strings as TOML basic strings. It exists so the two
// files cc-guides writes — the lock and layout.toml — share one escaper rather than
// two that can drift apart.
package tomlstr

import (
	"fmt"
	"strings"
)

// Quote wraps s in a TOML basic string, escaping every character TOML forbids raw:
// the backslash, the double quote, and every control character. A serialization
// boundary must not be able to emit TOML that cannot be parsed back, whatever it is
// handed — a raw newline reaching cc-guides.lock wrote a file every later render and
// check failed to load, recoverable only by repairing the lock by hand.
func Quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
