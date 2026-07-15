package guide_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func TestComposeJoining(t *testing.T) {
	// Trailing newlines per piece are trimmed; pieces are joined by one blank line;
	// the whole ends in exactly one trailing newline.
	pieces := []guide.Piece{
		{Body: []byte("# Head\n\nIntro.\n"), Origin: "a"},
		{Body: []byte("## Shared\nbody\n\n\n"), Origin: "b"},
		{Body: []byte("End.\n"), Origin: "c"},
	}
	got, err := guide.Compose(guide.KindMD, pieces)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Head\n\nIntro.\n\n## Shared\nbody\n\nEnd.\n"
	if string(got) != want {
		t.Fatalf("compose =\n%q\nwant\n%q", got, want)
	}
}

// A yaml artifact concatenates like md/sh (one blank line between pieces, not a
// semantic merge), so load-bearing comments survive verbatim.
func TestComposeYAML(t *testing.T) {
	pieces := []guide.Piece{
		{Body: []byte("# a load-bearing comment\nname: Docs\non: push\n"), Origin: "head.yml"},
		{Body: []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n"), Origin: "jobs.yml"},
	}
	got, err := guide.Compose(guide.KindYAML, pieces)
	if err != nil {
		t.Fatal(err)
	}
	want := "# a load-bearing comment\nname: Docs\non: push\n\njobs:\n  build:\n    runs-on: ubuntu-latest\n"
	if string(got) != want {
		t.Fatalf("yaml compose =\n%q\nwant\n%q", got, want)
	}
}

func TestComposeShebangFirstOnly(t *testing.T) {
	ok := []guide.Piece{
		{Body: []byte("#!/bin/sh\nset -e\n"), Origin: "sh1"},
		{Body: []byte("echo hi\n"), Origin: "sh2"},
	}
	body, err := guide.Compose(guide.KindSH, ok)
	if err != nil {
		t.Fatalf("first-piece shebang must be allowed: %v", err)
	}
	if !strings.HasPrefix(string(body), "#!/bin/sh\n") {
		t.Fatalf("shebang must lead: %q", body)
	}

	bad := []guide.Piece{
		{Body: []byte("set -e\n"), Origin: "sh1"},
		{Body: []byte("#!/bin/sh\necho hi\n"), Origin: "sh2"},
	}
	if _, err := guide.Compose(guide.KindSH, bad); !errors.Is(err, guide.ErrShebangNotFirst) {
		t.Fatalf("err = %v, want ErrShebangNotFirst", err)
	}
}

func TestComposeArgsSubstitution(t *testing.T) {
	pieces := []guide.Piece{
		{Body: []byte("name={{binary}} repo={{repo}}\n"), Args: map[string]string{"binary": "slop-cop", "repo": "yasyf/slop-cop"}, Keys: []string{"binary", "repo"}, Origin: "imp"},
	}
	got, err := guide.Compose(guide.KindSH, pieces)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "name=slop-cop repo=yasyf/slop-cop\n" {
		t.Fatalf("substitution = %q", got)
	}
}

// A piece WITHOUT args is emitted byte-for-byte: `${{ … }}` and `{{VAR}}`-shaped
// literals must survive (real fleet prose contains them).
func TestComposeNoArgsLeavesTokens(t *testing.T) {
	body := "run ${{ github.run_number }} and {{VAR}} and {{token}}\n"
	pieces := []guide.Piece{{Body: []byte(body), Origin: "prose"}}
	got, err := guide.Compose(guide.KindMD, pieces)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("no-args piece must pass through verbatim: %q", got)
	}
}

func TestComposeMissingAndUnusedTokensSorted(t *testing.T) {
	// Missing tokens: sorted, complete, deterministic.
	missing := []guide.Piece{
		{Body: []byte("{{repo}} {{binary}} {{plugin}} {{brew}}\n"), Args: map[string]string{}, Keys: nil, Origin: "imp"},
	}
	_, err := guide.Compose(guide.KindSH, missing)
	if !errors.Is(err, guide.ErrTokenNoArg) {
		t.Fatalf("err = %v, want ErrTokenNoArg", err)
	}
	if !strings.Contains(err.Error(), "{{binary}}, {{brew}}, {{plugin}}, {{repo}}") {
		t.Fatalf("missing tokens not sorted/complete: %v", err)
	}

	// Unused args: sorted, complete.
	unused := []guide.Piece{
		{Body: []byte("just prose {{used}}\n"), Args: map[string]string{"used": "x", "zeta": "z", "alpha": "a"}, Keys: []string{"alpha", "used", "zeta"}, Origin: "imp"},
	}
	_, err = guide.Compose(guide.KindMD, unused)
	if !errors.Is(err, guide.ErrArgUnused) {
		t.Fatalf("err = %v, want ErrArgUnused", err)
	}
	if !strings.Contains(err.Error(), "alpha=, zeta=") {
		t.Fatalf("unused args not sorted/complete: %v", err)
	}
}

func TestComposeCRLFRejected(t *testing.T) {
	pieces := []guide.Piece{{Body: []byte("a\r\nb\n"), Origin: "crlf"}}
	if _, err := guide.Compose(guide.KindMD, pieces); !errors.Is(err, guide.ErrCRLF) {
		t.Fatalf("err = %v, want ErrCRLF", err)
	}
}
