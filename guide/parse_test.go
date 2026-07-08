package guide_test

import (
	"errors"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func TestParseDirectiveGrammar(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr error
		check   func(*testing.T, *guide.Doc)
	}{
		{
			name: "bare name",
			src:  "{{> ccx}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				inc := onlyInclude(t, d)
				if inc.Name != "ccx" {
					t.Fatalf("name = %q, want ccx", inc.Name)
				}
				if len(inc.Args) != 0 {
					t.Fatalf("args = %v, want none", inc.Args)
				}
			},
		},
		{
			name: "one arg",
			src:  "{{> f k=v}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				inc := onlyInclude(t, d)
				if inc.Args["k"] != "v" {
					t.Fatalf("args = %v, want k=v", inc.Args)
				}
			},
		},
		{
			name: "multiple args keep source order",
			src:  "{{> f a=1 b=2 c=3}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				inc := onlyInclude(t, d)
				want := []string{"a", "b", "c"}
				if len(inc.Keys) != 3 || inc.Keys[0] != want[0] || inc.Keys[1] != want[1] || inc.Keys[2] != want[2] {
					t.Fatalf("keys = %v, want %v", inc.Keys, want)
				}
			},
		},
		{
			name: "allowed value owner/tap/name",
			src:  "{{> f k=yasyf/tap/cc-review}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if got := onlyInclude(t, d).Args["k"]; got != "yasyf/tap/cc-review" {
					t.Fatalf("value = %q, want yasyf/tap/cc-review", got)
				}
			},
		},
		{
			name: "allowed value version",
			src:  "{{> f k=v1.2.3}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if got := onlyInclude(t, d).Args["k"]; got != "v1.2.3" {
					t.Fatalf("value = %q, want v1.2.3", got)
				}
			},
		},
		{
			name: "allowed value mixed punctuation",
			src:  "{{> f k=a_b-c.d}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if got := onlyInclude(t, d).Args["k"]; got != "a_b-c.d" {
					t.Fatalf("value = %q, want a_b-c.d", got)
				}
			},
		},
		{
			name: "allowed value url-ish",
			src:  "{{> f k=owner/repo@tag:x+y}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if got := onlyInclude(t, d).Args["k"]; got != "owner/repo@tag:x+y" {
					t.Fatalf("value = %q, want owner/repo@tag:x+y", got)
				}
			},
		},
		{
			name: "digit-leading name is valid",
			src:  "{{> 3fold}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if got := onlyInclude(t, d).Name; got != "3fold" {
					t.Fatalf("name = %q", got)
				}
			},
		},
		{
			name: "inline directive is prose",
			src:  "see {{> ccx}} above\n",
			check: func(t *testing.T, d *guide.Doc) {
				if hasInclude(d) {
					t.Fatal("inline {{> }} must pass through as a literal")
				}
			},
		},
		{
			name: "indented directive is prose",
			src:  "  {{> ccx}}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if hasInclude(d) {
					t.Fatal("non-column-0 {{> }} must pass through")
				}
			},
		},
		{
			name: "prose tokens pass through untouched",
			src:  "{{VAR}} and {{#SECTION}} and ${{ x }}\n",
			check: func(t *testing.T, d *guide.Doc) {
				if hasInclude(d) {
					t.Fatal("prose tokens must not become directives")
				}
			},
		},
		{name: "crlf is a hard error", src: "a\r\nb\n", wantErr: guide.ErrCRLF},
		{name: "malformed no close", src: "{{> ccx\n", wantErr: guide.ErrMalformedDirective},
		{name: "malformed trailing text", src: "{{> ccx}} tail\n", wantErr: guide.ErrMalformedDirective},
		{name: "malformed no space", src: "{{>ccx}}\n", wantErr: guide.ErrMalformedDirective},
		{name: "legacy path form", src: "{{> _partials/ccx.md}}\n", wantErr: guide.ErrLegacyPathDirective},
		{name: "legacy md suffix", src: "{{> ccx.md}}\n", wantErr: guide.ErrLegacyPathDirective},
		{name: "bad name uppercase", src: "{{> Ccx}}\n", wantErr: guide.ErrBadName},
		{name: "bad name underscore", src: "{{> a_b}}\n", wantErr: guide.ErrBadName},
		{name: "bad arg not key=value", src: "{{> f xyz}}\n", wantErr: guide.ErrBadArg},
		{name: "bad arg empty value", src: "{{> f k=}}\n", wantErr: guide.ErrBadArg},
		{name: "bad arg uppercase key", src: "{{> f K=v}}\n", wantErr: guide.ErrBadArg},
		{name: "duplicate arg", src: "{{> f k=1 k=2}}\n", wantErr: guide.ErrDuplicateArg},
		{name: "double-quoted value", src: `{{> f k="v"}}` + "\n", wantErr: guide.ErrBadArgValue},
		{name: "single-quoted value", src: "{{> f k='v'}}\n", wantErr: guide.ErrBadArgValue},
		{name: "unbalanced quote", src: `{{> f k="x}}` + "\n", wantErr: guide.ErrBadArgValue},
		{name: "command substitution value", src: "{{> f k=$(id)}}\n", wantErr: guide.ErrBadArgValue},
		{name: "backtick value", src: "{{> f k=`id`}}\n", wantErr: guide.ErrBadArgValue},
		{name: "comma in value", src: "{{> f k=foo,bar}}\n", wantErr: guide.ErrBadArgValue},
		{name: "equals no longer allowed in value", src: "{{> f k=a=b}}\n", wantErr: guide.ErrBadArgValue},
		// A space cannot reach a value: strings.Fields splits it into a bare token
		// that fails the key=value shape first.
		{name: "space in value is impossible via fields", src: "{{> f k=a b}}\n", wantErr: guide.ErrBadArg},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := guide.Parse([]byte(tc.src), guide.KindMD)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.check != nil {
				tc.check(t, doc)
			}
		})
	}
}

func onlyInclude(t *testing.T, d *guide.Doc) *guide.Include {
	t.Helper()
	for _, n := range d.Nodes {
		if n.Include != nil {
			return n.Include
		}
	}
	t.Fatal("no include node found")
	return nil
}

func hasInclude(d *guide.Doc) bool {
	for _, n := range d.Nodes {
		if n.Include != nil {
			return true
		}
	}
	return false
}
