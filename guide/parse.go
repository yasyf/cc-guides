package guide

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// Node is one line of a parsed source: either a literal passthrough line or a
// column-0 include directive. Exactly one of the fields is meaningful.
type Node struct {
	Literal string   // the raw line (no trailing newline) when Include == nil
	Include *Include // non-nil when the line is a column-0 include directive
}

// Include is a parsed `{{> name k=v …}}` directive.
type Include struct {
	Name string
	Args map[string]string
	Keys []string // argument keys in source order (determinism)
	Raw  string   // the original directive line, for diagnostics
	Line int      // 1-based line number in the source
}

// Doc is a parsed source document: an ordered list of nodes plus its kind.
type Doc struct {
	Kind  Kind
	Nodes []Node
}

const directivePrefix = "{{>"

var (
	nameRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	keyRe       = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	directiveRe = regexp.MustCompile(`^\{\{>\s+(\S.*?)\s*\}\}$`)
	// argValueRe whitelists the characters a directive value may contain: enough
	// for binary names, owner/repo, brew taps, plugin names, and versions, and
	// nothing (quotes, spaces, commas, shell metacharacters) that could leak into
	// or inject the rendered shell.
	argValueRe = regexp.MustCompile(`^[A-Za-z0-9._/@:+-]+$`)
)

// LooksLikeDirective reports whether a line would be treated as a column-0
// include directive (well-formed or not). Used by the migrator's collision scan.
func LooksLikeDirective(line string) bool {
	return strings.HasPrefix(line, directivePrefix)
}

// Parse splits src into literal and include nodes. CRLF input is a hard error;
// prose (`{{VAR}}`, `{{#SECTION}}`, `${{ … }}`, inline `{{> …}}`) passes through
// untouched — only column-0 `{{> …}}` lines become directives.
func Parse(src []byte, kind Kind) (*Doc, error) {
	if bytes.IndexByte(src, '\r') >= 0 {
		return nil, ErrCRLF
	}
	lines := strings.Split(string(src), "\n")
	doc := &Doc{Kind: kind, Nodes: make([]Node, 0, len(lines))}
	for i, line := range lines {
		if strings.HasPrefix(line, directivePrefix) {
			inc, err := parseDirective(line, i+1)
			if err != nil {
				return nil, err
			}
			doc.Nodes = append(doc.Nodes, Node{Include: inc})
			continue
		}
		doc.Nodes = append(doc.Nodes, Node{Literal: line})
	}
	return doc, nil
}

func parseDirective(line string, lineNo int) (*Include, error) {
	m := directiveRe.FindStringSubmatch(line)
	if m == nil {
		return nil, fmt.Errorf("%w on line %d: %q", ErrMalformedDirective, lineNo, line)
	}
	fields := strings.Fields(m[1])
	name := fields[0]
	if strings.Contains(name, "/") || strings.Contains(name, ".md") || strings.Contains(name, ".sh") {
		return nil, fmt.Errorf("%w on line %d: %q — use a bare name, e.g. {{> %s}}",
			ErrLegacyPathDirective, lineNo, line, legacyHint(name))
	}
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w on line %d: %q", ErrBadName, lineNo, name)
	}
	inc := &Include{Name: name, Args: map[string]string{}, Raw: line, Line: lineNo}
	for _, f := range fields[1:] {
		eq := strings.IndexByte(f, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("%w on line %d: %q (expected key=value)", ErrBadArg, lineNo, f)
		}
		k, v := f[:eq], f[eq+1:]
		if !keyRe.MatchString(k) {
			return nil, fmt.Errorf("%w on line %d: bad key %q", ErrBadArg, lineNo, k)
		}
		if v == "" {
			return nil, fmt.Errorf("%w on line %d: %q has an empty value", ErrBadArg, lineNo, f)
		}
		if !argValueRe.MatchString(v) {
			return nil, fmt.Errorf("%w on line %d: %q", ErrBadArgValue, lineNo, f)
		}
		if _, dup := inc.Args[k]; dup {
			return nil, fmt.Errorf("%w on line %d: %q", ErrDuplicateArg, lineNo, k)
		}
		inc.Args[k] = v
		inc.Keys = append(inc.Keys, k)
	}
	return inc, nil
}

// legacyHint suggests the bare name for a legacy path-form directive.
func legacyHint(name string) string {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".md")
	base = strings.TrimSuffix(base, ".sh")
	return base
}
