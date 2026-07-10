package guide_test

import (
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

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
