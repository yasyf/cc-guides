package guide

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// tokenRe matches a substitution token inside a resolved fragment body. Uppercase
// `{{VAR}}`, `{{#SECTION}}` and shell `${{ … }}` deliberately do not match — only
// declared args drive substitution, and only inside a piece that carries args.
var tokenRe = regexp.MustCompile(`\{\{[a-z][a-z0-9-]*\}\}`)

// Piece is one resolved composition input: a fragment body plus, when the layout
// entry declared them, the args that drive token substitution. A piece with a nil
// Args map is emitted byte-for-byte (never scanned for tokens) — load-bearing:
// real fleet prose contains `${{ github.run_number }}` and `{{…}}`-shaped
// literals that must survive verbatim.
type Piece struct {
	Body   []byte            // fragment body (a trailing newline is trimmed on compose)
	Args   map[string]string // nil unless the entry declared args
	Keys   []string          // arg keys (sorted) for deterministic diagnostics
	Origin string            // diagnostic label, e.g. "intro.fragment.md" or "cc-skills:ccx@abcdef012345"
}

// Compose joins resolved pieces into an artifact body WITHOUT a marker, ending in
// exactly one newline. The rules (mirrored in the plan):
//
//	(2) tokens are substituted only for pieces carrying args (two-way strict);
//	(3) each piece is TrimRight'd of trailing newlines, pieces are joined with one
//	    blank line, and the whole ends in exactly one trailing newline;
//	(4) only the FIRST piece of a .sh artifact may begin `#!` — a later piece
//	    beginning `#!` is a hard error;
//	    CRLF anywhere is a hard error.
func Compose(kind Kind, pieces []Piece) ([]byte, error) {
	parts := make([]string, 0, len(pieces))
	for i, p := range pieces {
		if bytes.IndexByte(p.Body, '\r') >= 0 {
			return nil, fmt.Errorf("%w: %s", ErrCRLF, p.Origin)
		}
		body := strings.TrimRight(string(p.Body), "\n")
		if specOf(kind).shebang && i != 0 && strings.HasPrefix(body, "#!") {
			return nil, fmt.Errorf("%w: %s", ErrShebangNotFirst, p.Origin)
		}
		if p.Args != nil {
			sub, err := substituteTokens(body, p.Args, p.Keys, p.Origin)
			if err != nil {
				return nil, err
			}
			body = sub
		}
		parts = append(parts, body)
	}
	return ensureSingleTrailingNewline([]byte(strings.Join(parts, "\n\n"))), nil
}

// substituteTokens replaces every `{{token}}` in body with its arg. Two-way
// strict: every token must have an arg and every arg (keys) must be consumed;
// either mismatch is a hard error naming the full sorted offending set, so the
// message is deterministic across runs. origin labels the offending piece.
func substituteTokens(body string, args map[string]string, keys []string, origin string) (string, error) {
	tokens := map[string]bool{}
	for _, m := range tokenRe.FindAllString(body, -1) {
		tokens[m[2:len(m)-2]] = true
	}
	var missing []string
	for tok := range tokens {
		if _, ok := args[tok]; !ok {
			missing = append(missing, "{{"+tok+"}}")
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("%w: %s needs %s but no matching arg is supplied",
			ErrTokenNoArg, origin, strings.Join(missing, ", "))
	}
	var unused []string
	for _, k := range keys {
		if !tokens[k] {
			unused = append(unused, k+"=")
		}
	}
	if len(unused) > 0 {
		sort.Strings(unused)
		return "", fmt.Errorf("%w: %s supplies %s but the fragment has no matching {{token}}",
			ErrArgUnused, origin, strings.Join(unused, ", "))
	}
	return tokenRe.ReplaceAllStringFunc(body, func(m string) string {
		return args[m[2:len(m)-2]]
	}), nil
}
