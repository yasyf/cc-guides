package cli

import (
	"strings"
	"testing"

	"github.com/yasyf/cc-guides/lockfile"
)

func TestLockDiffPinsOnly(t *testing.T) {
	const headers = "diff --git a/.claude/fragments/cc-guides.lock b/.claude/fragments/cc-guides.lock\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/.claude/fragments/cc-guides.lock\n" +
		"+++ b/.claude/fragments/cc-guides.lock\n"

	tests := []struct {
		name string
		diff string
		want bool
	}{
		{
			name: "pins only — version and commit moved",
			diff: headers +
				"@@ -2 +2 @@\n" +
				"-version = \"0.1.16\"\n" +
				"+version = \"0.1.17\"\n" +
				"@@ -9 +9 @@\n" +
				"-commit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n" +
				"+commit = \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"\n",
			want: true,
		},
		{
			name: "commit-only bump",
			diff: headers +
				"@@ -9 +9 @@\n" +
				"-commit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n" +
				"+commit = \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"\n",
			want: true,
		},
		{
			name: "semantic — spec changed",
			diff: headers +
				"@@ -7 +7 @@\n" +
				"-spec = \"github:yasyf/cc-skills@main\"\n" +
				"+spec = \"github:yasyf/cc-skills@next\"\n",
			want: false,
		},
		{
			name: "semantic — artifacts changed",
			diff: headers +
				"@@ -4 +4 @@\n" +
				"-artifacts = [\"AGENTS.md\"]\n" +
				"+artifacts = [\"AGENTS.md\", \"CLAUDE.md\"]\n",
			want: false,
		},
		{
			name: "semantic — schema changed",
			diff: headers +
				"@@ -2 +2 @@\n" +
				"-schema = 1\n" +
				"+schema = 2\n",
			want: false,
		},
		{
			name: "semantic — new source table header",
			diff: headers +
				"@@ -10 +11,4 @@\n" +
				"+[sources.team]\n" +
				"+spec = \"github:acme/g//g@v1\"\n" +
				"+commit = \"cccccccccccccccccccccccccccccccccccccccc\"\n",
			want: false,
		},
		{
			name: "mixed — pins plus a spec",
			diff: headers +
				"@@ -2 +2 @@\n" +
				"-version = \"0.1.16\"\n" +
				"+version = \"0.1.17\"\n" +
				"@@ -7 +7 @@\n" +
				"-spec = \"github:yasyf/cc-skills@main\"\n" +
				"+spec = \"github:yasyf/cc-skills@next\"\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lockDiffPinsOnly(tt.diff); got != tt.want {
				t.Errorf("lockDiffPinsOnly = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourcePins(t *testing.T) {
	tests := []struct {
		name string
		lock string
		want string
	}{
		{
			name: "two sources, alias order",
			lock: "# Written by 'cc-guides render' — do not edit.\n" +
				"schema = 1\nversion = \"0.1.17\"\nartifacts = [\"AGENTS.md\"]\n\n" +
				"[sources.cc-skills]\nspec = \"github:yasyf/cc-skills@main\"\n" +
				"commit = \"abcdef0123456789abcdef0123456789abcdef01\"\n\n" +
				"[sources.team]\nspec = \"github:acme/g//g@v1\"\n" +
				"commit = \"0123456789abcdef0123456789abcdef01234567\"\n",
			want: "cc-skills@abcdef012345,team@0123456789ab",
		},
		{
			name: "local pin kept whole (shorter than 12)",
			lock: "schema = 1\nversion = \"dev\"\nartifacts = [\"AGENTS.md\"]\n\n" +
				"[sources.cc-skills]\nspec = \"github:yasyf/cc-skills@main\"\ncommit = \"local\"\n",
			want: "cc-skills@local",
		},
		{
			name: "import-free lock yields empty",
			lock: "schema = 1\nversion = \"0.1.17\"\nartifacts = [\"AGENTS.md\"]\n",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sourcePins([]byte(tt.lock)); got != tt.want {
				t.Errorf("sourcePins = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSourcePinsMatchesEncodedLock guards the pins builder against drift in the
// lock's on-disk format by running it over a real lockfile.Encode() output.
func TestSourcePinsMatchesEncodedLock(t *testing.T) {
	lk := &lockfile.Lock{
		Schema:    1,
		Version:   "0.1.17",
		Artifacts: []string{"AGENTS.md"},
		Sources: map[string]lockfile.SourcePin{
			"cc-skills": {Spec: "github:yasyf/cc-skills@main", Commit: strings.Repeat("a", 40)},
			"team":      {Spec: "github:acme/g//g@v1", Commit: strings.Repeat("b", 40)},
		},
	}
	want := "cc-skills@" + strings.Repeat("a", 12) + ",team@" + strings.Repeat("b", 12)
	if got := sourcePins(lk.Encode()); got != want {
		t.Fatalf("sourcePins over encoded lock = %q, want %q", got, want)
	}
}
