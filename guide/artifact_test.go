package guide_test

import (
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func TestArtifactPath(t *testing.T) {
	cases := []struct {
		src     string
		want    string
		wantErr bool
	}{
		{src: "AGENTS.src.md", want: "AGENTS.md"},
		{src: "dir/CLAUDE.src.md", want: "dir/CLAUDE.md"},
		{src: "a/b/install-binary.src.sh", want: "a/b/install-binary.sh"},
		{src: "AGENTS.md", wantErr: true},     // not a source
		{src: "notes.src.txt", wantErr: true}, // unsupported ext
	}
	for _, tc := range cases {
		got, err := guide.ArtifactPath(tc.src)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ArtifactPath(%q) = %q, want error", tc.src, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ArtifactPath(%q) error: %v", tc.src, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ArtifactPath(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestSourcePath(t *testing.T) {
	if got := guide.SourcePath("AGENTS.md"); got != "AGENTS.src.md" {
		t.Errorf("SourcePath = %q", got)
	}
	if got := guide.SourcePath("d/x.sh"); got != "d/x.src.sh" {
		t.Errorf("SourcePath = %q", got)
	}
}

func TestIsSource(t *testing.T) {
	yes := []string{"AGENTS.src.md", "x.src.sh", "d/y.src.md"}
	no := []string{"AGENTS.md", "x.sh", "README.md", "x.src.txt", "src.md"}
	for _, p := range yes {
		if !guide.IsSource(p) {
			t.Errorf("IsSource(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if guide.IsSource(p) {
			t.Errorf("IsSource(%q) = true, want false", p)
		}
	}
}

func TestTargetForLayoutDir(t *testing.T) {
	cases := []struct {
		dir     string
		want    string
		wantErr bool
	}{
		{dir: ".claude/fragments/AGENTS.md", want: "AGENTS.md"},
		{dir: ".claude/fragments/plugin/scripts/install-binary.sh", want: "plugin/scripts/install-binary.sh"},
		{dir: ".claude/fragments/CLAUDE.md", want: "CLAUDE.md"},
		{dir: "AGENTS.md", wantErr: true},                                // not under the fragments root
		{dir: ".claude/fragments/notes.txt", wantErr: true},              // unsupported extension
		{dir: ".claude/fragments/../../etc/passwd.md", wantErr: true},    // escapes via ..
		{dir: ".claude/fragments/.claude/fragments/x.md", wantErr: true}, // lands back under the root
	}
	for _, tc := range cases {
		got, _, err := guide.TargetForLayoutDir(tc.dir)
		if tc.wantErr {
			if err == nil {
				t.Errorf("TargetForLayoutDir(%q) = %q, want error", tc.dir, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("TargetForLayoutDir(%q) error: %v", tc.dir, err)
			continue
		}
		if got != tc.want {
			t.Errorf("TargetForLayoutDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

func TestKindFromExt(t *testing.T) {
	if _, err := guide.KindFromExt(".md"); err != nil {
		t.Errorf(".md: %v", err)
	}
	if _, err := guide.KindFromExt(".sh"); err != nil {
		t.Errorf(".sh: %v", err)
	}
	if _, err := guide.KindFromExt(".txt"); err == nil {
		t.Error(".txt should error")
	}
}
