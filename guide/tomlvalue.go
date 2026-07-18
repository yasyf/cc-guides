package guide

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// LintTOML validates that body decodes as well-formed TOML, tolerating {{token}}
// placeholders by neutralizing each to a scalar (a real substitution runs at
// compose time). The decoder is stricter than the grammar: it rejects a table
// defined twice — the redefinition tree-sitter parses happily but TOML semantics
// forbid — so a fragment (or a composed artifact) that duplicates a table fails
// here with a message naming the offending key.
func LintTOML(body []byte) error {
	var v map[string]any
	if _, err := toml.Decode(string(neutralizeDistinctInt(body)), &v); err != nil {
		return fmt.Errorf("%w: %w", ErrTOMLDecode, err)
	}
	return nil
}
