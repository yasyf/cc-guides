package guide

import (
	"fmt"
	"strings"
)

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
	sub, err := substituteTokens(body, inc.Args, inc.Keys, fmt.Sprintf("fragment %q (line %d)", frag.Name, inc.Line))
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
