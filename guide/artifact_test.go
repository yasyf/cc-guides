package guide_test

import (
	"errors"
	"strings"
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
		{dir: ".claude/fragments/great-docs.yml", want: "great-docs.yml"},
		{dir: ".claude/fragments/.github/workflows/docs.yml", want: ".github/workflows/docs.yml"}, // nested yml target
		{dir: ".claude/fragments/.pre-commit-config.yaml", want: ".pre-commit-config.yaml"},       // root dotfile .yaml target
		{dir: ".claude/fragments/.gitignore", want: ".gitignore"},                                 // root gitignore target
		{dir: ".claude/fragments/docs/.gitignore", want: "docs/.gitignore"},                       // nested gitignore target
		{dir: "AGENTS.md", wantErr: true},                                                         // not under the fragments root
		{dir: ".claude/fragments/notes.txt", wantErr: true},                                       // unsupported extension
		{dir: ".claude/fragments/../../etc/passwd.md", wantErr: true},                             // escapes via ..
		{dir: ".claude/fragments/.claude/fragments/x.md", wantErr: true},                          // lands back under the root
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
	if _, err := guide.KindFromExt(".yml"); err != nil {
		t.Errorf(".yml: %v", err)
	}
	if k, err := guide.KindFromExt(".yaml"); err != nil {
		t.Errorf(".yaml: %v", err)
	} else if k != guide.KindYAML {
		t.Errorf(".yaml = %v, want KindYAML", k)
	}
	_, err := guide.KindFromExt(".txt")
	if err == nil {
		t.Error(".txt should error")
	} else if !strings.Contains(err.Error(), ".yml") {
		t.Errorf(".txt error must list .yml as supported: %v", err)
	}
	// filepath.Ext(".gitignore") is the whole dotfile name, so a root or nested
	// .gitignore target dispatches to KindGitignore unchanged.
	if k, err := guide.KindForPath(".gitignore"); err != nil {
		t.Errorf(".gitignore: %v", err)
	} else if k != guide.KindGitignore {
		t.Errorf("KindForPath(\".gitignore\") = %v, want KindGitignore", k)
	}
	if k, err := guide.KindForPath("docs/.gitignore"); err != nil {
		t.Errorf("docs/.gitignore: %v", err)
	} else if k != guide.KindGitignore {
		t.Errorf("KindForPath(\"docs/.gitignore\") = %v, want KindGitignore", k)
	}
}

func TestExtensionDiagnostics(t *testing.T) {
	const extensions = ".md, .sh, .json, .yml, .yaml, .toml, or .gitignore"
	if got := guide.SupportedExtensions(); got != extensions {
		t.Fatalf("SupportedExtensions() = %q, want %q", got, extensions)
	}

	dir := ".claude/fragments/notes.txt"
	_, _, err := guide.TargetForLayoutDir(dir)
	if err == nil {
		t.Fatal("TargetForLayoutDir() succeeded, want error")
	}
	if !errors.Is(err, guide.ErrUnknownExt) {
		t.Fatalf("TargetForLayoutDir() error = %v, want errors.Is(err, guide.ErrUnknownExt)", err)
	}
	want := `layout dir ".claude/fragments/notes.txt": target "notes.txt" must end in .md, .sh, .json, .yml, .yaml, .toml, or .gitignore: unsupported extension: ".txt" (supported: .md, .sh, .json, .yml, .yaml, .toml, or .gitignore)`
	if err.Error() != want {
		t.Fatalf("TargetForLayoutDir() error = %q, want %q", err, want)
	}
}
