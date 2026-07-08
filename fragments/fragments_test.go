package fragments_test

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/fragments"
	"github.com/yasyf/cc-guides/guide"
)

var lowerToken = regexp.MustCompile(`\{\{[a-z][a-z0-9-]*\}\}`)

func entries(t *testing.T) []guide.Entry {
	t.Helper()
	l, ok := fragments.Resolver().(guide.Lister)
	if !ok {
		t.Fatal("embedded resolver must implement guide.Lister")
	}
	return l.Entries()
}

func TestExactFragmentNames(t *testing.T) {
	var md, sh []string
	for _, e := range entries(t) {
		switch e.Kind {
		case guide.KindMD:
			md = append(md, e.Name)
		case guide.KindSH:
			sh = append(sh, e.Name)
		}
	}
	sort.Strings(md)
	sort.Strings(sh)

	wantMD := []string{"ask-before-assuming", "ccx", "code-review-response", "parallelize", "version-control", "writing-plans"}
	wantSH := []string{"install-binary-latest", "install-binary-pinned"}

	if !eq(md, wantMD) {
		t.Errorf("md fragments = %v, want %v", md, wantMD)
	}
	if !eq(sh, wantSH) {
		t.Errorf("sh fragments = %v, want %v", sh, wantSH)
	}
}

func TestFragmentPurity(t *testing.T) {
	r := fragments.Resolver()
	for _, e := range entries(t) {
		f, ok, err := r.Resolve(e.Name, e.Kind)
		if err != nil || !ok {
			t.Fatalf("resolve %s: ok=%v err=%v", e.Name, ok, err)
		}
		body := f.Body
		name := e.Name

		if bytes.IndexByte(body, '\r') >= 0 {
			t.Errorf("%s: contains CR (must be LF-only)", name)
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			t.Errorf("%s: must end with a newline", name)
		}
		if bytes.HasSuffix(body, []byte("\n\n")) {
			t.Errorf("%s: must end with exactly one trailing newline", name)
		}
		if e.Origin != "embedded" {
			t.Errorf("%s: origin = %q, want embedded", name, e.Origin)
		}
	}
}

func TestMarkdownGuidesAreTokenFree(t *testing.T) {
	r := fragments.Resolver()
	for _, e := range entries(t) {
		if e.Kind != guide.KindMD {
			continue
		}
		f, _, _ := r.Resolve(e.Name, e.Kind)
		if m := lowerToken.FindAll(f.Body, -1); m != nil {
			t.Errorf("%s: markdown guides must be token-free, found %q", e.Name, m)
		}
	}
}

func TestShellFragmentsHaveShebangAndFourTokens(t *testing.T) {
	r := fragments.Resolver()
	wantTokens := []string{"{{binary}}", "{{brew}}", "{{plugin}}", "{{repo}}"}
	for _, e := range entries(t) {
		if e.Kind != guide.KindSH {
			continue
		}
		f, _, _ := r.Resolve(e.Name, e.Kind)
		if !bytes.HasPrefix(f.Body, []byte("#!/bin/sh\n")) {
			t.Errorf("%s: shell fragment must start with a shebang", e.Name)
		}
		var got []string
		seen := map[string]bool{}
		for _, m := range lowerToken.FindAll(f.Body, -1) {
			s := string(m)
			if !seen[s] {
				seen[s] = true
				got = append(got, s)
			}
		}
		sort.Strings(got)
		if !eq(got, wantTokens) {
			t.Errorf("%s: tokens = %v, want %v", e.Name, got, wantTokens)
		}
		// No leftover mustache section/marker syntax.
		if strings.Contains(string(f.Body), "{{#") || strings.Contains(string(f.Body), "{{/") {
			t.Errorf("%s: leftover mustache block markers", e.Name)
		}
		if strings.Contains(string(f.Body), "canonical:") {
			t.Errorf("%s: canonical stamp not stripped", e.Name)
		}
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
