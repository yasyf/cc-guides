package source

import (
	"errors"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func TestParseManifest(t *testing.T) {
	valid := "name = \"cc-skills\"\ndescription = \"shared fragments\"\nguides = \"plugin/guides\"\n"
	m, err := ParseManifest([]byte(valid))
	if err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	if m.Name != "cc-skills" || m.Guides != "plugin/guides" || m.Description != "shared fragments" {
		t.Fatalf("parsed = %+v", m)
	}
}

func TestParseManifestErrors(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr error
	}{
		{"unknown key", "name = \"x\"\nguides = \"g\"\nsauce = \"typo\"\n", ErrBadManifest},
		{"missing guides", "name = \"x\"\n", ErrBadManifest},
		{"bad name", "name = \"BAD\"\nguides = \"g\"\n", ErrBadManifest},
		{"absolute guides", "name = \"x\"\nguides = \"/etc\"\n", ErrBadManifest},
		{"traversal guides", "name = \"x\"\nguides = \"../g\"\n", ErrBadManifest},
		{"unclean guides", "name = \"x\"\nguides = \"a//b\"\n", ErrBadManifest},
		{"crlf", "name = \"x\"\r\nguides = \"g\"\n", guide.ErrCRLF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.toml)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
