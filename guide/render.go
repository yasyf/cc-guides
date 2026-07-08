package guide

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// tokenRe matches a substitution token inside a resolved fragment body. Uppercase
// `{{VAR}}`, `{{#SECTION}}` and shell `${{ … }}` deliberately do not match — only
// directive args drive substitution, and only inside fragment bodies.
var tokenRe = regexp.MustCompile(`\{\{[a-z][a-z0-9-]*\}\}`)

// Render expands doc against r and returns the artifact body WITHOUT a banner,
// ending in exactly one newline. Prose (literal nodes) is emitted byte-for-byte;
// only resolved fragment bodies are token-substituted.
func Render(doc *Doc, r Resolver) ([]byte, error) {
	var b strings.Builder
	for i, n := range doc.Nodes {
		if n.Include != nil {
			body, err := expandInclude(n.Include, doc.Kind, r)
			if err != nil {
				return nil, err
			}
			b.WriteString(body)
		} else {
			b.WriteString(n.Literal)
		}
		if i < len(doc.Nodes)-1 {
			b.WriteByte('\n')
		}
	}
	return ensureSingleTrailingNewline([]byte(b.String())), nil
}

func expandInclude(inc *Include, kind Kind, r Resolver) (string, error) {
	frag, ok, err := r.Resolve(inc.Name, kind)
	if err != nil {
		return "", err
	}
	if !ok {
		// Distinguish a truly-unknown name from a kind mismatch.
		for _, other := range AllKinds {
			if other == kind {
				continue
			}
			if _, ok2, err2 := r.Resolve(inc.Name, other); err2 == nil && ok2 {
				return "", fmt.Errorf("%w: %q is a %s fragment, cannot include it into a %s artifact (line %d)",
					ErrKindMismatch, inc.Name, other, kind, inc.Line)
			}
		}
		return "", fmt.Errorf("%w: %q (line %d)", ErrUnknownFragment, inc.Name, inc.Line)
	}

	// Fragment bodies carry a single trailing newline; strip it to line content.
	body := strings.TrimSuffix(string(frag.Body), "\n")

	if err := ensureNoNestedInclude(body, inc.Name); err != nil {
		return "", err
	}
	sub, err := substitute(body, inc, frag.Name)
	if err != nil {
		return "", err
	}
	if frag.Local {
		openMk, closeMk := localMarkers(kind, frag.Origin)
		return openMk + "\n" + sub + "\n" + closeMk, nil
	}
	return sub, nil
}

func ensureNoNestedInclude(body, fragName string) error {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, directivePrefix) {
			return fmt.Errorf("%w: fragment %q contains %q", ErrNestedInclude, fragName, line)
		}
	}
	return nil
}

// substitute replaces every `{{token}}` in body with its directive arg. Every
// token must have an arg and every arg must be consumed; either mismatch is a
// hard error naming the fragment and the full sorted set of offending
// tokens/args, so the message is deterministic across runs.
func substitute(body string, inc *Include, fragName string) (string, error) {
	tokens := map[string]bool{}
	for _, m := range tokenRe.FindAllString(body, -1) {
		tokens[m[2:len(m)-2]] = true
	}
	var missing []string
	for tok := range tokens {
		if _, ok := inc.Args[tok]; !ok {
			missing = append(missing, "{{"+tok+"}}")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("%w: fragment %q needs %s but the directive supplies no matching arg (line %d)",
			ErrTokenNoArg, fragName, strings.Join(missing, ", "), inc.Line)
	}
	var unused []string
	for _, k := range inc.Keys {
		if !tokens[k] {
			unused = append(unused, k+"=")
		}
	}
	if len(unused) > 0 {
		sort.Strings(unused)
		return "", fmt.Errorf("%w: directive passes %s but fragment %q has no matching {{token}} (line %d)",
			ErrArgUnused, strings.Join(unused, ", "), fragName, inc.Line)
	}
	return tokenRe.ReplaceAllStringFunc(body, func(m string) string {
		return inc.Args[m[2:len(m)-2]]
	}), nil
}

func localMarkers(kind Kind, origin string) (openMk, closeMk string) {
	switch kind {
	case KindMD:
		return "<!-- local: " + origin + " -->", "<!-- /local: " + origin + " -->"
	case KindSH:
		return "# local: " + origin, "# /local: " + origin
	default:
		return "", ""
	}
}
