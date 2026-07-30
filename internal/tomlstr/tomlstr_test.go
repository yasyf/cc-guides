package tomlstr_test

import (
	"testing"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/yasyf/cc-guides/internal/tomlstr"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "AGENTS.md", `"AGENTS.md"`},
		{"quote", `a"b`, `"a\"b"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"newline", "a\nb", `"a\nb"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"tab", "a\tb", `"a\tb"`},
		{"backspace", "a\bb", `"a\bb"`},
		{"form feed", "a\fb", `"a\fb"`},
		{"nul", "a\x00b", `"a\u0000b"`},
		{"delete", "a\x7fb", `"a\u007Fb"`},
		{"unicode is not escaped", "é—ø", `"é—ø"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tomlstr.Quote(tt.in); got != tt.want {
				t.Fatalf("Quote(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// Whatever Quote emits must parse back as TOML, and back to the original string —
// for EVERY rune, not the handful an author thought to list. A fixed table tests the
// classes already considered, which is the wrong shape for an escaper: the class that
// breaks it is the one nobody listed. Surrogates are skipped (not valid standalone).
func TestQuoteRoundTripsEveryRune(t *testing.T) {
	failed := 0
	for r := rune(0); r <= 0x10FFFF; r++ {
		if (r >= 0xD800 && r <= 0xDFFF) || !utf8.ValidRune(r) {
			continue
		}
		in := "a" + string(r) + "b"
		var got struct {
			X string `toml:"x"`
		}
		if _, err := toml.Decode("x = "+tomlstr.Quote(in), &got); err != nil {
			failed++
			if failed <= 10 {
				t.Errorf("U+%04X: Quote emitted unparseable TOML %s: %v", r, tomlstr.Quote(in), err)
			}
			continue
		}
		if got.X != in {
			failed++
			if failed <= 10 {
				t.Errorf("U+%04X: round-tripped to %q", r, got.X)
			}
		}
	}
	if failed > 10 {
		t.Fatalf("%d runes failed to round-trip (first 10 reported)", failed)
	}
}

// The payloads the rune sweep cannot reproduce: whole strings, including the exact
// injection that bricked a lock at exit 0.
func TestQuoteRoundTripsPayloads(t *testing.T) {
	values := []string{
		"AGENTS.md",
		"a\nb",
		"ok.md\n[sources.evil]\nspec = \"pwned\"\nnote.md",
		`quote" and \ backslash`,
		"\x00\x01\x1f\x7f",
		"tab\tnewline\ncr\r",
		"é—ø",
	}
	for _, v := range values {
		var got struct {
			X string `toml:"x"`
		}
		if _, err := toml.Decode("x = "+tomlstr.Quote(v), &got); err != nil {
			t.Fatalf("Quote(%q) produced unparseable TOML %s: %v", v, tomlstr.Quote(v), err)
		}
		if got.X != v {
			t.Fatalf("round-trip of %q gave %q", v, got.X)
		}
	}
}
