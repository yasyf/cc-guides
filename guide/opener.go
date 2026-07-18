package guide

import (
	"fmt"
	"slices"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// checkOpener enforces a kind's opener constraint on a non-first append fragment: the
// fragment's first content node (comments skipped) must be one of the accepted CST node
// kinds and begin its line after only ASCII space or tab. It parses the neutralized body
// with the kind's grammar, so a `{{token}}`-bearing header (e.g. `[packs.{{name}}]`) is
// not a false failure, and runs on the pre-substitution body. A comment-only or blank
// fragment adds no keys and passes. A violation wraps the constraint's sentinel and names
// the origin.
func checkOpener(oc openerConstraint, g *grammar, neutralize neutralizer, body []byte, origin string) error {
	src := body
	if neutralize != nil {
		src = neutralize(body)
	}
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(g.lang)); err != nil {
		return fmt.Errorf("%w: %w", g.wrap, err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	root := tree.RootNode()
	for i := uint(0); i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child.Kind() == "comment" {
			continue
		}
		if !slices.Contains(oc.kinds, child.Kind()) || !onlySpaceTabBefore(src, child.StartByte()) {
			return fmt.Errorf("%w: %s", oc.wrap, origin)
		}
		return nil
	}
	return nil
}

// onlySpaceTabBefore reports whether every byte from the start of start's line up to
// start is an ASCII space or tab — the only whitespace TOML permits before a table
// header, so a header preceded by any other byte (a vertical tab, say) is not a real
// opener.
func onlySpaceTabBefore(src []byte, start uint) bool {
	i := start
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	for _, b := range src[i:start] {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}
