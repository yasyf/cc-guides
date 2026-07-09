package layout_test

import (
	"errors"
	"testing"

	"github.com/yasyf/cc-guides/guide"
	"github.com/yasyf/cc-guides/layout"
)

const validTOML = `fragments = [
  "agents-md",
  "cc-skills:ccx",
  { use = "cc-skills:install-binary-latest", args = { binary = "slop-cop", plugin = "slop-cop", repo = "yasyf/slop-cop", brew = "yasyf/tap/slop-cop" } },
  "learned-workspace-facts",
]

[sources.cc-skills]
source = "github:yasyf/cc-skills//guides@main"
`

func TestParseValid(t *testing.T) {
	lay, err := layout.Parse([]byte(validTOML))
	if err != nil {
		t.Fatal(err)
	}
	if len(lay.Entries) != 4 {
		t.Fatalf("entries = %d, want 4", len(lay.Entries))
	}
	if lay.Entries[0].IsImport() || lay.Entries[0].Name != "agents-md" {
		t.Fatalf("entry 0 = %+v, want local agents-md", lay.Entries[0])
	}
	if !lay.Entries[1].IsImport() || lay.Entries[1].Ref() != "cc-skills:ccx" {
		t.Fatalf("entry 1 = %+v, want import cc-skills:ccx", lay.Entries[1])
	}
	e := lay.Entries[2]
	if !e.IsImport() || e.Name != "install-binary-latest" || e.Args["binary"] != "slop-cop" {
		t.Fatalf("entry 2 = %+v", e)
	}
	// Arg keys are sorted for deterministic diagnostics.
	if got := e.Keys; len(got) != 4 || got[0] != "binary" || got[3] != "repo" {
		t.Fatalf("arg keys not sorted: %v", got)
	}
	if got := lay.UsedAliases(); len(got) != 1 || got[0] != "cc-skills" {
		t.Fatalf("used aliases = %v", got)
	}
}

func TestParseRejectsCRLF(t *testing.T) {
	// CRLF anywhere in layout.toml is a hard error (the CRLF-everywhere invariant).
	if _, err := layout.Parse([]byte("fragments = [\"a\"]\r\n")); !errors.Is(err, guide.ErrCRLF) {
		t.Fatalf("err = %v, want ErrCRLF", err)
	}
}

func TestBakedDefaultInjection(t *testing.T) {
	// No [sources] table: the cc-skills default is injected so imports resolve.
	lay, err := layout.Parse([]byte("fragments = [\"cc-skills:ccx\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if lay.Sources["cc-skills"] != layout.DefaultSourceSpec {
		t.Fatalf("baked default not injected: %v", lay.Sources)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		toml    string
		wantErr error
	}{
		{"unknown top-level key", "fragments = [\"a\"]\nfragmnets = [\"b\"]\n", layout.ErrUnknownKey},
		{"unknown source key alongside a valid source", "fragments = [\"a\"]\n\n[sources.cc-skills]\nsource = \"github:x/y//g\"\nsauce = \"typo\"\n", layout.ErrUnknownKey},
		{"fragments nested under sources", "[sources.cc-skills]\nsource = \"github:x/y//g\"\nfragments = [\"a\"]\n", layout.ErrFragmentsNested},
		{"empty compose", "[sources.cc-skills]\nsource = \"github:x/y//g\"\n", layout.ErrEmptyCompose},
		{"bad name uppercase", "fragments = [\"Ccx\"]\n", layout.ErrBadName},
		{"bad alias", "fragments = [\"BAD:ccx\"]\n", layout.ErrBadName},
		{"undeclared alias", "fragments = [\"team:x\"]\n", layout.ErrUndeclaredAlias},
		{"bad entry type", "fragments = [42]\n", layout.ErrBadEntry},
		{"table without use", "fragments = [ { args = { a = \"b\" } } ]\n", layout.ErrBadEntry},
		{"bad arg value quote-ish", "fragments = [ { use = \"cc-skills:x\", args = { k = \"a b\" } } ]\n", layout.ErrBadArg},
		{"empty source spec", "fragments = [\"a\"]\n\n[sources.team]\nsource = \"\"\n", layout.ErrEmptySource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := layout.Parse([]byte(tc.toml))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseCustomSource(t *testing.T) {
	toml := "fragments = [\"team:x\"]\n\n[sources.team]\nsource = \"github:acme/guides//g@v1\"\n"
	lay, err := layout.Parse([]byte(toml))
	if err != nil {
		t.Fatal(err)
	}
	if lay.Sources["team"] != "github:acme/guides//g@v1" {
		t.Fatalf("custom source = %v", lay.Sources)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	lay, err := layout.Parse([]byte(validTOML))
	if err != nil {
		t.Fatal(err)
	}
	encoded := layout.Encode(lay)
	// The baked default source is omitted from the encoding.
	if got := string(encoded); containsStr(got, "[sources.cc-skills]") {
		t.Fatalf("baked-default source must be omitted:\n%s", got)
	}
	back, err := layout.Parse(encoded)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, encoded)
	}
	if len(back.Entries) != len(lay.Entries) {
		t.Fatalf("round-trip entries %d != %d", len(back.Entries), len(lay.Entries))
	}
	for i := range lay.Entries {
		if back.Entries[i].Ref() != lay.Entries[i].Ref() {
			t.Fatalf("entry %d ref %q != %q", i, back.Entries[i].Ref(), lay.Entries[i].Ref())
		}
	}
	if back.Entries[2].Args["brew"] != "yasyf/tap/slop-cop" {
		t.Fatalf("round-trip lost args: %+v", back.Entries[2])
	}
}

// A custom [sources.*] table survives the round-trip.
func TestEncodeCustomSourceRoundTrip(t *testing.T) {
	toml := "fragments = [\"team:x\"]\n\n[sources.team]\nsource = \"github:acme/guides//g@v1\"\n"
	lay, _ := layout.Parse([]byte(toml))
	back, err := layout.Parse(layout.Encode(lay))
	if err != nil {
		t.Fatal(err)
	}
	if back.Sources["team"] != "github:acme/guides//g@v1" {
		t.Fatalf("custom source lost: %v", back.Sources)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
