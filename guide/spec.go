package guide

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	tstoml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	tsyaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
	tsbash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tsjson "github.com/tree-sitter/tree-sitter-json/bindings/go"
)

// composeStrategy selects how an artifact's pieces combine.
type composeStrategy int

const (
	// strategyAppend concatenates pieces with blank-line joins (md/sh/yml).
	strategyAppend composeStrategy = iota
	// strategyMerge deep-merges pieces through the order-preserving JSON codec.
	strategyMerge
)

// commentSyntax describes how a kind wraps the generated marker line. An empty
// open marks the kind markerless: it stamps no marker, so its managed-ness is
// decided by lockfile membership alone (today's JSON behavior).
type commentSyntax struct {
	open  string // rendered marker prefix, e.g. "<!-- " or "# "
	close string // rendered marker suffix, e.g. " -->" (empty for a line comment)
	match string // prefix a line must carry to be recognized as this kind's marker
}

// markered reports whether the kind stamps a marker.
func (c commentSyntax) markered() bool { return c.open != "" }

// spec is the declarative behavior of one file kind: the registry holds exactly
// one spec per Kind. Adding a language is a new Kind constant plus one spec entry,
// not a new branch in every kind-aware switch.
type spec struct {
	name      string
	exts      []string // accepted extensions, primary first (yml also accepts .yaml)
	strategy  composeStrategy
	comment   commentSyntax
	shebang   bool        // marker follows a leading shebang; new files are executable
	newMode   os.FileMode // mode for a freshly created artifact
	lintRules []func([]byte) error
	syntax    func([]byte) error // tree-sitter syntax check; nil where syntax is unchecked (md)
	semantic  func([]byte) error // strict semantic check where syntax is not enough; nil otherwise
}

// specs is the per-kind registry, indexed by Kind. The ordered slice literal keeps
// String/Ext/KindFromExt deterministic.
var specs = []spec{
	KindMD: {
		name:      "md",
		exts:      []string{".md"},
		strategy:  strategyAppend,
		comment:   commentSyntax{open: "<!-- ", close: " -->", match: "<!--"},
		newMode:   0o644,
		lintRules: []func([]byte) error{mdTokenFree},
		// Markdown's syntax validator is deliberately nil: CommonMark parses anything,
		// and this dodges tree-sitter-markdown's split block/inline two-grammar model.
	},
	KindSH: {
		name:      "sh",
		exts:      []string{".sh"},
		strategy:  strategyAppend,
		comment:   commentSyntax{open: "# ", match: "# "},
		shebang:   true,
		newMode:   0o755,
		lintRules: []func([]byte) error{shShebang, shNoMustache},
		syntax:    bashSyntax,
	},
	KindJSON: {
		name:     "json",
		exts:     []string{".json"},
		strategy: strategyMerge,
		comment:  commentSyntax{}, // markerless: managed-ness comes from the lock alone
		newMode:  0o644,
		syntax:   jsonSyntax,
		semantic: LintJSON,
	},
	KindYAML: {
		name:     "yml",
		exts:     []string{".yml", ".yaml"},
		strategy: strategyAppend,
		comment:  commentSyntax{open: "# ", match: "# "},
		newMode:  0o644,
		syntax:   yamlSyntax,
		semantic: LintYAML,
	},
	KindTOML: {
		name:     "toml",
		exts:     []string{".toml"},
		strategy: strategyAppend, // concat: fragments are disjoint table sets, comments + marker survive
		comment:  commentSyntax{open: "# ", match: "# "},
		newMode:  0o644,
		syntax:   tomlSyntax,
		semantic: LintTOML, // BurntSushi decode catches a duplicate table, which tree-sitter cannot
	},
}

// specOf returns the spec for a kind. Every Kind is minted by KindFromExt, so the
// index is always in range for the strategy/marker/mode/lint queries.
func specOf(k Kind) spec { return specs[k] }

// Merges reports whether the kind composes by deep merge (JSON) rather than by
// ordered concatenation.
func (k Kind) Merges() bool { return specOf(k).strategy == strategyMerge }

// Markered reports whether the kind stamps a generated marker. A markerless kind
// (JSON) is recognized as managed only through the lock.
func (k Kind) Markered() bool { return specOf(k).comment.markered() }

// NewFileMode is the filesystem mode for a freshly created artifact of this kind.
func (k Kind) NewFileMode() os.FileMode { return specOf(k).newMode }

// Lint returns the kind-specific fragment violations for body: the kind's textual
// rules, then a tree-sitter syntax check, then — only when syntax passes — the
// strict semantic check. Universal purity (LF, single trailing newline, non-empty)
// is enforced by the caller, not here.
func (k Kind) Lint(body []byte) []string {
	s := specOf(k)
	var out []string
	for _, rule := range s.lintRules {
		if err := rule(body); err != nil {
			out = append(out, err.Error())
		}
	}
	if s.syntax != nil {
		if err := s.syntax(body); err != nil {
			return append(out, err.Error())
		}
	}
	if s.semantic != nil {
		if err := s.semantic(body); err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}

// PostComposeValidate re-runs a kind's SEMANTIC validator over a COMPOSED artifact
// body at render time — the first render-path validation. It catches the one class of
// mistake per-fragment lint cannot see: two fragments each valid alone but composing
// to a broken whole — the same TOML table defined in two of them, or a duplicate
// top-level YAML key — which surfaces here, named against the target, rather than at
// capt-hook parse time.
//
// Only the semantic validator runs, and only for an Append kind that has one. The
// per-fragment tree-sitter SYNTAX check is deliberately NOT re-run post-compose: it is
// a per-fragment purity check (a concatenation of syntactically-valid fragments stays
// syntactically valid), and a composed shell artifact legitimately carries GitHub
// Actions `${{ … }}` expressions that pass through verbatim — valid YAML, but which
// tree-sitter-bash rejects (verified against the live fleet). Running the syntax check
// post-compose would fail those legitimate renders; the semantic decoders (yaml/toml)
// accept `${{ … }}` fine. Net effect: yml and toml gain post-compose validation; sh
// (no semantic validator) and md (neither) are untouched, and a Merge kind (JSON) is
// already validated structurally by its own codec.
//
// It is validation only: it inspects body and returns an error, never mutating it, so
// rendered bytes are byte-identical to before this hook existed.
func (k Kind) PostComposeValidate(body []byte) error {
	s := specOf(k)
	if s.strategy != strategyAppend || s.semantic == nil {
		return nil
	}
	return s.semantic(body)
}

// extIndex maps every accepted extension (lowercase) to its kind.
var extIndex = func() map[string]Kind {
	m := map[string]Kind{}
	for k, s := range specs {
		for _, ext := range s.exts {
			m[ext] = Kind(k)
		}
	}
	return m
}()

// supportedExts is the comma-joined extension list, in registry order, for the
// unknown-extension diagnostic.
var supportedExts = func() string {
	var all []string
	for _, s := range specs {
		all = append(all, s.exts...)
	}
	return strings.Join(all, ", ")
}()

// mdTokenFree rejects a markdown fragment carrying a `{{token}}` placeholder — prose
// is never token-substituted, so a token in it is a mistake.
func mdTokenFree(body []byte) error {
	if m := tokenRe.Find(body); m != nil {
		return fmt.Errorf("markdown fragment must be token-free, found %q", m)
	}
	return nil
}

// shShebang rejects a shell fragment that does not open with a #!/bin/sh shebang.
func shShebang(body []byte) error {
	if !strings.HasPrefix(string(body), "#!/bin/sh\n") {
		return errors.New("shell fragment must start with a #!/bin/sh shebang")
	}
	return nil
}

// shNoMustache rejects a shell fragment carrying leftover mustache block markers.
func shNoMustache(body []byte) error {
	if s := string(body); strings.Contains(s, "{{#") || strings.Contains(s, "{{/") {
		return errors.New("leftover mustache block markers ({{# / {{/)")
	}
	return nil
}

// Grammar language pointers, loaded once at startup; each grammar vendors its C
// source, so no network or file access happens at parse time.
var (
	bashLang = tsbash.Language()
	jsonLang = tsjson.Language()
	yamlLang = tsyaml.Language()
	tomlLang = tstoml.Language()
)

func bashSyntax(body []byte) error { return treeSitterSyntax(bashLang, ErrShellParse, body) }
func jsonSyntax(body []byte) error {
	return treeSitterSyntax(jsonLang, ErrJSONParse, jsonNeutralize(body))
}

func yamlSyntax(body []byte) error {
	return treeSitterSyntax(yamlLang, ErrYAMLParse, yamlNeutralize(body))
}

func tomlSyntax(body []byte) error {
	return treeSitterSyntax(tomlLang, ErrTOMLParse, tomlNeutralize(body))
}

// treeSitterSyntax parses src with the grammar and rejects on any ERROR or MISSING
// node, wrapping the failure so callers can match it and the message names the
// language. A grammar/runtime ABI mismatch surfaces as a wrapped error too.
func treeSitterSyntax(lang unsafe.Pointer, wrap error, src []byte) error {
	parser := ts.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(ts.NewLanguage(lang)); err != nil {
		return fmt.Errorf("%w: %w", wrap, err)
	}
	tree := parser.Parse(src, nil)
	defer tree.Close()
	if tree.RootNode().HasError() {
		return fmt.Errorf("%w (tree-sitter)", wrap)
	}
	return nil
}

// jsonNeutralize replaces every `{{token}}` with the scalar 0 so a placeholder-
// bearing fragment parses as valid JSON — mirrors LintJSON's tolerance.
func jsonNeutralize(body []byte) []byte {
	return tokenRe.ReplaceAll(body, []byte("0"))
}

// yamlNeutralize replaces every `{{token}}` with its bare name so placeholders parse
// as valid YAML scalars and distinct tokens stay distinct — mirrors LintYAML.
func yamlNeutralize(body []byte) []byte {
	return tokenRe.ReplaceAllFunc(body, func(m []byte) []byte { return m[2 : len(m)-2] })
}

// tomlNeutralize replaces every `{{token}}` with the scalar 0 so a placeholder-
// bearing fragment parses as valid TOML in either key or value position — mirrors
// jsonNeutralize (a bare identifier is not a valid TOML value, so 0, not the name).
func tomlNeutralize(body []byte) []byte {
	return tokenRe.ReplaceAll(body, []byte("0"))
}
