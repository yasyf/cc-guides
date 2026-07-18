package guide

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"

	tstoml "github.com/tree-sitter-grammars/tree-sitter-toml/bindings/go"
	tsyaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
	tsbash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tsjson "github.com/tree-sitter/tree-sitter-json/bindings/go"
)

// category selects how an artifact's pieces combine.
type category int

const (
	// catAppend concatenates pieces with blank-line joins, honoring an optional opener
	// constraint (md/sh/yml/toml).
	catAppend category = iota
	// catMerge deep-merges pieces through the kind's codec into one ordered tree (json).
	catMerge
)

// neutralizer rewrites a fragment body's {{token}} placeholders into parseable
// stand-ins before a grammar or opener check parses it.
type neutralizer func([]byte) []byte

// grammar pairs a tree-sitter language with the sentinel its parse failure wraps.
type grammar struct {
	lang unsafe.Pointer
	wrap error
}

// codec is a merge kind's format bridge: a strict single-object-root parse into the
// order-preserving tree and a canonical re-encode ending in one newline.
type codec interface {
	parse([]byte) (treeValue, error)
	encode(treeValue) ([]byte, error)
}

// openerConstraint requires every append fragment after the first to open with one of
// the accepted CST node kinds; a violation wraps the sentinel. It requires a grammar.
type openerConstraint struct {
	kinds []string
	wrap  error
}

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
	name     string
	exts     []string // accepted extensions, primary first (yml also accepts .yaml)
	category category
	comment  commentSyntax // empty open = markerless (lock-managed; today's JSON)
	shebang  bool          // marker follows a leading shebang; new files are executable
	newMode  os.FileMode   // mode for a freshly created artifact

	lintRules  []func([]byte) error
	grammar    *grammar           // tree-sitter syntax gate; nil where unchecked (md)
	neutralize neutralizer        // nil = fragments checked verbatim
	semantic   func([]byte) error // decoder-strict check; doubles as post-compose validation for append kinds

	codec  codec             // required iff category == catMerge
	opener *openerConstraint // optional append-boundary rule; requires grammar
}

// specs is the per-kind registry, indexed by Kind. The ordered slice literal keeps
// String/Ext/KindFromExt deterministic.
var specs = []spec{
	KindMD: {
		name:      "md",
		exts:      []string{".md"},
		category:  catAppend,
		comment:   commentSyntax{open: "<!-- ", close: " -->", match: "<!--"},
		newMode:   0o644,
		lintRules: []func([]byte) error{mdTokenFree},
		// Markdown's syntax validator is deliberately nil: CommonMark parses anything,
		// and this dodges tree-sitter-markdown's split block/inline two-grammar model.
	},
	KindSH: {
		name:       "sh",
		exts:       []string{".sh"},
		category:   catAppend,
		comment:    commentSyntax{open: "# ", match: "# "},
		shebang:    true,
		newMode:    0o755,
		lintRules:  []func([]byte) error{shShebang, shNoMustache},
		grammar:    &grammar{lang: bashLang, wrap: ErrShellParse},
		neutralize: neutralizeBareName,
	},
	KindJSON: {
		name:       "json",
		exts:       []string{".json"},
		category:   catMerge,
		comment:    commentSyntax{}, // markerless: managed-ness comes from the lock alone
		newMode:    0o644,
		grammar:    &grammar{lang: jsonLang, wrap: ErrJSONParse},
		neutralize: neutralizeZero,
		semantic:   LintJSON,
		codec:      jsonCodec{},
	},
	KindYAML: {
		name:       "yml",
		exts:       []string{".yml", ".yaml"},
		category:   catAppend,
		comment:    commentSyntax{open: "# ", match: "# "},
		newMode:    0o644,
		grammar:    &grammar{lang: yamlLang, wrap: ErrYAMLParse},
		neutralize: neutralizeBareName,
		semantic:   LintYAML,
	},
	KindTOML: {
		name:       "toml",
		exts:       []string{".toml"},
		category:   catAppend, // concat: fragments are disjoint table sets, comments + marker survive
		comment:    commentSyntax{open: "# ", match: "# "},
		newMode:    0o644,
		grammar:    &grammar{lang: tomlLang, wrap: ErrTOMLParse},
		neutralize: neutralizeDistinctInt,
		semantic:   LintTOML, // BurntSushi decode catches a duplicate table, which tree-sitter cannot
		opener:     &openerConstraint{kinds: []string{"table", "table_array_element"}, wrap: ErrTOMLRootKey},
	},
}

// specOf returns the spec for a kind. Every Kind is minted by KindFromExt, so the
// index is always in range for the category/marker/mode/lint queries.
func specOf(k Kind) spec { return specs[k] }

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
	if err := syntaxCheck(s.grammar, s.neutralize, body); err != nil {
		return append(out, err.Error())
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
	if s.category != catAppend || s.semantic == nil {
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

// syntaxCheck runs the kind's tree-sitter grammar over the neutralized fragment body,
// wrapping a parse failure in the grammar's sentinel. A nil grammar is unchecked (md);
// a nil neutralizer parses the body verbatim.
func syntaxCheck(g *grammar, neutralize neutralizer, body []byte) error {
	if g == nil {
		return nil
	}
	src := body
	if neutralize != nil {
		src = neutralize(body)
	}
	return treeSitterSyntax(g.lang, g.wrap, src)
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

// neutralizeZero replaces every `{{token}}` with the scalar 0 so a placeholder-
// bearing fragment parses as valid JSON — mirrors LintJSON's tolerance.
func neutralizeZero(body []byte) []byte {
	return tokenRe.ReplaceAll(body, []byte("0"))
}

// neutralizeBareName replaces every `{{token}}` with its bare name — a valid word in
// both bash and YAML — so a legitimately mustache-templated fragment parses and distinct
// tokens stay distinct. GitHub-Actions `${{ … }}` expressions do not match tokenRe (the
// space after `{{` and the uppercase/dotted body), so they are left verbatim and still
// fail the bash syntax check — a declared, intended limitation.
func neutralizeBareName(body []byte) []byte {
	return tokenRe.ReplaceAllFunc(body, func(m []byte) []byte { return m[2 : len(m)-2] })
}

// neutralizeDistinctInt replaces every distinct `{{token}}` with its own numeric literal so a
// placeholder-bearing fragment parses as valid TOML in either key or value position (an
// all-digit bare key is legal, as is an integer value) while distinct tokens stay distinct
// — `[packs.{{first}}]` and `[packs.{{second}}]` must not both collapse to one table and
// read as a false duplicate. Each token's literal is the smallest integer >= 1000 that is
// neither already assigned nor a decimal literal already present in the body, so a source
// table like `[packs.1000]` cannot collide with a neutralized token and read as a false
// duplicate either. A token inside a typed scalar (a date like `2026-{{month}}-01`) can
// still neutralize to a malformed typed value: context-free substitution cannot satisfy
// every typed position.
func neutralizeDistinctInt(body []byte) []byte {
	literals := map[string]bool{}
	for _, m := range decimalRe.FindAll(body, -1) {
		literals[string(m)] = true
	}
	assigned := map[string][]byte{}
	next := 1000
	return tokenRe.ReplaceAllFunc(body, func(m []byte) []byte {
		name := string(m[2 : len(m)-2])
		lit, ok := assigned[name]
		if !ok {
			for literals[strconv.Itoa(next)] {
				next++
			}
			lit = []byte(strconv.Itoa(next))
			assigned[name] = lit
			next++
		}
		return lit
	})
}
