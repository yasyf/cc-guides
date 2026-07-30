package guide_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/guide"
)

func TestTargetForLayoutDir(t *testing.T) {
	cases := []struct {
		dir      string
		override string
		want     string
		wantErr  bool
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

		// An override renders the dir to a path other than its own, so a dir named
		// gitignore/ need not shadow the file every tool reads.
		{dir: ".claude/fragments/gitignore", override: ".gitignore", want: ".gitignore"},
		{dir: ".claude/fragments/mcp-json", override: ".mcp.json", want: ".mcp.json"},
		{dir: ".claude/fragments/docs/gitignore", override: "docs/.gitignore", want: "docs/.gitignore"},
		// The dir guard still runs on the dir, not the override.
		{dir: "gitignore", override: ".gitignore", wantErr: true},
		// Every target guard runs on the override exactly as on a path-derived target.
		{dir: ".claude/fragments/gitignore", override: "../../etc/passwd.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: "docs/../../passwd.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: "/etc/passwd.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: ".claude/fragments/x.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: "notes.txt", wantErr: true},
		// An unsupported dir extension is rescued by a supported override, and a
		// supported dir extension is not rescued by an unsupported one.
		{dir: ".claude/fragments/notes.txt", override: "notes.md", want: "notes.md"},
		{dir: ".claude/fragments/AGENTS.md", override: "AGENTX.txt", wantErr: true},
		// A control character would reach the lock and write TOML that cannot be
		// parsed back, so it is refused at the guard, not at the serializer.
		{dir: ".claude/fragments/gitignore", override: "ok.md\n[sources.evil]\nspec = \"x\"\nnote.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: "a\tb.md", wantErr: true},
		{dir: ".claude/fragments/gitignore", override: "a\x00b.md", wantErr: true},
		// A backslash is not a separator on any platform cc-guides ships for.
		{dir: ".claude/fragments/gitignore", override: `..\etc\passwd.md`, wantErr: true},
	}
	for _, tc := range cases {
		got, _, err := guide.TargetForLayoutDir(tc.dir, tc.override)
		if tc.wantErr {
			if err == nil {
				t.Errorf("TargetForLayoutDir(%q, %q) = %q, want error", tc.dir, tc.override, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("TargetForLayoutDir(%q, %q) error: %v", tc.dir, tc.override, err)
			continue
		}
		if got != tc.want {
			t.Errorf("TargetForLayoutDir(%q, %q) = %q, want %q", tc.dir, tc.override, got, tc.want)
		}
	}
}

// A target naming a directory rather than a file must be turned back by the escape
// guard. `..` once reached the extension check and was refused only because
// filepath.Ext("..") is "." — an accident that reported a misleading error and would
// have vanished had "." ever become a registered kind.
func TestTargetDirectoryFormsHitTheEscapeGuard(t *testing.T) {
	for _, override := range []string{"..", ".", "../", "./"} {
		_, _, err := guide.TargetForLayoutDir(".claude/fragments/gitignore", override)
		if err == nil {
			t.Fatalf("override %q must be refused", override)
		}
		if !strings.Contains(err.Error(), "unsafe target") {
			t.Fatalf("override %q must fail the escape guard, got: %v", override, err)
		}
		if errors.Is(err, guide.ErrUnknownExt) {
			t.Fatalf("override %q must not be refused by the extension check: %v", override, err)
		}
	}
}

// A control character or backslash in a target is refused with its own sentinel, so
// the diagnostic says what is wrong rather than blaming the extension.
func TestTargetCharsetSentinel(t *testing.T) {
	for _, override := range []string{"a\nb.md", "a\x1fb.md", `a\b.md`} {
		_, _, err := guide.TargetForLayoutDir(".claude/fragments/gitignore", override)
		if !errors.Is(err, guide.ErrUnsafePathChar) {
			t.Fatalf("override %q error = %v, want ErrUnsafePathChar", override, err)
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
	_, _, err := guide.TargetForLayoutDir(dir, "")
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
