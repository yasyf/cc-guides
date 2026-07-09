package guide_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

var update = flag.Bool("update", false, "update golden files")

// mapResolver is a minimal in-memory resolver keyed by name+kind.
type mapResolver map[string]guide.Fragment

func (m mapResolver) Resolve(name string, kind guide.Kind) (guide.Fragment, bool, error) {
	f, ok := m[name+"|"+kind.String()]
	return f, ok, nil
}

func mr(frags ...guide.Fragment) mapResolver {
	m := mapResolver{}
	for _, f := range frags {
		m[f.Name+"|"+f.Kind.String()] = f
	}
	return m
}

func frag(name string, kind guide.Kind, body string) guide.Fragment {
	return guide.Fragment{Name: name, Kind: kind, Body: []byte(body), Origin: "embedded"}
}

func renderString(t *testing.T, src string, kind guide.Kind, r guide.Resolver) string {
	t.Helper()
	doc, err := guide.Parse([]byte(src), kind)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := guide.Render(doc, r)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(out)
}

func TestRenderMechanics(t *testing.T) {
	t.Run("substitution consumes args", func(t *testing.T) {
		r := mr(frag("greet", guide.KindSH, "hello {{who}} from {{where}}\n"))
		got := renderString(t, "{{> greet who=world where=here}}\n", guide.KindSH, r)
		if got != "hello world from here\n" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("prose around a directive is byte-exact", func(t *testing.T) {
		r := mr(frag("mid", guide.KindMD, "MIDDLE\n"))
		src := "top {{VAR}}\n{{> mid}}\nbottom ${{x}}\n"
		got := renderString(t, src, guide.KindMD, r)
		want := "top {{VAR}}\nMIDDLE\nbottom ${{x}}\n"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		r := mr(frag("f", guide.KindMD, "body\n"))
		a := renderString(t, "x\n{{> f}}\ny\n", guide.KindMD, r)
		b := renderString(t, "x\n{{> f}}\ny\n", guide.KindMD, r)
		if a != b {
			t.Fatalf("render not deterministic: %q vs %q", a, b)
		}
	})

	errCases := []struct {
		name    string
		src     string
		kind    guide.Kind
		res     guide.Resolver
		wantErr error
	}{
		{
			name:    "token without arg",
			src:     "{{> f}}\n",
			kind:    guide.KindMD,
			res:     mr(frag("f", guide.KindMD, "{{missing}}\n")),
			wantErr: guide.ErrTokenNoArg,
		},
		{
			name:    "unused arg",
			src:     "{{> f k=v}}\n",
			kind:    guide.KindMD,
			res:     mr(frag("f", guide.KindMD, "no tokens here\n")),
			wantErr: guide.ErrArgUnused,
		},
		{
			name:    "nested include",
			src:     "{{> outer}}\n",
			kind:    guide.KindMD,
			res:     mr(frag("outer", guide.KindMD, "{{> inner}}\n")),
			wantErr: guide.ErrNestedInclude,
		},
		{
			name:    "unknown fragment",
			src:     "{{> nope}}\n",
			kind:    guide.KindMD,
			res:     mr(),
			wantErr: guide.ErrUnknownFragment,
		},
		{
			name:    "kind mismatch",
			src:     "{{> shonly}}\n",
			kind:    guide.KindMD,
			res:     mr(frag("shonly", guide.KindSH, "echo hi\n")),
			wantErr: guide.ErrKindMismatch,
		},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := guide.Parse([]byte(tc.src), tc.kind)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, err = guide.Render(doc, tc.res)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRenderLocalOverrideMarkers(t *testing.T) {
	t.Run("md", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "ccx.md"), "## Local\n\nlocal body\n")
		chain := guide.NewChain(
			guide.NewDirResolver(dir, ".claude/fragments"),
			mr(frag("ccx", guide.KindMD, "## Embedded\n")),
		)
		got := renderString(t, "before\n{{> ccx}}\nafter\n", guide.KindMD, chain)
		want := "before\n" +
			"<!-- local: .claude/fragments/ccx.md -->\n" +
			"## Local\n\nlocal body\n" +
			"<!-- /local: .claude/fragments/ccx.md -->\n" +
			"after\n"
		if got != want {
			t.Fatalf("got:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("sh", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "hook.sh"), "echo local\n")
		chain := guide.NewChain(
			guide.NewDirResolver(dir, ".claude/fragments"),
			mr(frag("hook", guide.KindSH, "echo embedded\n")),
		)
		got := renderString(t, "{{> hook}}\n", guide.KindSH, chain)
		want := "# local: .claude/fragments/hook.sh\n" +
			"echo local\n" +
			"# /local: .claude/fragments/hook.sh\n"
		if got != want {
			t.Fatalf("got:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("embedded inlines with zero markers", func(t *testing.T) {
		r := mr(frag("ccx", guide.KindMD, "## Embedded\nbody\n"))
		got := renderString(t, "{{> ccx}}\n", guide.KindMD, r)
		if got != "## Embedded\nbody\n" {
			t.Fatalf("embedded must inline without markers, got %q", got)
		}
	})
}

// TestRenderGolden exercises a multi-fragment render against a checked-in golden.
func TestRenderGolden(t *testing.T) {
	r := mr(
		frag("alpha", guide.KindMD, "## Alpha\n\nAlpha body.\n"),
		frag("param", guide.KindMD, "Env is {{env}} on {{host}}.\n"),
	)
	src := "# Head\n\nIntro {{VAR}} passes through.\n\n{{> alpha}}\n\nBridge.\n\n{{> param env=prod host=web1}}\n\nEnd.\n"
	got := renderString(t, src, guide.KindMD, r)
	final := guide.AddBanner(guide.KindMD, "1.2.3", "AGENTS.src.md", "cc-skills@abcdef012345", []byte(got))
	checkGolden(t, "render_full.golden.md", final)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p) // #nosec G304 -- test reads a fixed testdata golden path
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("golden %s mismatch\n got:\n%s\nwant:\n%s", name, got, want)
	}
}
