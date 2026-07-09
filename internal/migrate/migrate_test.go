package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

// mapImporter is an in-memory source.Importer for offline migrate tests.
type mapImporter struct {
	bodies map[string][]byte // "alias:name:kind" -> body
	pin    string
	used   map[string]bool
}

func newImporter(pin string) *mapImporter {
	return &mapImporter{bodies: map[string][]byte{}, pin: pin, used: map[string]bool{}}
}

func (m *mapImporter) add(alias, name string, kind guide.Kind, body string) {
	m.bodies[alias+":"+name+":"+kind.String()] = []byte(body)
}

func (m *mapImporter) Resolve(_ context.Context, alias, name string, kind guide.Kind) ([]byte, bool, error) {
	b, ok := m.bodies[alias+":"+name+":"+kind.String()]
	if ok {
		m.used[alias] = true
	}
	return b, ok, nil
}

func (m *mapImporter) Pin(alias string) (string, bool) {
	if m.used[alias] {
		return m.pin, true
	}
	return "", false
}

func doc(t *testing.T, src string) *guide.Doc {
	t.Helper()
	d, err := guide.Parse([]byte(src), guide.KindMD)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSegments(t *testing.T) {
	segs := Segments(doc(t, "# Head\n\nIntro.\n\n{{> ccx}}\n\nEnd.\n"))
	if len(segs) != 3 {
		t.Fatalf("segments = %d, want 3", len(segs))
	}
	if segs[0].Prose != "# Head\n\nIntro." {
		t.Fatalf("seg0 = %q", segs[0].Prose)
	}
	if segs[1].Import == nil || segs[1].Import.Ref() != "cc-skills:ccx" {
		t.Fatalf("seg1 = %+v", segs[1])
	}
	if segs[2].Prose != "End." {
		t.Fatalf("seg2 = %q", segs[2].Prose)
	}
}

// Two directives with only blank lines between them drop the empty run.
func TestSegmentsDropsEmptyRun(t *testing.T) {
	segs := Segments(doc(t, "{{> a}}\n\n{{> b}}\n"))
	if len(segs) != 2 || segs[0].Import == nil || segs[1].Import == nil {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestSlugNaming(t *testing.T) {
	cases := []struct{ prose, want string }{
		{"# Compact Context (ccx)\nbody", "compact-context-ccx"},
		{"just prose, no heading", ""},
		{"### 3-fold plan\ntext", "3-fold-plan"},
		{"# " + strings.Repeat("a", 60), strings.Repeat("a", 40)},
	}
	for _, tc := range cases {
		if got := slugFromProse(tc.prose); got != tc.want {
			t.Errorf("slugFromProse(%q) = %q, want %q", tc.prose, got, tc.want)
		}
	}
}

func TestSlugDedupe(t *testing.T) {
	used := map[string]int{}
	if got := dedupe("intro", used); got != "intro" {
		t.Fatalf("first = %q", got)
	}
	if got := dedupe("intro", used); got != "intro-2" {
		t.Fatalf("second = %q", got)
	}
	if got := dedupe("intro", used); got != "intro-3" {
		t.Fatalf("third = %q", got)
	}
}

func TestBuildRoundTrip(t *testing.T) {
	imp := newImporter("abcdef012345")
	imp.add("cc-skills", "ccx", guide.KindMD, "## Compact Context\nccx body\n")
	segs := Segments(doc(t, "# Head\n\nIntro.\n\n{{> ccx}}\n\nEnd.\n"))
	expect := "# Head\n\nIntro.\n\n## Compact Context\nccx body\n\nEnd.\n"

	out, err := Build(context.Background(), Input{
		Target:     "AGENTS.md",
		Kind:       guide.KindMD,
		Segments:   segs,
		ExpectBody: []byte(expect),
		Version:    "1.0.0",
		Importer:   imp,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if out.LayoutDir != ".claude/fragments/AGENTS.md" {
		t.Fatalf("layout dir = %q", out.LayoutDir)
	}
	if _, ok := out.FragmentFiles["head.fragment.md"]; !ok {
		t.Fatalf("missing head.fragment.md: %v", keys(out.FragmentFiles))
	}
	if _, ok := out.FragmentFiles["part-1.fragment.md"]; !ok {
		t.Fatalf("missing part-1.fragment.md: %v", keys(out.FragmentFiles))
	}
	lt := string(out.LayoutTOML)
	for _, want := range []string{`"head"`, `"cc-skills:ccx"`, `"part-1"`} {
		if !strings.Contains(lt, want) {
			t.Errorf("layout.toml missing %s:\n%s", want, lt)
		}
	}
	// The artifact carries a v2 banner pinning the import's sha, then the body.
	art := string(out.Artifact)
	if !strings.HasPrefix(art, "<!-- cc-guides 1.0.0 src=.claude/fragments/AGENTS.md fragments=cc-skills@abcdef012345 | GENERATED") {
		t.Fatalf("banner = %q", firstLine(art))
	}
	if !strings.HasSuffix(art, "\n\n## Compact Context\nccx body\n\nEnd.\n") {
		t.Fatalf("artifact body wrong:\n%s", art)
	}
}

// A flat v1 override backing a directive folds into a local fragment, and the
// deployed artifact's v1 `local:` markers are stripped for the self-verify.
func TestBuildFoldsOverride(t *testing.T) {
	imp := newImporter("abcdef012345")
	imp.add("cc-skills", "ccx", guide.KindMD, "## Shared ccx\nupstream body\n")
	segs := Segments(doc(t, "# Head\n\n{{> ccx}}\n"))
	// The deployed artifact rendered the override wrapped in v1 local: markers.
	expect := "# Head\n\n<!-- local: .claude/fragments/ccx.md -->\n## Local ccx\nrepo-specific body\n<!-- /local: .claude/fragments/ccx.md -->\n"
	overrides := map[string][]byte{"ccx": []byte("## Local ccx\nrepo-specific body\n")}

	out, err := Build(context.Background(), Input{
		Target:     "AGENTS.md",
		Kind:       guide.KindMD,
		Segments:   segs,
		ExpectBody: []byte(expect),
		Version:    "1.0.0",
		Importer:   imp,
		Overrides:  overrides,
	})
	if err != nil {
		t.Fatalf("fold override: %v", err)
	}
	// The override became a LOCAL fragment named for the directive, not an import.
	if _, ok := out.FragmentFiles["ccx.fragment.md"]; !ok {
		t.Fatalf("override not folded to local fragment: %v", keys(out.FragmentFiles))
	}
	if strings.Contains(string(out.LayoutTOML), "cc-skills:ccx") {
		t.Fatalf("folded override must not remain an import:\n%s", out.LayoutTOML)
	}
	if !strings.Contains(string(out.Artifact), "repo-specific body") {
		t.Fatalf("folded override content lost:\n%s", out.Artifact)
	}
}

func TestBuildSelfVerifyMismatch(t *testing.T) {
	imp := newImporter("abcdef012345")
	imp.add("cc-skills", "ccx", guide.KindMD, "## Compact Context\nccx body\n")
	segs := Segments(doc(t, "# Head\n\n{{> ccx}}\n"))
	_, err := Build(context.Background(), Input{
		Target:     "AGENTS.md",
		Kind:       guide.KindMD,
		Segments:   segs,
		ExpectBody: []byte("totally different\n"),
		Version:    "1.0.0",
		Importer:   imp,
	})
	var sv *SelfVerifyError
	if !errors.As(err, &sv) {
		t.Fatalf("err = %v, want SelfVerifyError", err)
	}
	if !strings.Contains(sv.Diff, "AGENTS.md") {
		t.Fatalf("diff should be labeled: %q", sv.Diff)
	}
}

func TestBuildImportArgs(t *testing.T) {
	imp := newImporter("abcdef012345")
	imp.add("cc-skills", "install", guide.KindSH, "#!/bin/sh\nNAME={{binary}}\n")
	d, _ := guide.Parse([]byte("{{> install binary=slop-cop}}\n"), guide.KindSH)
	segs := Segments(d)
	expect := "#!/bin/sh\nNAME=slop-cop\n"
	out, err := Build(context.Background(), Input{
		Target:     "install.sh",
		Kind:       guide.KindSH,
		Segments:   segs,
		ExpectBody: []byte(expect),
		Version:    "1.0.0",
		Importer:   imp,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(string(out.LayoutTOML), `binary = "slop-cop"`) {
		t.Fatalf("layout.toml missing args:\n%s", out.LayoutTOML)
	}
	// Shebang stays on line 1, banner on line 2.
	lines := strings.SplitN(string(out.Artifact), "\n", 3)
	if lines[0] != "#!/bin/sh" || !strings.HasPrefix(lines[1], "# cc-guides ") {
		t.Fatalf("shebang/banner placement wrong: %q / %q", lines[0], lines[1])
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
